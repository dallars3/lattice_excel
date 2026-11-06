// 全流程 P1/P2 用例执行器：API / WS 部分（对 http://127.0.0.1:8080 真实部署）
// 运行：node p1p2-api.mjs（Node 22；依赖 ws、xlsx）
// 结果落 p1p2-api-results.json，供汇总报告使用。
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import WebSocket from 'ws'
import * as XLSX from 'xlsx'

const BASE = process.env.FF_BASE || 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = 'p' + Date.now().toString(36).slice(-6)
const OUT_DIR = path.dirname(fileURLToPath(import.meta.url))
const fixtures = path.join(OUT_DIR, 'fixtures')
const evidenceDir = path.join(OUT_DIR, 'evidence')
fs.mkdirSync(evidenceDir, { recursive: true })
fs.mkdirSync(fixtures, { recursive: true })

const results = []
const ctx = {}

function record(id, name, status, detail) {
  results.push({ id, name, status, detail })
  const icon = status
  console.log(`[${icon}] ${id} ${name}${detail ? ' — ' + detail : ''}`)
}

class Blocked extends Error {}
class NotRun extends Error {}

async function testCase(id, name, fn) {
  try {
    const detail = await fn()
    record(id, name, 'PASS', detail)
  } catch (error) {
    if (error instanceof Blocked) record(id, name, 'BLOCKED', error.message)
    else if (error instanceof NotRun) record(id, name, 'NOT_RUN', error.message)
    else record(id, name, 'FAIL', error?.message || String(error))
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

function sleep(ms) { return new Promise((resolve) => setTimeout(resolve, ms)) }

async function req(method, urlPath, { token, body, headers = {}, redirect = 'follow', form, raw = false } = {}) {
  const url = urlPath.startsWith('http') ? urlPath : BASE + urlPath
  const h = { ...headers }
  if (token) h.Authorization = `Bearer ${token}`
  let payload
  if (form !== undefined) payload = form
  else if (body !== undefined) { h['Content-Type'] = 'application/json'; payload = JSON.stringify(body) }
  let response
  try {
    response = await fetch(url, { method, headers: h, body: payload, redirect })
  } catch (error) {
    console.log(`  network error: ${method} ${urlPath.split('?')[0]} -> ${error.cause?.code || error.cause?.message || error.message}`)
    throw error
  }
  const buf = Buffer.from(await response.arrayBuffer())
  const text = buf.toString('utf8')
  let json = null
  if ((response.headers.get('content-type') || '').includes('json')) {
    try { json = JSON.parse(text) } catch { /* ignore */ }
  }
  return { status: response.status, headers: response.headers, buf, text, json, raw }
}

function apiData(r) { return r.json?.data }

async function register(username, extra = {}, attempt = 0) {
  const r = await req('POST', '/api/v1/auth/register', { body: { username, password: PWD, ...extra } })
  if (r.json?.code === 5003 && attempt < 5) {
    console.log(`  register ${username} rate limited, wait 15s`)
    await sleep(15000)
    return register(username, extra, attempt + 1)
  }
  return r
}

async function login(username, password = PWD, attempt = 0) {
  const r = await req('POST', '/api/v1/auth/login', { body: { username, password } })
  if (r.json?.code === 5003 && attempt < 6) {
    await sleep(10000)
    return login(username, password, attempt + 1)
  }
  if (r.json?.code !== 0) throw new Error(`login ${username} failed: ${r.status} ${r.text}`)
  return r.json.data.token
}

function decodeValue(value) {
  if (value === null || typeof value !== 'object') return value
  if ('string_value' in value) return value.string_value
  if ('int_value' in value) return Number(value.int_value)
  if ('double_value' in value) return value.double_value
  if ('bool_value' in value) return value.bool_value
  if ('date_value_ms' in value) return Number(value.date_value_ms)
  if ('list_value' in value) return (value.list_value?.value_list ?? []).map(decodeValue)
  return value
}

function decodeCells(row) {
  const cells = row?.cells_map ?? {}
  const decoded = {}
  for (const [key, value] of Object.entries(cells)) decoded[key] = decodeValue(value)
  return decoded
}

function enumText(value) {
  return String(value ?? '')
    .toLowerCase()
    .replace(/^(member_role|share_role|collaborator_role|member_status|audit_action|comment_status|import_status|export_status|role)_/, '')
}

async function uploadFile(token, baseId, filePath, usage = 'import_temp', displayName = null) {
  const fileName = path.basename(filePath)
  const bytes = fs.readFileSync(filePath)
  const form = new FormData()
  form.append('usage', usage)
  if (baseId) form.append('base_id', baseId)
  form.append('file', new Blob([bytes]), fileName)
  return req('POST', '/api/v1/files', { token, form })
}

function wsConnect(token, baseId, tableId) {
  const wsUrl = BASE.replace(/^http/, 'ws') + `/bases/${encodeURIComponent(baseId)}/tables/${encodeURIComponent(tableId)}/ws`
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(wsUrl, { headers: { Authorization: `Bearer ${token}` } })
    const frames = []
    const waiters = []
    let closed = false
    ws.on('message', (data) => {
      try {
        const frame = JSON.parse(data.toString())
        frames.push(frame)
        for (const waiter of [...waiters]) {
          if (waiter.predicate(frame)) {
            waiters.splice(waiters.indexOf(waiter), 1)
            clearTimeout(waiter.timer)
            waiter.resolve(frame)
          }
        }
      } catch { /* ignore */ }
    })
    ws.on('close', () => { closed = true })
    ws.on('open', () => resolve({ ws, frames, waitFrame, waitClose, close: () => ws.close(), isClosed: () => closed }))
    ws.on('error', reject)

    function waitFrame(predicate, timeoutMs = 5000) {
      const hit = frames.find(predicate)
      if (hit) return Promise.resolve(hit)
      return new Promise((res, rej) => {
        const waiter = {
          predicate,
          resolve: res,
          timer: setTimeout(() => {
            const idx = waiters.indexOf(waiter)
            if (idx >= 0) waiters.splice(idx, 1)
            rej(new Error('ws frame timeout'))
          }, timeoutMs),
        }
        waiters.push(waiter)
      })
    }

    function waitClose(timeoutMs = 3000) {
      if (closed) return Promise.resolve(true)
      return new Promise((res) => {
        const timer = setTimeout(() => res(false), timeoutMs)
        ws.on('close', () => { clearTimeout(timer); res(true) })
      })
    }
  })
}

function wsExpectRejected(token, baseId, tableId, timeoutMs = 4000) {
  const wsUrl = BASE.replace(/^http/, 'ws') + `/bases/${encodeURIComponent(baseId)}/tables/${encodeURIComponent(tableId)}/ws`
  return new Promise((resolve) => {
    const ws = new WebSocket(wsUrl, { headers: { Authorization: `Bearer ${token}` } })
    let settled = false
    const finish = (value) => { if (!settled) { settled = true; clearTimeout(timer); try { ws.terminate() } catch {} resolve(value) } }
    const timer = setTimeout(() => finish({ rejected: false, reason: 'timeout' }), timeoutMs)
    ws.on('open', () => finish({ rejected: false, reason: 'opened' }))
    ws.on('unexpected-response', (_request, response) => finish({ rejected: true, status: response.statusCode }))
    ws.on('error', (error) => finish({ rejected: true, reason: error.message }))
  })
}

// ---------- 夹具生成 ----------
function ensureFixtures() {
  const cleanCsv = path.join(fixtures, 'clean.csv')
  const dirtyCsv = path.join(fixtures, 'dirty.csv')
  const multiXlsx = path.join(fixtures, 'multi.xlsx')
  const legacyXls = path.join(fixtures, 'legacy.xls')
  const corruptXlsx = path.join(fixtures, 'corrupt.xlsx')
  const bigFile = path.join(fixtures, 'big-import.bin')
  const exeFile = path.join(fixtures, 'malware.exe')
  const largeCsv = path.join(fixtures, 'large.csv')

  fs.writeFileSync(cleanCsv, '名称,数量\n甲,1\n乙,2\n丙,3\n')
  fs.writeFileSync(dirtyCsv, '名称,数量\n正常行,1\n坏行,不是数字\n只有名称,\n')
  fs.writeFileSync(corruptXlsx, 'this is not a real xlsx')
  fs.writeFileSync(exeFile, Buffer.from('MZ fake executable content'))
  fs.writeFileSync(bigFile, Buffer.alloc(21 * 1024 * 1024, 65))
  const largeLines = ['名称,数量']
  for (let i = 0; i < 20000; i += 1) largeLines.push(`行${i},${i}`)
  fs.writeFileSync(largeCsv, largeLines.join('\n'))

  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([['名称', '数量'], ['甲', 1], ['乙', 2], ['丙', 3]]), '第一页')
  XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([['标题', '分数'], ['丙', 90], ['丁', 80], ['戊', 70]]), '第二页')
  XLSX.writeFile(wb, multiXlsx)
  XLSX.writeFile(wb, legacyXls, { bookType: 'xls' })
  return { cleanCsv, dirtyCsv, multiXlsx, legacyXls, corruptXlsx, bigFile, exeFile, largeCsv }
}

async function main() {
  const files = ensureFixtures()

  // ================= 0. 环境与账号准备 =================
  const health = await req('GET', '/healthz')
  const ready = await req('GET', '/readyz')
  console.log(`env health=${health.status} ready=${ready.status}`)

  const names = {
    owner: `p1own${RUN}`,
    peer: `p1peer${RUN}`,
    editor: `p1edt${RUN}`,
    filler: `p1fil${RUN}`,
    viewer: `p1vie${RUN}`,
    admin2: `p1adm${RUN}`,
    ext: `p1ext${RUN}`,
    extViewer: `p1exv${RUN}`,
    invitee: `p1inv${RUN}`,
  }
  ctx.names = names

  const reg = {}
  for (const [key, name] of Object.entries(names)) {
    const extra = key === 'owner' ? { enterprise_name: `P1企业${RUN.slice(-4)}` } : key === 'peer' ? { enterprise_name: `P1对测${RUN.slice(-4)}` } : {}
    const r = await register(name, extra)
    assert(r.json?.code === 0, `register ${key} failed: ${r.status} ${r.text}`)
    reg[key] = r
  }

  const tokens = {}
  for (const key of Object.keys(names)) tokens[key] = await login(names[key])
  ctx.tokens = tokens

  // 邀请 editor/filler/viewer/admin2/invitee 入 owner 企业
  const invite = await req('POST', '/api/v1/enterprise/invites', {
    token: tokens.owner,
    body: { username_list: [names.editor, names.filler, names.viewer, names.admin2, names.invitee] },
  })
  assert(invite.json?.code === 0, `enterprise invite failed: ${invite.text}`)
  const invitationList = apiData(invite)?.invitation_list ?? []
  for (const key of ['editor', 'filler', 'viewer', 'admin2', 'invitee']) {
    const invitation = invitationList.find((item) => item.username === names[key])
    assert(invitation, `invitation for ${key} missing`)
    const accepted = await req('POST', `/api/v1/invitations/${invitation.id}/accept`, { token: tokens[key] })
    assert(accepted.json?.code === 0, `accept invitation ${key} failed: ${accepted.text}`)
    tokens[key] = await login(names[key])
  }
  ctx.inviteAccepted = invitationList.length

  const meOwner = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.owner }))
  const mePeer = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.peer }))
  ctx.ownerUserId = meOwner?.user?.id
  ctx.peerUserId = mePeer?.user?.id
  assert(ctx.ownerUserId && ctx.peerUserId, 'user ids missing')

  // admin2 提为企业管理员
  const admin2Id = apiData(await req('GET', '/api/v1/enterprise/members', { token: tokens.owner }))?.member_list
    ?.find((member) => member.username === names.admin2)?.user_id
  assert(admin2Id, 'admin2 user id missing')
  const promote = await req('PUT', `/api/v1/enterprise/members/${admin2Id}/role`, { token: tokens.owner, body: { role: 'manager' } })
  assert(promote.json?.code === 0, `promote admin2 failed: ${promote.text}`)
  ctx.admin2Id = admin2Id

  // Base D1/D2
  const baseResp = await req('POST', '/api/v1/bases', { token: tokens.owner, body: { name: `P1沙盒-${RUN}` } })
  const base = apiData(baseResp)?.base
  assert(base?.id, `create base failed: ${baseResp.text}`)
  ctx.baseId = base.id

  const tables = apiData(await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner }))?.table_list ?? []
  const table1 = tables[0]
  assert(table1?.id, 'table1 missing')
  ctx.tableId = table1.id
  const fields0 = apiData(await req('GET', `/api/v1/tables/${table1.id}/fields`, { token: tokens.owner }))?.field_list ?? []
  const primaryField = fields0.find((field) => field.is_primary)
  assert(primaryField?.id, 'primary field missing')

  const table2Resp = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner, body: { name: '目标表' } })
  const table2 = apiData(table2Resp)?.table
  assert(table2?.id, 'table2 missing')
  ctx.table2Id = table2.id
  const table2Fields = apiData(await req('GET', `/api/v1/tables/${table2.id}/fields`, { token: tokens.owner }))?.field_list ?? []
  const table2Primary = table2Fields.find((field) => field.is_primary)
  const targetRecordResp = await req('POST', `/api/v1/bases/${base.id}/records`, {
    token: tokens.owner,
    body: { table_id: table2.id, cells_map: { [table2Primary.id]: '目标记录A' }, client_request_id: `${RUN}-target-1` },
  })
  const targetRecord = apiData(targetRecordResp)?.record
  assert(targetRecord?.id, `target record failed: ${targetRecordResp.text}`)

  async function createField(name, type, config = {}) {
    return req('POST', `/api/v1/tables/${table1.id}/fields`, { token: tokens.owner, body: { name, type, config_map: config } })
  }

  const numberResp = await createField('数量', 'number', { format: 'decimal', decimals: 2, thousands: true })
  const selectResp = await createField('单选', 'select', { option_list: [{ name: '待办', color: 'blue' }, { name: '完成', color: 'green' }], allow_create: false })
  const multiResp = await createField('多选', 'multiselect', { option_list: [{ name: '红', color: 'red' }, { name: '蓝', color: 'blue' }], allow_create: false })
  const dateResp = await createField('日期', 'date', { format: 'YYYY-MM-DD', include_time: true })
  const personResp = await createField('人员', 'person', { allow_multiple: true })
  const attachResp = await createField('附件', 'attachment', {})
  const urlResp = await createField('链接', 'url', {})
  const formulaResp = await createField('公式', 'formula', { expression: 'SUM({数量},10)' })
  const errorFormulaResp = await createField('错误公式', 'formula', { expression: '10/{数量}' })
  const relationResp = await createField('关联', 'relation', { target_table_id: table2.id, allow_multiple: true })
  const relationField = apiData(relationResp)?.field
  const lookupResp = await createField('引用', 'lookup', { relation_field_id: relationField?.id, target_field_id: table2Primary.id })

  const createdFields = [
    ['数量', numberResp], ['单选', selectResp], ['多选', multiResp], ['日期', dateResp],
    ['人员', personResp], ['附件', attachResp], ['链接', urlResp], ['公式', formulaResp],
    ['错误公式', errorFormulaResp], ['关联', relationResp], ['引用', lookupResp],
  ]
  console.log('  field create codes:', createdFields.map(([name, response]) => `${name}=${response.json?.code}`).join(', '))
  const badField = createdFields.find(([, response]) => response.json?.code !== 0)
  assert(!badField, `create field ${badField?.[0]} failed: ${badField?.[1]?.text}`)
  const f = {
    primary: primaryField,
    number: apiData(numberResp)?.field,
    select: apiData(selectResp)?.field,
    multi: apiData(multiResp)?.field,
    date: apiData(dateResp)?.field,
    person: apiData(personResp)?.field,
    attachment: apiData(attachResp)?.field,
    url: apiData(urlResp)?.field,
    formula: apiData(formulaResp)?.field,
    errorFormula: apiData(errorFormulaResp)?.field,
    relation: relationField,
    lookup: apiData(lookupResp)?.field,
  }
  ctx.fields = f

  const selectOptionId = f.select?.config_map?.option_list?.[0]?.id
  const multiOptionIds = (f.multi?.config_map?.option_list ?? []).map((option) => option.id)
  const nowMs = 1730000000000
  const rec1Resp = await req('POST', `/api/v1/bases/${base.id}/records`, {
    token: tokens.owner,
    body: {
      table_id: table1.id,
      cells_map: {
        [f.primary.id]: '记录A',
        [f.number.id]: 10,
        [f.select.id]: selectOptionId,
        [f.multi.id]: multiOptionIds,
        [f.date.id]: nowMs,
        [f.person.id]: [ctx.ownerUserId],
        [f.url.id]: { url: 'https://example.com/a', display_text: '示例A' },
        [f.relation.id]: [targetRecord.id],
      },
      client_request_id: `${RUN}-rec-1`,
    },
  })
  const rec1 = apiData(rec1Resp)?.record
  assert(rec1?.id, `create rec1 failed: ${rec1Resp.text}`)
  ctx.rec1Id = rec1.id

  const rec2Resp = await req('POST', `/api/v1/bases/${base.id}/records`, {
    token: tokens.owner,
    body: { table_id: table1.id, cells_map: { [f.primary.id]: '记录B', [f.number.id]: 2 }, client_request_id: `${RUN}-rec-2` },
  })
  const rec2 = apiData(rec2Resp)?.record
  assert(rec2?.id, `create rec2 failed: ${rec2Resp.text}`)
  ctx.rec2Id = rec2.id

  const views = apiData(await req('GET', `/api/v1/tables/${table1.id}/views`, { token: tokens.owner }))?.view_list ?? []
  const defaultView = views[0]
  assert(defaultView?.id, 'default view missing')
  ctx.viewId = defaultView.id

  // Base 协作者
  const addEditor = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.editor, role: 'editor' } })
  const addFiller = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.filler, role: 'filler' } })
  const addViewer = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.viewer, role: 'viewer' } })
  const addPeer = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.peer, role: 'editor' } })
  const addExt = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.ext, role: 'editor' } })
  for (const [label, r] of [['editor', addEditor], ['filler', addFiller], ['viewer', addViewer], ['peer', addPeer], ['ext', addExt]]) {
    assert(r.json?.code === 0, `add collaborator ${label} failed: ${r.text}`)
  }

  async function getRecord(recordId, token = tokens.owner) {
    const r = await req('GET', `/api/v1/records/${recordId}?base_id=${base.id}`, { token })
    return { response: r, data: apiData(r)?.record, cells: decodeCells(apiData(r)?.record) }
  }

  async function waitRecordValue(recordId, fieldId, expected, timeoutMs = 12000) {
    const deadline = Date.now() + timeoutMs
    let last
    while (Date.now() < deadline) {
      const snapshot = await getRecord(recordId)
      last = snapshot.cells?.[fieldId]
      if (JSON.stringify(last) === JSON.stringify(expected)) return { ok: true, value: last }
      await sleep(500)
    }
    return { ok: false, value: last }
  }

  async function getNotifications(token, limit = 100) {
    const r = await req('GET', `/api/v1/notifications?limit=${limit}`, { token })
    return { response: r, list: apiData(r)?.notification_list ?? [] }
  }

  async function waitNotification(token, predicate, timeoutMs = 15000) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      const { list } = await getNotifications(token)
      const hit = list.find(predicate)
      if (hit) return hit
      await sleep(700)
    }
    return null
  }

  async function getComments(recordId, token = tokens.owner) {
    const r = await req('GET', `/api/v1/records/${recordId}/comments?limit=100`, { token })
    return { response: r, list: apiData(r)?.comment_list ?? [], unsolved: apiData(r)?.unsolved_count }
  }

  // 分享链接（anyone/filler），供 T00/T13 使用
  const linkSet = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'anyone', role: 'filler' } })
  assert(linkSet.json?.code === 0, `set link failed: ${linkSet.text}`)
  const linkUrl = apiData(linkSet)?.link_config?.url ?? ''
  const linkToken = linkUrl.split('/').filter(Boolean).pop()
  ctx.linkUrl = linkUrl
  ctx.linkToken = linkToken

  // ================= T00 =================
  await testCase('TC-000-05', '资产缓存策略', async () => {
    const index = await req('GET', '/')
    const indexCache = index.headers.get('cache-control') || ''
    const html = index.text
    const assetPath = html.match(/(?:src|href)="([^"]*assets\/[^"]+\.(?:js|css))"/)?.[1] ?? ''
    assert(assetPath, `no hashed asset found in index (cache=${indexCache})`)
    const asset = await req('GET', assetPath)
    const assetCache = asset.headers.get('cache-control') || ''
    assert(/no-cache/i.test(indexCache), `index cache-control expected no-cache, got ${indexCache}`)
    assert(/max-age=31536000/i.test(assetCache) && /immutable/i.test(assetCache), `asset cache-control unexpected: ${assetCache}`)
    return `index=${indexCache}; asset=${assetCache}; asset=${assetPath}`
  })

  await testCase('TC-000-06', '分享链接入口（API 侧）', async () => {
    const anon = await req('GET', `/s/${linkToken}`, { redirect: 'manual' })
    const forged = await req('GET', '/s/forged-token-xyz', { redirect: 'manual' })
    assert(anon.status === 302 && (anon.headers.get('location') || '').includes('/register?link_token='), `valid token: ${anon.status} ${anon.headers.get('location')}`)
    assert(forged.status === 302 && (forged.headers.get('location') || '').includes('/err?kind=share-link-unavailable'), `forged token: ${forged.status} ${forged.headers.get('location')}`)
    return `valid=302 register; forged=302 err; token=${linkToken.slice(0, 8)}…`
  })

  await testCase('TC-000-07', 'WS 握手合法/非法', async () => {
    const valid = await wsConnect(tokens.owner, base.id, table1.id)
    const rejected = await wsExpectRejected(tokens.owner, 'base-not-exist-xyz', table1.id)
    valid.close()
    assert(rejected.rejected, `invalid base handshake not rejected: ${JSON.stringify(rejected)}`)
    return `valid=101; invalid=${JSON.stringify(rejected)}`
  })

  // ================= T01 =================
  await testCase('TC-010-06', '连续失败锁定', async () => {
    const lockUser = `p1lk${RUN}`
    const lockReg = await register(lockUser, { enterprise_name: `P1锁${RUN.slice(-3)}` })
    assert(lockReg.json?.code === 0, `lock user register failed: ${lockReg.text}`)
    let firstRound = []
    let secondRound = []
    for (let i = 0; i < 6; i += 1) {
      const attempt = await req('POST', '/api/v1/auth/login', { body: { username: lockUser, password: 'Wrong1234' } })
      firstRound.push({ code: attempt.json?.code, message: attempt.json?.message })
    }
    const hitRateLimit = firstRound.some((item) => item.code === 5003)
    if (hitRateLimit) {
      console.log('  login rate limited during lockout test, wait 65s and retry')
      await sleep(65000)
      for (let i = 0; i < 3; i += 1) {
        const attempt = await req('POST', '/api/v1/auth/login', { body: { username: lockUser, password: 'Wrong1234' } })
        secondRound.push({ code: attempt.json?.code, message: attempt.json?.message })
      }
    }
    const firstFive = firstRound.slice(0, 5).every((item) => item.message === '用户名或密码错误')
    const locked = firstRound[5]?.message === '失败次数过多，请稍后再试' ||
      (hitRateLimit && secondRound.length > 0 && secondRound.every((item) => item.message === '失败次数过多，请稍后再试'))
    assert(firstFive && locked, `first=${JSON.stringify(firstRound)}, second=${JSON.stringify(secondRound)}`)
    ctx.lockUser = lockUser
    return `前 5 次=用户名或密码错误；随后=失败次数过多${hitRateLimit ? '（含登录限速等待后复核）' : ''}`
  })

  await testCase('TC-010-09', '企业邀请注册（API 侧重放）', async () => {
    // 已在 setup 中完成 invitee 邀请/接受；这里复核企业成员列表与收件箱语义
    const members = apiData(await req('GET', '/api/v1/enterprise/members?limit=100', { token: tokens.owner }))?.member_list ?? []
    const joined = members.some((member) => member.username === names.invitee)
    const invites = await req('GET', '/api/v1/enterprise/invites?limit=100', { token: tokens.owner })
    assert(joined && invites.json?.code === 0, `invitee joined=${joined}, invites code=${invites.json?.code}`)
    return `invitee 已入企业；成员数=${members.length}`
  })

  await testCase('TC-010-10', '分享链接注册（API 侧）', async () => {
    const linkUser = `p1lnk${RUN}`
    const linkReg = await register(linkUser, { link_token: linkToken })
    assert(linkReg.json?.code === 0, `link register failed: ${linkReg.text}`)
    const linkSession = linkReg.json?.data?.token
    assert(linkSession, 'link register token missing')
    const access = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSession })
    assert(access.json?.code === 0, `link session access failed: ${access.text}`)
    ctx.linkUser = linkUser
    ctx.linkSession = linkSession
    return `注册=0；链接会话读表=0；用户=${linkUser}`
  })

  await testCase('TC-010-11', '改密（API 侧）', async () => {
    const change = await req('POST', '/api/v1/me/password', { token: tokens.invitee, body: { old_password: PWD, new_password: 'NewPass1234' } })
    assert(change.json?.code === 0, `change password failed: ${change.text}`)
    const oldLogin = await req('POST', '/api/v1/auth/login', { body: { username: names.invitee, password: PWD } })
    assert(oldLogin.json?.code !== 0, `old password still works: ${oldLogin.text}`)
    tokens.invitee = await login(names.invitee, 'NewPass1234')
    await req('POST', '/api/v1/me/password', { token: tokens.invitee, body: { old_password: 'NewPass1234', new_password: PWD } })
    tokens.invitee = await login(names.invitee)
    return `旧会话改密后旧密码登录被拒，已恢复原密码`
  })

  await testCase('TC-010-12', '改显示名（API 侧）', async () => {
    const update = await req('PUT', '/api/v1/me/profile', { token: tokens.invitee, body: { display_name: 'P1显示名' } })
    assert(update.json?.code === 0, `update profile failed: ${update.text}`)
    const me = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.invitee }))
    assert(me?.user?.display_name === 'P1显示名', `display_name=${me?.user?.display_name}`)
    return `display_name=${me?.user?.display_name}`
  })

  await testCase('TC-010-13', '注销冷静期与撤回', async () => {
    const target = `p1del${RUN}`
    const delReg = await register(target, { enterprise_name: `P1注销${RUN.slice(-3)}` })
    assert(delReg.json?.code === 0, `deactivate user register failed: ${delReg.text}`)
    const delToken = await login(target)
    const request = await req('POST', '/api/v1/me/deactivate', { token: delToken })
    const deadline = apiData(request)?.deadline_ms ?? apiData(request)?.deadline
    assert(request.json?.code === 0 && deadline !== undefined, `request deactivate failed: ${request.text}`)
    const cancel = await req('POST', '/api/v1/me/deactivate/cancel', { token: delToken })
    assert(cancel.json?.code === 0, `cancel deactivate failed: ${cancel.text}`)
    const after = await req('POST', '/api/v1/auth/login', { body: { username: target, password: PWD } })
    assert(after.json?.code === 0, `login after cancel failed: ${after.text}`)
    return `deadline=${JSON.stringify(deadline)}；撤回后可登录`
  })

  // ================= T02 =================
  await testCase('TC-020-05', 'Base 搜索', async () => {
    const hit = await req('GET', `/api/v1/bases?q=${encodeURIComponent('P1沙盒')}`, { token: tokens.owner })
    const miss = await req('GET', `/api/v1/bases?q=${encodeURIComponent('绝不存在的名字xyz')}`, { token: tokens.owner })
    const hitIds = (apiData(hit)?.base_list ?? []).map((item) => item.id)
    const missIds = (apiData(miss)?.base_list ?? []).map((item) => item.id)
    assert(hit.json?.code === 0 && hitIds.includes(base.id), `search hit missing: ${hit.text}`)
    assert(miss.json?.code === 0 && !missIds.includes(base.id), `search miss included base: ${miss.text}`)
    return `命中=${hitIds.length}, 未命中=${missIds.length}`
  })

  await testCase('TC-020-06', '重命名/星标', async () => {
    const newName = `P1沙盒改名-${RUN.slice(-4)}`
    const rename = await req('PATCH', `/api/v1/bases/${base.id}`, { token: tokens.owner, body: { name: newName } })
    assert(rename.json?.code === 0 && apiData(rename)?.base?.name === newName, `rename failed: ${rename.text}`)
    const star = await req('POST', `/api/v1/bases/${base.id}/star`, { token: tokens.owner, body: { starred: true } })
    assert(star.json?.code === 0 && apiData(star)?.base?.starred === true, `star failed: ${star.text}`)
    const home = apiData(await req('GET', '/api/v1/home', { token: tokens.owner }))
    const starred = (home?.star_list ?? []).some((item) => item.id === base.id)
    assert(starred, 'base missing in star_list')
    await req('POST', `/api/v1/bases/${base.id}/star`, { token: tokens.owner, body: { starred: false } })
    return `rename/star/unstar 均生效；星标列表命中`
  })

  await testCase('TC-020-07', '复制 Base', async () => {
    // 源 Base 上留一条评论与一条编辑历史，复制后不应带走
    const comment = await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.owner, body: { content: '复制前评论', mention_list: [] } })
    assert(comment.json?.code === 0, `source comment failed: ${comment.text}`)
    const edit = await req('PUT', `/api/v1/records/${rec1.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.owner, body: { value: 11, expect_version: Number(rec1.version) },
    })
    assert(edit.json?.code === 0, `source edit failed: ${edit.text}`)

    const dup = await req('POST', `/api/v1/bases/${base.id}/duplicate`, { token: tokens.owner })
    const newBase = apiData(dup)?.base
    assert(dup.json?.code === 0 && newBase?.id, `duplicate base failed: ${dup.text}`)
    const dupTables = apiData(await req('GET', `/api/v1/bases/${newBase.id}/tables`, { token: tokens.owner }))?.table_list ?? []
    // 源表数按复制时刻取值：套件启动时的 tables 快照早于「目标表」创建，是陈旧值。
    const srcTablesAtDup = apiData(await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner }))?.table_list ?? []
    assert(dupTables.length === srcTablesAtDup.length, `tables=${dupTables.length}, want ${srcTablesAtDup.length}`)
    const dupFields = apiData(await req('GET', `/api/v1/tables/${dupTables[0].id}/fields`, { token: tokens.owner }))?.field_list ?? []
    assert(dupFields.length === fields0.length + 11, `fields=${dupFields.length}`)
    const dupViews = apiData(await req('GET', `/api/v1/tables/${dupTables[0].id}/views`, { token: tokens.owner }))?.view_list ?? []
    const dupRecords = apiData(await req('GET', `/api/v1/bases/${newBase.id}/records?table_id=${dupTables[0].id}&view_id=${dupViews[0]?.id}&limit=100`, { token: tokens.owner }))?.record_list ?? []
    const dupPrimary = dupFields.find((field) => field.is_primary)
    const dupRecA = dupRecords.find((row) => decodeCells(row)[dupPrimary.id] === '记录A')
    assert(dupRecA, 'copied record A missing')
    const dupNumberField = dupFields.find((field) => field.name === '数量')
    assert(Number(decodeCells(dupRecA)[dupNumberField.id]) === 11, `copied value=${decodeCells(dupRecA)[dupNumberField.id]}`)
    const dupComments = apiData(await req('GET', `/api/v1/records/${dupRecA.id}/comments?limit=100`, { token: tokens.owner }))?.comment_list ?? []
    assert(dupComments.length === 0, `comments copied: ${dupComments.length}`)
    const dupHistory = apiData(await req('GET', `/api/v1/records/${dupRecA.id}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    const editNodes = dupHistory.filter((node) => node.before !== undefined && node.before !== null)
    assert(editNodes.length === 0, `history edit nodes copied: ${editNodes.length}`)
    ctx.dupBaseId = newBase.id
    return `结构+数据复制；评论=0；编辑历史节点=0；表=${dupTables.length} 字段=${dupFields.length} 记录=${dupRecords.length}`
  })

  // ================= T03 =================
  await testCase('TC-030-04', '复制表', async () => {
    // 在 table2 里补一条评论后复制
    const comment = await req('POST', `/api/v1/records/${targetRecord.id}/comments`, { token: tokens.owner, body: { content: '表复制前评论', mention_list: [] } })
    assert(comment.json?.code === 0, `table source comment failed: ${comment.text}`)
    const dup = await req('POST', `/api/v1/tables/${table2.id}/duplicate`, { token: tokens.owner })
    const newTable = apiData(dup)?.table
    assert(dup.json?.code === 0 && newTable?.id, `duplicate table failed: ${dup.text}`)
    const dupFields = apiData(await req('GET', `/api/v1/tables/${newTable.id}/fields`, { token: tokens.owner }))?.field_list ?? []
    const dupViews = apiData(await req('GET', `/api/v1/tables/${newTable.id}/views`, { token: tokens.owner }))?.view_list ?? []
    const dupRecords = apiData(await req('GET', `/api/v1/bases/${base.id}/records?table_id=${newTable.id}&view_id=${dupViews[0]?.id}&limit=100`, { token: tokens.owner }))?.record_list ?? []
    assert(dupFields.length === table2Fields.length && dupViews.length === 1 && dupRecords.length === 1, `fields=${dupFields.length}, views=${dupViews.length}, records=${dupRecords.length}`)
    const dupComments = apiData(await req('GET', `/api/v1/records/${dupRecords[0].id}/comments?limit=100`, { token: tokens.owner }))?.comment_list ?? []
    assert(dupComments.length === 0, `table comments copied: ${dupComments.length}`)
    await req('DELETE', `/api/v1/tables/${newTable.id}`, { token: tokens.owner, body: { confirm: true } })
    return `字段=${dupFields.length}, 视图=${dupViews.length}, 记录=${dupRecords.length}, 评论=0`
  })

  await testCase('TC-030-06', '重命名表（API 侧）', async () => {
    const rename = await req('PATCH', `/api/v1/tables/${table2.id}`, { token: tokens.owner, body: { name: '目标表-改名' } })
    assert(rename.json?.code === 0 && apiData(rename)?.table?.name === '目标表-改名', `rename table failed: ${rename.text}`)
    const list = apiData(await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner }))?.table_list ?? []
    assert(list.some((item) => item.id === table2.id && item.name === '目标表-改名'), 'renamed table missing in list')
    return `列表已反映新名称`
  })

  // ================= T04 =================
  await testCase('TC-040-17', '类型转换禁区', async () => {
    const formulaToText = await req('POST', `/api/v1/fields/${f.formula.id}/convert`, { token: tokens.owner, body: { target_type: 'text', confirm: true } })
    const relationToLookup = await req('POST', `/api/v1/fields/${f.relation.id}/convert`, { token: tokens.owner, body: { target_type: 'lookup', config_map: { relation_field_id: f.relation.id, target_field_id: f.primary.id }, confirm: true } })
    const lookupToRelation = await req('POST', `/api/v1/fields/${f.lookup.id}/convert`, { token: tokens.owner, body: { target_type: 'relation', config_map: { target_table_id: table2.id }, confirm: true } })
    const list = apiData(await req('GET', `/api/v1/tables/${table1.id}/fields`, { token: tokens.owner }))?.field_list ?? []
    const typeOf = (id) => list.find((field) => field.id === id)?.type
    const unchanged = enumText(typeOf(f.formula.id)).includes('formula') && enumText(typeOf(f.relation.id)).includes('relation') && enumText(typeOf(f.lookup.id)).includes('lookup')
    assert(formulaToText.json?.code !== 0 && relationToLookup.json?.code !== 0 && lookupToRelation.json?.code !== 0 && unchanged,
      `codes=${formulaToText.json?.code}/${relationToLookup.json?.code}/${lookupToRelation.json?.code}, unchanged=${unchanged}`)
    return `公式→文本=${formulaToText.json?.code}, 关联→引用=${relationToLookup.json?.code}, 引用→关联=${lookupToRelation.json?.code}；类型未变`
  })

  await testCase('TC-040-19', '复制字段', async () => {
    const dup = await req('POST', `/api/v1/fields/${f.number.id}/duplicate`, { token: tokens.owner })
    const copied = apiData(dup)?.field
    assert(dup.json?.code === 0 && copied?.id, `duplicate field failed: ${dup.text}`)
    assert(Number(copied.config_map?.decimals) === 2, `config not copied: ${JSON.stringify(copied.config_map)}`)
    const row = await getRecord(rec1.id)
    assert(row.cells?.[copied.id] === null || row.cells?.[copied.id] === undefined, `copied field has data: ${row.cells?.[copied.id]}`)
    ctx.copiedFieldId = copied.id
    return `配置一致；数据为空；新字段=${copied.id}`
  })

  await testCase('TC-040-20', '字段描述', async () => {
    const created = await req('POST', `/api/v1/tables/${table1.id}/fields`, {
      token: tokens.owner,
      body: { name: '带描述字段', type: 'text', config_map: {}, description: '这是字段描述' },
    })
    const field = apiData(created)?.field
    assert(created.json?.code === 0 && field?.description === '这是字段描述', `create with description failed: ${created.text}`)
    const list = apiData(await req('GET', `/api/v1/tables/${table1.id}/fields`, { token: tokens.owner }))?.field_list ?? []
    assert(list.find((item) => item.id === field.id)?.description === '这是字段描述', 'description missing in list')
    return `description=${field.description}`
  })

  await testCase('TC-040-21', '选项同值归一', async () => {
    const optionList = (f.select?.config_map?.option_list ?? []).map((option) => ({ id: option.id, name: option.name, color: option.color }))
    const before = optionList.length
    // 基线：不带重复名称的全量写回应成功
    const baseline = await req('PUT', `/api/v1/fields/${f.select.id}/options`, {
      token: tokens.owner,
      body: { option_list: optionList, allow_create: false },
    })
    assert(baseline.json?.code === 0, `baseline option update failed: ${baseline.text}`)
    const update = await req('PUT', `/api/v1/fields/${f.select.id}/options`, {
      token: tokens.owner,
      body: { option_list: [...optionList, { name: ' 待办 ', color: 'red' }], allow_create: false },
    })
    assert(update.json?.code === 0, `duplicate-name option update rejected: ${update.text}`)
    const after = apiData(update)?.field?.config_map?.option_list ?? []
    assert(after.length === before, `options duplicated: ${before} -> ${after.length}`)
    return `选项数保持 ${before}（重复名称未新增）`
  })

  // ================= T05 =================
  await testCase('TC-050-08', '复制记录（H-49+H-50 组合）', async () => {
    await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.owner, body: { content: '记录复制前评论', mention_list: [] } })
    const source = await getRecord(rec1.id)
    assert(source.response.json?.code === 0, `read source failed: ${source.response.text}`)
    const writableEntries = Object.entries(source.cells ?? {}).filter(([fieldId]) => {
      const field = Object.values(f).find((candidate) => candidate?.id === fieldId)
      if (!field) return false
      const type = enumText(field.type)
      return !type.includes('formula') && !type.includes('lookup')
    })
    const cellsMap = Object.fromEntries(writableEntries)
    const created = await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner,
      body: { table_id: table1.id, cells_map: cellsMap, client_request_id: `${RUN}-copy-rec` },
    })
    const copy = apiData(created)?.record
    assert(created.json?.code === 0 && copy?.id, `copy record failed: ${created.text}`)
    const copySnapshot = await getRecord(copy.id)
    for (const [fieldId, expected] of writableEntries) {
      assert(JSON.stringify(copySnapshot.cells?.[fieldId]) === JSON.stringify(expected), `field ${fieldId} mismatch: ${JSON.stringify(copySnapshot.cells?.[fieldId])} != ${JSON.stringify(expected)}`)
    }
    const comments = await getComments(copy.id)
    assert(comments.list.length === 0, `comments copied: ${comments.list.length}`)
    ctx.copiedRecordId = copy.id
    return `可写字段 ${writableEntries.length} 个值一致；评论=0`
  })

  await testCase('TC-050-13', '批量编辑幂等', async () => {
    const snapshot = await getRecord(rec2.id)
    const version = Number(snapshot.data?.version ?? 0)
    const value = 77
    const body = { base_id: base.id, op_list: [{ record_id: rec2.id, field_id: f.number.id, value, expect_version: version }], client_request_id: `${RUN}-batch-idem` }
    const first = await req('POST', '/api/v1/records/batch', { token: tokens.owner, body })
    const second = await req('POST', '/api/v1/records/batch', { token: tokens.owner, body })
    const after = await getRecord(rec2.id)
    const history = apiData(await req('GET', `/api/v1/records/${rec2.id}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    const nodes = history.filter((node) => node.field_id === f.number.id && Number(decodeValue(node.after)) === value)
    assert(first.json?.code === 0 && second.json?.code === 0 && Number(after.data?.version) === version + 1 && nodes.length === 1,
      `first=${first.json?.code}, second=${second.json?.code}, version=${snapshot.data?.version}->${after.data?.version}, nodes=${nodes.length}`)
    return `两次调用成功；版本仅 +1；历史节点=1`
  })

  await testCase('TC-050-14', '乐观锁正常链路', async () => {
    const before = await getRecord(rec1.id)
    const version = Number(before.data?.version ?? 0)
    const update = await req('PUT', `/api/v1/records/${rec1.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.owner, body: { value: 33, expect_version: version },
    })
    assert(update.json?.code === 0 && Number(apiData(update)?.version) === version + 1, `update failed: ${update.text}`)
    const peerRead = await getRecord(rec1.id, tokens.peer)
    assert(peerRead.response.json?.code === 0 && Number(peerRead.cells?.[f.number.id]) === 33, `peer read code=${peerRead.response.json?.code}, value=${JSON.stringify(peerRead.cells?.[f.number.id])}, body=${peerRead.response.text.slice(0, 160)}`)
    return `version ${version}->${version + 1}；协作者读回 33`
  })

  // 恢复 rec1 数量为 11，避免影响后续公式/历史断言
  await req('PUT', `/api/v1/records/${rec1.id}/cells/${f.number.id}?base_id=${base.id}`, {
    token: tokens.owner, body: { value: 11, expect_version: Number((await getRecord(rec1.id)).data?.version ?? 0) },
  })

  // ================= T08 =================
  await testCase('TC-080-06', '外部角色封顶', async () => {
    const attempt = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.extViewer, role: 'manager' } })
    assert(attempt.json?.code !== 0, `external manager accepted: ${attempt.text}`)
    return `外部授可管理被拒：code=${attempt.json?.code}`
  })

  await testCase('TC-080-07', '权限降级即时生效', async () => {
    const writeBefore = await req('PUT', `/api/v1/records/${rec2.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.editor, body: { value: 21, expect_version: Number((await getRecord(rec2.id)).data?.version ?? 0) },
    })
    assert(writeBefore.json?.code === 0, `editor write before downgrade failed: ${writeBefore.text}`)
    // 用协作者条目 id（=user_id）降级
    const editorUserId = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.editor }))?.user?.id
    const roleChange = await req('PUT', `/api/v1/shares/members/${editorUserId}/role`, { token: tokens.owner, body: { base_id: base.id, role: 'viewer' } })
    assert(roleChange.json?.code === 0, `downgrade failed: ${roleChange.text}`)
    let denied = null
    const deadline = Date.now() + 70000
    while (Date.now() < deadline) {
      const probe = await req('PUT', `/api/v1/records/${rec2.id}/cells/${f.number.id}?base_id=${base.id}`, {
        token: tokens.editor, body: { value: 22, expect_version: Number((await getRecord(rec2.id)).data?.version ?? 0) },
      })
      if (probe.json?.code === 3001) { denied = probe.json?.code; break }
      await sleep(5000)
    }
    // 恢复 editor 角色，避免影响后续用例
    await req('PUT', `/api/v1/shares/members/${editorUserId}/role`, { token: tokens.owner, body: { base_id: base.id, role: 'editor' } })
    assert(denied === 3001, `downgraded write not denied within 70s: ${JSON.stringify(denied)}`)
    return `降级后写请求 3001（缓存窗口内生效）`
  })

  await testCase('TC-080-08', '可填写评论范围', async () => {
    const other = await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.filler, body: { content: 'filler 评论他人记录', mention_list: [] } })
    assert(other.json?.code !== 0, `filler commented others record: ${other.text}`)
    const ownRecord = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.filler, body: { table_id: table1.id, cells_map: { [f.primary.id]: 'filler 自己的记录' } },
    }))?.record
    assert(ownRecord?.id, 'filler own record create failed')
    const own = await req('POST', `/api/v1/records/${ownRecord.id}/comments`, { token: tokens.filler, body: { content: '评论自己的记录', mention_list: [] } })
    assert(own.json?.code === 0, `filler comment own record failed: ${own.text}`)
    return `他人记录评论被拒 code=${other.json?.code}；自己记录评论成功`
  })

  await testCase('TC-080-09', '可查看动态可见性', async () => {
    const history = await req('GET', `/api/v1/records/${rec1.id}/history?limit=100`, { token: tokens.viewer })
    const list = apiData(history)?.history_list ?? []
    assert(history.json?.code !== 0 || list.length === 0, `viewer saw history: code=${history.json?.code}, count=${list.length}`)
    return `viewer 历史读取：code=${history.json?.code}, 条数=${list.length}`
  })

  await testCase('TC-080-10', '仅剩可管理者自我保护', async () => {
    const ownerId = ctx.ownerUserId
    const demote = await req('PUT', `/api/v1/shares/members/${ownerId}/role`, { token: tokens.owner, body: { base_id: base.id, role: 'viewer' } })
    const remove = await req('DELETE', `/api/v1/shares/members/${ownerId}`, { token: tokens.owner, body: { base_id: base.id } })
    assert(demote.json?.code !== 0 && remove.json?.code !== 0, `self-protection failed: demote=${demote.json?.code}, remove=${remove.json?.code}`)
    return `自降/自移除均被拒：${demote.json?.code}/${remove.json?.code}`
  })

  // ================= T09 WS =================
  await testCase('TC-090-03', 'presence 进出', async () => {
    const a1 = await wsConnect(tokens.owner, base.id, table1.id)
    await a1.ws.send(JSON.stringify({ msg_id: 'p3-j1', join: { base_id: base.id, table_id: table1.id } }))
    await a1.waitFrame((frame) => frame.snapshot !== undefined)
    const a8 = await wsConnect(tokens.peer, base.id, table1.id)
    await a8.ws.send(JSON.stringify({ msg_id: 'p3-j2', join: { base_id: base.id, table_id: table1.id } }))
    await a8.waitFrame((frame) => frame.snapshot !== undefined)
    await a1.waitFrame((frame) => frame.presence?.online === true, 5000)
    a8.close()
    const offline = await a1.waitFrame((frame) => frame.presence?.online === false, 6000).catch(() => null)
    a1.close()
    assert(offline, 'no presence offline frame')
    return `离场帧 user=${offline.presence.user_id}, online=false`
  })

  await testCase('TC-090-04', '选区/编辑帧', async () => {
    const a1 = await wsConnect(tokens.owner, base.id, table1.id)
    const a8 = await wsConnect(tokens.peer, base.id, table1.id)
    await a1.ws.send(JSON.stringify({ msg_id: 'p4-j1', join: { base_id: base.id, table_id: table1.id } }))
    await a8.ws.send(JSON.stringify({ msg_id: 'p4-j2', join: { base_id: base.id, table_id: table1.id } }))
    await a1.waitFrame((frame) => frame.snapshot !== undefined)
    await a8.waitFrame((frame) => frame.snapshot !== undefined)
    await a1.ws.send(JSON.stringify({ msg_id: 'p4-c', cursor: { record_id: rec1.id, field_id: f.number.id } }))
    const cursor = await a8.waitFrame((frame) => frame.cursor_echo?.record_id === rec1.id && frame.cursor_echo?.field_id === f.number.id, 4000).catch(() => null)
    await a1.ws.send(JSON.stringify({ msg_id: 'p4-e', editing: { record_id: rec1.id, field_id: f.number.id, on: true } }))
    const editing = await a8.waitFrame((frame) => frame.cursor_echo?.range === 'editing:on', 4000).catch(() => null)
    a1.close(); a8.close()
    assert(cursor && editing, `cursor=${JSON.stringify(cursor)}, editing=${JSON.stringify(editing)}`)
    return `cursor_echo 与 editing:on 均到达`
  })

  await testCase('TC-090-05', '结构广播', async () => {
    const a8 = await wsConnect(tokens.peer, base.id, table1.id)
    await a8.ws.send(JSON.stringify({ msg_id: 'p5-j', join: { base_id: base.id, table_id: table1.id } }))
    await a8.waitFrame((frame) => frame.snapshot !== undefined)
    const created = await createField('WS结构字段', 'text', {})
    const field = apiData(created)?.field
    const structure = await a8.waitFrame((frame) => frame.structure_changed?.field_id === field?.id, 6000).catch(() => null)
    const kinds = [...new Set(a8.frames.map((frame) => Object.keys(frame).filter((key) => key !== 'msg_id')[0]))]
    a8.close()
    assert(created.json?.code === 0 && structure, `create=${created.json?.code}, frame=${JSON.stringify(structure)}, kinds=${JSON.stringify(kinds)}`)
    return `structure_changed seq=${structure.structure_changed.seq}, table=${structure.structure_changed.table_id}, field=${structure.structure_changed.field_id}`
  })

  await testCase('TC-090-06', '行删除广播', async () => {
    const a8 = await wsConnect(tokens.peer, base.id, table1.id)
    await a8.ws.send(JSON.stringify({ msg_id: 'p6-j', join: { base_id: base.id, table_id: table1.id } }))
    await a8.waitFrame((frame) => frame.snapshot !== undefined)
    const temp = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner, body: { table_id: table1.id, cells_map: { [f.primary.id]: '待删广播记录' } },
    }))?.record
    const removed = await req('DELETE', `/api/v1/records?base_id=${base.id}`, { token: tokens.owner, body: { record_id_list: [temp.id] } })
    const frame = await a8.waitFrame((f) => f.record_deleted?.record_id === temp.id, 6000).catch(() => null)
    a8.close()
    assert(removed.json?.code === 0 && frame, `delete=${removed.json?.code}, frame=${JSON.stringify(frame)}`)
    return `record_deleted seq=${frame.record_deleted.seq}`
  })

  await testCase('TC-090-07', '断线重连对账', async () => {
    const a8 = await wsConnect(tokens.peer, base.id, table1.id)
    await a8.ws.send(JSON.stringify({ msg_id: 'p7-j1', join: { base_id: base.id, table_id: table1.id } }))
    const snapshot = await a8.waitFrame((frame) => frame.snapshot !== undefined)
    const lastSeq = Number(snapshot.snapshot?.seq ?? 0)
    a8.close()
    await sleep(500)
    const changes = []
    for (const value of [41, 42, 43]) {
      const current = await getRecord(rec2.id)
      const update = await req('PUT', `/api/v1/records/${rec2.id}/cells/${f.number.id}?base_id=${base.id}`, {
        token: tokens.owner, body: { value, expect_version: Number(current.data?.version ?? 0) },
      })
      assert(update.json?.code === 0, `update ${value} failed: ${update.text}`)
      changes.push(value)
    }
    const reconnect = await wsConnect(tokens.peer, base.id, table1.id)
    await reconnect.ws.send(JSON.stringify({ msg_id: 'p7-j2', join: { base_id: base.id, table_id: table1.id, last_seq: lastSeq } }))
    await reconnect.waitFrame((frame) => frame.snapshot !== undefined, 5000).catch(() => null)
    let replayed = reconnect.frames.filter((frame) => frame.cell_changed && Number(frame.cell_changed.seq) > lastSeq)
    if (replayed.length < 3) {
      await reconnect.ws.send(JSON.stringify({ msg_id: 'p7-r', replay_request: { last_seq: lastSeq } }))
      await sleep(1500)
      replayed = reconnect.frames.filter((frame) => frame.cell_changed && Number(frame.cell_changed.seq) > lastSeq)
    }
    const seqs = replayed.map((frame) => Number(frame.cell_changed.seq))
    const values = replayed.map((frame) => decodeValue(frame.cell_changed.value))
    const kinds = [...new Set(reconnect.frames.map((frame) => Object.keys(frame).filter((key) => key !== 'msg_id')[0]))]
    reconnect.close()
    const uniqueSorted = [...new Set(seqs)].sort((a, b) => a - b)
    assert(replayed.length >= 3 && uniqueSorted.length === seqs.length && changes.every((value) => values.includes(value)),
      `replayed=${JSON.stringify(seqs)}, values=${JSON.stringify(values)}, kinds=${JSON.stringify(kinds)}`)
    return `last_seq=${lastSeq}; 补齐 ${replayed.length} 帧；seq=${JSON.stringify(uniqueSorted)}；值=${JSON.stringify(values)}`
  })

  await testCase('TC-090-10', '通知帧', async () => {
    const a1 = await wsConnect(tokens.owner, base.id, table1.id)
    await a1.ws.send(JSON.stringify({ msg_id: 'p10-j', join: { base_id: base.id, table_id: table1.id } }))
    await a1.waitFrame((frame) => frame.snapshot !== undefined)
    const comment = await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.peer, body: { content: '@owner 提醒', mention_list: [ctx.ownerUserId] } })
    const frame = await a1.waitFrame((f) => f.notification !== undefined, 8000).catch(() => null)
    a1.close()
    assert(comment.json?.code === 0 && frame, `comment=${comment.json?.code}, frame=${JSON.stringify(frame)}`)
    return `notification type=${frame.notification.type}, base=${frame.notification.base_id}`
  })

  await testCase('TC-090-09', '心跳', async () => {
    const a1 = await wsConnect(tokens.owner, base.id, table1.id)
    await a1.ws.send(JSON.stringify({ msg_id: 'p9-j', join: { base_id: base.id, table_id: table1.id } }))
    await a1.waitFrame((frame) => frame.snapshot !== undefined)
    await a1.ws.send(JSON.stringify({ msg_id: 'p9-hb', heartbeat: {} }))
    const ack = await a1.waitFrame((frame) => frame.ack?.msg_id === 'p9-hb', 4000).catch(() => null)
    a1.close()
    assert(ack, 'heartbeat ack missing')
    return `ack.msg_id=${ack.ack.msg_id}`
  })

  // ================= T10 =================
  await testCase('TC-100-03', '对象已删 4002', async () => {
    const temp = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner, body: { table_id: table1.id, cells_map: { [f.primary.id]: '待删4002' } },
    }))?.record
    await req('DELETE', `/api/v1/records?base_id=${base.id}`, { token: tokens.owner, body: { record_id_list: [temp.id] } })
    const update = await req('PUT', `/api/v1/records/${temp.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.owner, body: { value: 1, expect_version: 0 },
    })
    assert(update.json?.code === 4002, `code=${update.json?.code}, body=${update.text}`)
    return `已删记录更新返回 4002`
  })

  await testCase('TC-100-04', '服务端冲突 9002', async () => {
    throw new NotRun('无法从黑盒 HTTP 边界构造服务端保存冲突（9002 为内部兜底路径），需单元/故障注入验证')
  })

  await testCase('TC-100-05', '冲突后正确值可读', async () => {
    const snapshot = await getRecord(rec1.id)
    const version = Number(snapshot.data?.version ?? 0)
    const first = await req('PUT', `/api/v1/records/${rec1.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.owner, body: { value: 55, expect_version: version },
    })
    const second = await req('PUT', `/api/v1/records/${rec1.id}/cells/${f.number.id}?base_id=${base.id}`, {
      token: tokens.peer, body: { value: 66, expect_version: version },
    })
    const after = await getRecord(rec1.id)
    assert(first.json?.code === 0 && second.json?.code === 4001 && Number(after.cells?.[f.number.id]) === 55 && Number(after.data?.version) === version + 1,
      `first=${first.json?.code}, second=${second.json?.code}, value=${after.cells?.[f.number.id]}, version=${after.data?.version}`)
    return `冲突后读回获胜值 55 / version=${after.data?.version}`
  })

  // ================= T11 评论与动态 =================
  await testCase('TC-110-01', '两级评论', async () => {
    const top = await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.owner, body: { content: '一级评论', mention_list: [] } })
    const topComment = apiData(top)?.comment
    assert(top.json?.code === 0 && topComment?.id, `top comment failed: ${top.text}`)
    const reply = await req('POST', `/api/v1/comments/${topComment.id}/replies`, { token: tokens.owner, body: { content: '二级回复', mention_list: [] } })
    const replyComment = apiData(reply)?.comment
    assert(reply.json?.code === 0 && replyComment?.id, `reply failed: ${reply.text}`)
    const third = await req('POST', `/api/v1/comments/${replyComment.id}/replies`, { token: tokens.owner, body: { content: '三级回复', mention_list: [] } })
    assert(third.json?.code !== 0, `third level accepted: ${third.text}`)
    const list = await getComments(rec1.id)
    assert(list.list.filter((item) => item.id === topComment.id || item.id === replyComment.id).length >= 2, 'two levels missing in list')
    return `二级成功；三级被拒 code=${third.json?.code}`
  })

  await testCase('TC-110-02', '@提及通知', async () => {
    const mention = await req('POST', `/api/v1/records/${rec1.id}/comments`, {
      token: tokens.owner, body: { content: `@${names.peer} 请查看`, mention_list: [ctx.peerUserId] },
    })
    assert(mention.json?.code === 0, `mention comment failed: ${mention.text}`)
    const notification = await waitNotification(tokens.peer, (item) => item.type === 'comment.mentioned', 15000)
    assert(notification, 'mention notification missing')
    return `peer 收到 comment.mentioned；notification=${notification.id}`
  })

  await testCase('TC-110-03', '解决/重开', async () => {
    const created = await req('POST', `/api/v1/records/${rec2.id}/comments`, { token: tokens.owner, body: { content: '待解决评论', mention_list: [] } })
    const comment = apiData(created)?.comment
    assert(created.json?.code === 0 && comment?.id, `create comment failed: ${created.text}`)
    const before = await getComments(rec2.id)
    const resolve = await req('POST', `/api/v1/comments/${comment.id}/resolve`, { token: tokens.owner, body: {} })
    const resolvedStatus = enumText(apiData(resolve)?.comment?.status)
    const afterResolve = await getComments(rec2.id)
    const reopen = await req('POST', `/api/v1/comments/${comment.id}/reopen`, { token: tokens.owner, body: {} })
    const reopenedStatus = enumText(apiData(reopen)?.comment?.status)
    const afterReopen = await getComments(rec2.id)
    assert(resolve.json?.code === 0 && resolvedStatus.includes('resolved') && reopen.json?.code === 0 && reopenedStatus.includes('open'), `status=${resolvedStatus}/${reopenedStatus}`)
    assert(Number(afterResolve.unsolved) === Number(before.unsolved) - 1 && Number(afterReopen.unsolved) === Number(before.unsolved),
      `unsolved ${before.unsolved}->${afterResolve.unsolved}->${afterReopen.unsolved}`)
    return `resolved→open；unsolved ${before.unsolved}→${afterResolve.unsolved}→${afterReopen.unsolved}`
  })

  await testCase('TC-110-04', '删除评论', async () => {
    const own = await req('POST', `/api/v1/records/${rec2.id}/comments`, { token: tokens.peer, body: { content: 'peer 自删评论', mention_list: [] } })
    const ownComment = apiData(own)?.comment
    const deleteOwn = await req('DELETE', `/api/v1/comments/${ownComment.id}`, { token: tokens.peer, body: {} })
    const others = await req('POST', `/api/v1/records/${rec2.id}/comments`, { token: tokens.editor, body: { content: 'editor 评论待管理者删除', mention_list: [] } })
    const otherComment = apiData(others)?.comment
    const deleteByManager = await req('DELETE', `/api/v1/comments/${otherComment.id}`, { token: tokens.owner, body: {} })
    const illegal = await req('POST', `/api/v1/records/${rec2.id}/comments`, { token: tokens.editor, body: { content: 'editor 评论待越权删除', mention_list: [] } })
    const illegalComment = apiData(illegal)?.comment
    const deleteByViewer = await req('DELETE', `/api/v1/comments/${illegalComment.id}`, { token: tokens.viewer, body: {} })
    assert(deleteOwn.json?.code === 0 && deleteByManager.json?.code === 0 && deleteByViewer.json?.code !== 0,
      `own=${deleteOwn.json?.code}, manager=${deleteByManager.json?.code}, viewer=${deleteByViewer.json?.code}`)
    return `自删=0；管理者删=0；越权删被拒 code=${deleteByViewer.json?.code}`
  })

  await testCase('TC-110-05', '行级未解决评论计数', async () => {
    const created = await req('POST', `/api/v1/records/${rec1.id}/comments`, { token: tokens.owner, body: { content: '未解决计数评论', mention_list: [] } })
    assert(created.json?.code === 0, `comment failed: ${created.text}`)
    const emptyRecord = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner, body: { table_id: table1.id, cells_map: { [f.primary.id]: '无评论记录' } },
    }))?.record
    assert(emptyRecord?.id, 'empty comment record create failed')
    const query = apiData(await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table1.id}&view_id=${ctx.viewId}&limit=100`, { token: tokens.owner }))
    const map = query?.unsolved_comment_count_map ?? {}
    assert(Number(map[rec1.id]) >= 1, `map=${JSON.stringify(map)}`)
    assert(map[emptyRecord.id] === undefined, `record without comments appears: ${map[emptyRecord.id]}`)
    return `unsolved_comment_count_map[${rec1.id}]=${map[rec1.id]}；无评论记录不出现`
  })

  await testCase('TC-110-06', '动态时间线', async () => {
    const created = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner, body: { table_id: table1.id, cells_map: { [f.primary.id]: '历史记录' } },
    }))?.record
    for (const value of [1, 2, 3]) {
      const snapshot = await getRecord(created.id)
      await req('PUT', `/api/v1/records/${created.id}/cells/${f.number.id}?base_id=${base.id}`, {
        token: tokens.owner, body: { value, expect_version: Number(snapshot.data?.version ?? 0) },
      })
    }
    const history = apiData(await req('GET', `/api/v1/records/${created.id}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    const times = history.map((node) => Number(node.created_at))
    const descending = times.every((time, index) => index === 0 || time <= times[index - 1])
    const hasOperator = history.every((node) => node.operator !== undefined && node.operator !== '')
    assert(history.length >= 3 && descending && hasOperator, `count=${history.length}, descending=${descending}, operator=${hasOperator}`)
    ctx.historyRecordId = created.id
    return `节点=${history.length}，倒序=${descending}，操作人完整=${hasOperator}`
  })

  await testCase('TC-110-07', '回滚', async () => {
    const recordId = ctx.historyRecordId
    const history = apiData(await req('GET', `/api/v1/records/${recordId}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    const node = history.find((item) => item.field_id === f.number.id && item.before !== undefined && item.before !== null && Number(decodeValue(item.before)) !== Number(decodeValue(item.after)))
    assert(node, `no rollbackable node: ${JSON.stringify(history)}`)
    const rollback = await req('POST', `/api/v1/records/${recordId}/history/${node.id}/rollback?base_id=${base.id}`, { token: tokens.owner, body: { node_id: node.id } })
    const record = apiData(rollback)?.record
    assert(rollback.json?.code === 0 && Number(decodeValue(record?.cells_map?.[f.number.id])) === Number(decodeValue(node.before)), `rollback failed: ${rollback.text}`)
    const after = apiData(await req('GET', `/api/v1/records/${recordId}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    assert(after.length > history.length, `rollback did not append history: ${history.length} -> ${after.length}`)
    return `回滚到 ${decodeValue(node.before)}；新增历史节点 ${history.length}->${after.length}`
  })

  await testCase('TC-110-08', '公式不可回滚', async () => {
    const recordId = ctx.historyRecordId
    const history = apiData(await req('GET', `/api/v1/records/${recordId}/history?limit=100`, { token: tokens.owner }))?.history_list ?? []
    const formulaNode = history.find((item) => item.field_id === f.formula.id)
    if (!formulaNode) throw new Blocked('历史列表中无公式字段节点（公式重算未落历史），无法验证公式不可回滚')
    const rollback = await req('POST', `/api/v1/records/${recordId}/history/${formulaNode.id}/rollback?base_id=${base.id}`, { token: tokens.owner, body: { node_id: formulaNode.id } })
    assert(rollback.json?.code !== 0, `formula rollback accepted: ${rollback.text}`)
    return `公式节点回滚被拒 code=${rollback.json?.code}`
  })

  // ================= T12 通知 =================
  await testCase('TC-120-01', '事件来源抽样', async () => {
    const mention = await waitNotification(tokens.peer, (item) => item.type === 'comment.mentioned', 12000)
    const collaborator = await waitNotification(tokens.editor, (item) => item.type === 'share.changed' || item.type === 'invite.created', 15000)
    const invite = await waitNotification(tokens.invitee, (item) => item.type === 'invite.created', 12000)
    assert(mention, 'comment.mentioned missing')
    assert(collaborator || invite, `collaborator/invite notification missing`)
    return `comment.mentioned=✓；协作者/邀请通知=${collaborator ? 'share.changed' : invite?.type}`
  })

  await testCase('TC-120-02', '轮询契约', async () => {
    const page = await req('GET', '/api/v1/notifications?limit=2', { token: tokens.owner })
    const list = apiData(page)?.notification_list ?? []
    const hasMore = apiData(page)?.has_more
    assert(page.json?.code === 0 && list.length <= 2 && typeof hasMore === 'boolean', `page=${page.text.slice(0, 200)}`)
    let nextOk = true
    if (hasMore && apiData(page)?.next_cursor) {
      const next = await req('GET', `/api/v1/notifications?limit=2&cursor=${encodeURIComponent(apiData(page).next_cursor)}`, { token: tokens.owner })
      nextOk = next.json?.code === 0 && (apiData(next)?.notification_list ?? []).length > 0
      assert(nextOk, `next page failed: ${next.text.slice(0, 200)}`)
    }
    const unread = apiData(await req('GET', '/api/v1/notifications/unread-count', { token: tokens.owner }))
    assert(unread?.count !== undefined, `unread missing: ${JSON.stringify(unread)}`)
    return `limit=2 → ${list.length} 条, has_more=${hasMore}, next=${nextOk}, unread=${unread.count}`
  })

  await testCase('TC-120-03', '失效对象', async () => {
    const temp = apiData(await req('POST', `/api/v1/bases/${base.id}/records`, {
      token: tokens.owner, body: { table_id: table1.id, cells_map: { [f.primary.id]: '通知失效对象' } },
    }))?.record
    const comment = await req('POST', `/api/v1/records/${temp.id}/comments`, {
      token: tokens.owner, body: { content: '@peer 失效对象', mention_list: [ctx.peerUserId] },
    })
    assert(comment.json?.code === 0, `comment failed: ${comment.text}`)
    const notification = await waitNotification(tokens.peer, (item) => item.record_id === temp.id, 15000)
    assert(notification, 'notification for temp record missing')
    await req('DELETE', `/api/v1/records?base_id=${base.id}`, { token: tokens.owner, body: { record_id_list: [temp.id] } })
    const readAfter = await req('GET', `/api/v1/records/${temp.id}?base_id=${base.id}`, { token: tokens.peer })
    assert(readAfter.json?.code === 4002 || readAfter.json?.code !== 0, `deleted record still readable: ${readAfter.text}`)
    return `通知保留；记录查询失效 code=${readAfter.json?.code}`
  })

  await testCase('TC-120-04', '已读/未读', async () => {
    const before = apiData(await req('GET', '/api/v1/notifications/unread-count', { token: tokens.owner }))?.count ?? 0
    const list = apiData(await req('GET', '/api/v1/notifications?limit=100', { token: tokens.owner }))?.notification_list ?? []
    const unreadItem = list.find((item) => item.read === false)
    assert(unreadItem, `no unread notification; count=${before}`)
    const mark = await req('POST', `/api/v1/notifications/${unreadItem.id}/read`, { token: tokens.owner, body: {} })
    const after = apiData(await req('GET', '/api/v1/notifications/unread-count', { token: tokens.owner }))?.count ?? 0
    assert(mark.json?.code === 0 && Number(after) === Number(before) - 1, `mark=${mark.json?.code}, ${before}->${after}`)
    return `未读数 ${before}->${after}`
  })

  // ================= T13 分享与协作者 =================
  await testCase('TC-130-03', '链接角色约束', async () => {
    const attempt = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'anyone', role: 'manager' } })
    assert(attempt.json?.code !== 0, `anyone+manager accepted: ${attempt.text}`)
    const restore = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'anyone', role: 'filler' } })
    assert(restore.json?.code === 0, `restore link failed: ${restore.text}`)
    return `任何人+可管理被拒 code=${attempt.json?.code}`
  })

  await testCase('TC-130-04', '内部协作者添加', async () => {
    const add = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.invitee, role: 'editor' } })
    assert(add.json?.code === 0, `add invitee failed: ${add.text}`)
    const collaborators = apiData(await req('GET', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner }))?.collaborator_list ?? []
    const entry = collaborators.find((item) => item.username === names.invitee)
    assert(entry && enumText(entry.role).includes('editor') && String(entry.source) === 'invite', `entry=${JSON.stringify(entry)}`)
    const notification = await waitNotification(tokens.invitee, (item) => item.base_id === base.id, 15000)
    assert(notification, 'collaborator notification missing')
    return `source=invite；通知 type=${notification.type}`
  })

  await testCase('TC-130-06', '外链清单', async () => {
    const listResp = await req('GET', '/api/v1/enterprise/open-links?limit=100', { token: tokens.admin2 })
    const list = apiData(listResp)?.link_list ?? []
    const entry = list.find((item) => item.base_id === base.id)
    assert(listResp.json?.code === 0 && entry, `open links=${listResp.text.slice(0, 300)}`)
    assert(entry.base_name && entry.role, `entry incomplete: ${JSON.stringify(entry)}`)
    return `企业外链 ${list.length} 条；命中 base=${entry.base_id}, role=${entry.role}`
  })

  await testCase('TC-130-05', '强制关闭链接（含在线外部会话失效）', async () => {
    // 确保链接有效并准备一个新的链接会话
    const enable = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'anyone', role: 'filler' } })
    assert(enable.json?.code === 0, `enable link failed: ${enable.text}`)
    const tokenNow = (apiData(enable)?.link_config?.url ?? '').split('/').filter(Boolean).pop()
    const linkUser2 = `p1ln2${RUN}`
    const reg2 = await register(linkUser2, { link_token: tokenNow })
    assert(reg2.json?.code === 0, `link user2 register failed: ${reg2.text}`)
    const linkSession2 = reg2.json?.data?.token
    const initial = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSession2 })
    assert(initial.json?.code === 0, `link session2 initial access failed: ${initial.text}`)

    const close = await req('POST', `/api/v1/enterprise/open-links/${base.id}/close`, { token: tokens.admin2, body: {} })
    assert(close.json?.code === 0, `force close failed: ${close.text}`)

    let denied = null
    const deadline = Date.now() + 70000
    while (Date.now() < deadline) {
      const probe = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSession2 })
      if (probe.json?.code !== 0) { denied = probe.json?.code; break }
      await sleep(3000)
    }
    const notification = await waitNotification(tokens.owner, (item) => item.type === 'link.closed', 20000)
    assert(denied !== null && notification, `denied=${denied}, notification=${notification ? 'yes' : 'no'}`)
    return `强关成功；外部会话 ${denied}；可管理者收到 link.closed`
  })

  await testCase('TC-130-07', '范围收窄失效', async () => {
    // 重新开启任何人链接并准备新会话
    const enable = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'anyone', role: 'viewer' } })
    assert(enable.json?.code === 0, `enable link failed: ${enable.text}`)
    const tokenNow = (apiData(enable)?.link_config?.url ?? '').split('/').filter(Boolean).pop()
    const user3 = `p1ln3${RUN}`
    const reg3 = await register(user3, { link_token: tokenNow })
    if (reg3.json?.code === 5003) throw new Blocked(`注册限速阻止链接会话准备：${reg3.text}`)
    assert(reg3.json?.code === 0, `link user3 register failed: ${reg3.text}`)
    const linkSession3 = reg3.json?.data?.token
    const before = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSession3 })
    assert(before.json?.code === 0, `link session3 initial access failed: ${before.text}`)
    const narrow = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: tokens.owner, body: { scope: 'enterprise', role: 'viewer' } })
    assert(narrow.json?.code === 0, `narrow failed: ${narrow.text}`)
    let denied = null
    const deadline = Date.now() + 70000
    while (Date.now() < deadline) {
      const probe = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSession3 })
      if (probe.json?.code !== 0) { denied = probe.json?.code; break }
      await sleep(3000)
    }
    assert(denied !== null, 'external session still allowed after 70s')
    return `收窄为 enterprise 后外部会话 ${denied}（≤70s）`
  })

  await testCase('TC-130-08', '移除协作者', async () => {
    const viewerId = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.viewer }))?.user?.id
    const remove = await req('DELETE', `/api/v1/shares/members/${viewerId}`, { token: tokens.owner, body: { base_id: base.id } })
    assert(remove.json?.code === 0, `remove viewer failed: ${remove.text}`)
    let denied = null
    const deadline = Date.now() + 70000
    while (Date.now() < deadline) {
      const probe = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.viewer })
      if (probe.json?.code !== 0) { denied = probe.json?.code; break }
      await sleep(3000)
    }
    assert(denied !== null, 'removed viewer still allowed after 70s')
    // 恢复 viewer 以便后续导出权限用例
    await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: tokens.owner, body: { username: names.viewer, role: 'viewer' } })
    return `移除后访问=${denied}`
  })

  // ================= T14 导入导出 =================
  async function runImport(filePath, sheetIndex, columnTypes, label, clientRequestId = `${RUN}-${label}`) {
    const upload = await uploadFile(tokens.owner, base.id, filePath, 'import_temp')
    const fileId = apiData(upload)?.id
    if (upload.json?.code !== 0) return { stage: 'upload', response: upload }
    const preview = await req('POST', '/api/v1/imports/preview', { token: tokens.owner, body: { file_id: fileId, sheet_index: sheetIndex } })
    const columns = apiData(preview)?.preview?.column_list ?? []
    const columnConfigList = columns.map((column, index) => ({
      index: column.index ?? index,
      name: column.name || `列${index + 1}`,
      type: columnTypes[index] ?? 'text',
      ignore: false,
    }))
    const create = await req('POST', '/api/v1/imports', {
      token: tokens.owner,
      body: { file_id: fileId, sheet_index: sheetIndex, first_row_header: true, column_config_list: columnConfigList, client_request_id: clientRequestId },
    })
    const taskId = apiData(create)?.import_task?.id
    if (create.json?.code !== 0) return { stage: 'create', response: create, preview, fileId }
    let task = apiData(create)?.import_task
    for (let i = 0; i < 90; i += 1) {
      await sleep(1000)
      task = apiData(await req('GET', `/api/v1/imports/${taskId}`, { token: tokens.owner }))?.import_task
      if (task?.status === 'IMPORT_STATUS_DONE' || task?.status === 'IMPORT_STATUS_FAILED') break
    }
    let rows = []
    if (task?.base_id) {
      const importedTables = apiData(await req('GET', `/api/v1/bases/${task.base_id}/tables`, { token: tokens.owner }))?.table_list ?? []
      if (importedTables[0]) {
        const importedViews = apiData(await req('GET', `/api/v1/tables/${importedTables[0].id}/views`, { token: tokens.owner }))?.view_list ?? []
        const query = new URLSearchParams({ table_id: importedTables[0].id, limit: '100' })
        if (importedViews[0]?.id) query.set('view_id', importedViews[0].id)
        rows = apiData(await req('GET', `/api/v1/bases/${task.base_id}/records?${query.toString()}`, { token: tokens.owner }))?.record_list ?? []
      }
    }
    return { stage: 'done', upload, preview, create, fileId, task, taskId, rows, columnConfigList }
  }

  const xlsxImport = await runImport(files.multiXlsx, 0, ['text', 'number'], 'xlsx', `${RUN}-import-xlsx`)
  await testCase('TC-140-04', '多 Sheet 指定第 2 个 Sheet', async () => {
    const result = await runImport(files.multiXlsx, 1, ['text', 'number'], 'sheet2', `${RUN}-import-sheet2`)
    assert(result.stage === 'done' && result.task?.status === 'IMPORT_STATUS_DONE', `import failed: ${JSON.stringify(result.stage)} ${result.response?.text ?? ''}`)
    const rows = result.rows.map((row) => decodeCells(row))
    const headers = apiData(result.preview)?.preview?.column_list?.map((column) => column.name) ?? []
    assert(headers.includes('标题') && headers.includes('分数'), `preview headers=${JSON.stringify(headers)}`)
    assert(rows.length === 3, `rows=${rows.length}`)
    const titles = rows.map((cells) => Object.values(cells)).flat().map(String)
    assert(titles.includes('丙') && !titles.includes('甲'), `unexpected sheet data: ${JSON.stringify(titles)}`)
    return `仅导入第 2 Sheet：3 行，列=${JSON.stringify(headers)}`
  })

  await testCase('TC-140-05', '部分失败报告', async () => {
    const result = await runImport(files.dirtyCsv, 0, ['text', 'number'], 'dirty', `${RUN}-import-dirty`)
    assert(result.stage === 'done' && result.task?.status === 'IMPORT_STATUS_DONE', `stage=${result.stage}, ${result.response?.text ?? ''}`)
    assert(Number(result.task.ok_rows) >= 1 && Number(result.task.fail_rows) >= 1, `ok=${result.task.ok_rows}, fail=${result.task.fail_rows}`)
    const report = await req('GET', `/api/v1/imports/${result.taskId}/report`, { token: tokens.owner })
    const reportText = report.buf.toString('utf8')
    assert(report.status === 200 && reportText.length > 0, `report status=${report.status}, len=${reportText.length}`)
    assert(/坏行|不是数字|行号|row/i.test(reportText), `report lacks row detail: ${reportText.slice(0, 200)}`)
    return `ok=${result.task.ok_rows}, fail=${result.task.fail_rows}; 报告 ${reportText.length} 字节`
  })

  await testCase('TC-140-06', '失败重试（同文件修正映射）', async () => {
    const upload = await uploadFile(tokens.owner, base.id, files.cleanCsv, 'import_temp')
    const fileId = apiData(upload)?.id
    assert(upload.json?.code === 0 && fileId, `upload failed: ${upload.text}`)
    const bad = await req('POST', '/api/v1/imports', {
      token: tokens.owner,
      body: { file_id: fileId, sheet_index: 5, first_row_header: true, column_config_list: [], client_request_id: `${RUN}-retry-bad` },
    })
    assert(bad.json?.code !== 0, `invalid sheet_index accepted: ${bad.text}`)
    const preview = await req('POST', '/api/v1/imports/preview', { token: tokens.owner, body: { file_id: fileId, sheet_index: 0 } })
    const columns = apiData(preview)?.preview?.column_list ?? []
    const columnConfigList = columns.map((column, index) => ({ index: column.index ?? index, name: column.name || `列${index + 1}`, type: ['text', 'number'][index] ?? 'text', ignore: false }))
    const retry = await req('POST', '/api/v1/imports', {
      token: tokens.owner,
      body: { file_id: fileId, sheet_index: 0, first_row_header: true, column_config_list: columnConfigList, client_request_id: `${RUN}-retry-good` },
    })
    const taskId = apiData(retry)?.import_task?.id
    assert(retry.json?.code === 0 && taskId, `retry failed: ${retry.text}`)
    let task = apiData(retry)?.import_task
    for (let i = 0; i < 60; i += 1) {
      await sleep(1000)
      task = apiData(await req('GET', `/api/v1/imports/${taskId}`, { token: tokens.owner }))?.import_task
      if (task?.status === 'IMPORT_STATUS_DONE' || task?.status === 'IMPORT_STATUS_FAILED') break
    }
    assert(task?.status === 'IMPORT_STATUS_DONE' && Number(task.ok_rows) === 3, `status=${task?.status}, ok=${task?.ok_rows}`)
    return `同一 file_id 修正后成功：3 行`
  })

  await testCase('TC-140-07', '导入超限拦截', async () => {
    const beforeHome = apiData(await req('GET', '/api/v1/home', { token: tokens.owner }))
    const beforeCount = (beforeHome?.created_list ?? []).length
    const big = await uploadFile(tokens.owner, base.id, files.bigFile, 'import_temp')
    assert(big.json?.code === 5002, `21MB upload code=${big.json?.code}, ${big.text.slice(0, 120)}`)
    const corruptUpload = await uploadFile(tokens.owner, base.id, files.corruptXlsx, 'import_temp')
    let corruptPreviewCode = null
    if (corruptUpload.json?.code === 0) {
      const preview = await req('POST', '/api/v1/imports/preview', { token: tokens.owner, body: { file_id: apiData(corruptUpload)?.id, sheet_index: 0 } })
      corruptPreviewCode = preview.json?.code
      assert(preview.json?.code !== 0, `corrupt xlsx preview accepted: ${preview.text.slice(0, 120)}`)
    }
    const afterHome = apiData(await req('GET', '/api/v1/home', { token: tokens.owner }))
    const afterCount = (afterHome?.created_list ?? []).length
    assert(afterCount === beforeCount, `half-created base detected: ${beforeCount} -> ${afterCount}`)
    return `21MB→5002；损坏文件 preview=${corruptPreviewCode}；无半成品 Base`
  })

  await testCase('TC-140-08', '导入中隔离', async () => {
    const upload = await uploadFile(tokens.owner, base.id, files.largeCsv, 'import_temp')
    const fileId = apiData(upload)?.id
    assert(upload.json?.code === 0 && fileId, `large upload failed: ${upload.text}`)
    const preview = await req('POST', '/api/v1/imports/preview', { token: tokens.owner, body: { file_id: fileId, sheet_index: 0 } })
    const columns = apiData(preview)?.preview?.column_list ?? []
    const columnConfigList = columns.map((column, index) => ({ index: column.index ?? index, name: column.name || `列${index + 1}`, type: ['text', 'number'][index] ?? 'text', ignore: false }))
    const create = await req('POST', '/api/v1/imports', {
      token: tokens.owner,
      body: { file_id: fileId, sheet_index: 0, first_row_header: true, column_config_list: columnConfigList, client_request_id: `${RUN}-import-large` },
    })
    const taskId = apiData(create)?.import_task?.id
    assert(create.json?.code === 0 && taskId, `large import create failed: ${create.text}`)
    let task = apiData(create)?.import_task
    let observedImporting = false
    let collaboratorCode = null
    const deadline = Date.now() + 120000
    while (Date.now() < deadline) {
      task = apiData(await req('GET', `/api/v1/imports/${taskId}`, { token: tokens.owner }))?.import_task
      if (task?.base_id && String(task.status).includes('IMPORTING') && !observedImporting) {
        observedImporting = true
        // 导入中把 editor 加入该 Base，再以 editor 访问
        await req('POST', `/api/v1/bases/${task.base_id}/shares/members`, { token: tokens.owner, body: { username: names.editor, role: 'viewer' } })
        const probe = await req('GET', `/api/v1/bases/${task.base_id}/tables`, { token: tokens.editor })
        collaboratorCode = probe.json?.code
      }
      if (task?.status === 'IMPORT_STATUS_DONE' || task?.status === 'IMPORT_STATUS_FAILED') break
      await sleep(200)
    }
    assert(task?.status === 'IMPORT_STATUS_DONE', `large import status=${task?.status}, ok=${task?.ok_rows}, fail=${task?.fail_rows}`)
    if (!observedImporting) throw new Blocked('导入过快，未捕获到 IMPORTING 窗口，无法验证隔离态')
    assert(collaboratorCode !== 0, `collaborator accessed importing base: code=${collaboratorCode}`)
    // 导入中新增的协作者身份受权限短缓存窗口影响（与 TC-080-07/TC-130-05 同一
    // 语义）：解锁后轮询等待缓存刷新，最长 75s。
    let afterCode = null
    for (let attempt = 0; attempt < 25; attempt += 1) {
      const afterProbe = await req('GET', `/api/v1/bases/${task.base_id}/tables`, { token: tokens.editor })
      afterCode = afterProbe.json?.code
      if (afterCode === 0) break
      await sleep(3000)
    }
    assert(afterCode === 0, `collaborator not restored after import: code=${afterCode}`)
    return `导入中协作者访问=${collaboratorCode}；完成后访问=0（共 ${task.ok_rows} 行）`
  })

  await testCase('TC-140-10', '导出权限', async () => {
    const viewerExport = await req('POST', '/api/v1/exports', {
      token: tokens.viewer, body: { base_id: base.id, table_id: table1.id, view_id: ctx.viewId, format: 'csv' },
    })
    const extExport = await req('POST', '/api/v1/exports', {
      token: tokens.ext, body: { base_id: base.id, table_id: table1.id, view_id: ctx.viewId, format: 'csv' },
    })
    assert([3001, 3002].includes(viewerExport.json?.code), `viewer export code=${viewerExport.json?.code}`)
    assert(extExport.json?.code === 3002, `external export code=${extExport.json?.code}`)
    return `viewer=${viewerExport.json?.code}; external=${extExport.json?.code}`
  })

  // ================= T15 企业管理与审计 =================
  await testCase('TC-150-01', '成员列表与邀请', async () => {
    const members = apiData(await req('GET', '/api/v1/enterprise/members?limit=100', { token: tokens.owner }))?.member_list ?? []
    assert(members.some((member) => member.username === names.admin2), 'admin2 missing from members')
    const invites = apiData(await req('GET', '/api/v1/enterprise/invites?limit=100', { token: tokens.owner }))?.invitation_list ?? []
    assert(Array.isArray(invites), 'enterprise invites malformed')
    return `成员=${members.length}；邀请列表=${invites.length}`
  })

  await testCase('TC-150-02', '停用/启用成员', async () => {
    const editorId = apiData(await req('GET', '/api/v1/auth/me', { token: tokens.editor }))?.user?.id
    const disable = await req('POST', `/api/v1/enterprise/members/${editorId}/disable`, { token: tokens.owner, body: {} })
    assert(disable.json?.code === 0, `disable failed: ${disable.text}`)
    const blocked = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.editor })
    assert(blocked.json?.code !== 0, `disabled member still allowed: ${blocked.text}`)
    const membersDisabled = apiData(await req('GET', '/api/v1/enterprise/members?limit=100', { token: tokens.owner }))?.member_list ?? []
    const entry = membersDisabled.find((member) => member.user_id === editorId)
    assert(enumText(entry?.status).includes('disabled'), `member status=${entry?.status}`)
    const enable = await req('POST', `/api/v1/enterprise/members/${editorId}/enable`, { token: tokens.owner, body: {} })
    assert(enable.json?.code === 0, `enable failed: ${enable.text}`)
    tokens.editor = await login(names.editor)
    let restored = null
    const deadline = Date.now() + 70000
    while (Date.now() < deadline) {
      restored = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: tokens.editor })
      if (restored.json?.code === 0) break
      await sleep(3000)
    }
    assert(restored.json?.code === 0, `enabled member access failed within 70s: ${restored.text}`)
    return `停用后访问=${blocked.json?.code}；状态=disabled；启用后恢复`
  })

  await testCase('TC-150-03', '角色调整', async () => {
    const admin2IdNow = ctx.admin2Id
    const toMember = await req('PUT', `/api/v1/enterprise/members/${admin2IdNow}/role`, { token: tokens.owner, body: { role: 'member' } })
    assert(toMember.json?.code === 0, `demote admin2 failed: ${toMember.text}`)
    const memberList = apiData(await req('GET', '/api/v1/enterprise/members?limit=100', { token: tokens.owner }))?.member_list ?? []
    const roleNow = memberList.find((item) => item.user_id === admin2IdNow)?.role
    assert(enumText(roleNow) === 'member', `role now=${JSON.stringify(roleNow)}`)
    const back = await req('PUT', `/api/v1/enterprise/members/${admin2IdNow}/role`, { token: tokens.owner, body: { role: 'manager' } })
    assert(back.json?.code === 0, `promote admin2 failed: ${back.text}`)
    return `manager→member→manager 均生效`
  })

  await testCase('TC-150-04', '审计查询与导出', async () => {
    // 触发可审计动作：导出 + 删除临时表
    const tempTable = apiData(await req('POST', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner, body: { name: '审计删除表' } }))?.table
    await req('DELETE', `/api/v1/tables/${tempTable.id}`, { token: tokens.owner, body: { confirm: true } })
    const exportResp = await req('POST', '/api/v1/exports', {
      token: tokens.owner, body: { base_id: base.id, table_id: table1.id, view_id: ctx.viewId, format: 'csv', client_request_id: `${RUN}-audit-export` },
    })
    assert(exportResp.json?.code === 0, `export failed: ${exportResp.text}`)
    let auditList = []
    const deadline = Date.now() + 20000
    while (Date.now() < deadline) {
      auditList = apiData(await req('GET', '/api/v1/enterprise/audits?limit=100', { token: tokens.owner }))?.audit_list ?? []
      const actions = auditList.map((item) => enumText(item.action))
      if (actions.includes('table_deleted') && actions.includes('data_export')) break
      await sleep(1000)
    }
    const actions = auditList.map((item) => enumText(item.action))
    assert(actions.includes('table_deleted') && actions.includes('data_export'), `actions=${JSON.stringify([...new Set(actions)])}`)
    const csv = await req('GET', '/api/v1/enterprise/audits/export.csv', { token: tokens.owner })
    const csvText = csv.buf.toString('utf8')
    assert(csv.status === 200 && csvText.length > 0, `audit csv status=${csv.status}`)
    ctx.auditActions = [...new Set(actions)]
    return `动作=${JSON.stringify(ctx.auditActions)}；CSV ${csvText.length} 字节`
  })

  await testCase('TC-150-05', '导出动作入审计（client_request_id）', async () => {
    const clientRequestId = `${RUN}-audit-export`
    let hit = null
    const deadline = Date.now() + 20000
    while (Date.now() < deadline) {
      const auditList = apiData(await req('GET', '/api/v1/enterprise/audits?limit=100', { token: tokens.owner }))?.audit_list ?? []
      hit = auditList.find((item) => enumText(item.action) === 'data_export' && JSON.stringify(item.detail_map ?? {}).includes(clientRequestId))
      if (hit) break
      await sleep(1000)
    }
    assert(hit, `no data_export audit with client_request_id=${clientRequestId}`)
    return `审计命中 client_request_id=${clientRequestId}`
  })

  await testCase('TC-150-06', '企业注销 30 天', async () => {
    throw new NotRun('企业注销受理无 HTTP/RPC 公开入口（仅 admin 脚本 RPC），到期执行需调度器与 DB 造数，本轮黑盒环境不具备')
  })

  // ================= T16 错误码 =================
  await testCase('TC-160-03', '1001 参数非法', async () => {
    const missingBaseId = await req('GET', `/api/v1/records/${rec1.id}`, { token: tokens.owner })
    const longName = await req('POST', '/api/v1/bases', { token: tokens.owner, body: { name: '超'.repeat(51) } })
    assert(missingBaseId.json?.code === 1001, `missing base_id code=${missingBaseId.json?.code}`)
    assert(longName.json?.code === 1001, `51-char name code=${longName.json?.code}`)
    return `缺 base_id=${missingBaseId.json?.code}; 超长名称=${longName.json?.code}`
  })

  await testCase('TC-160-04', '1002 重名', async () => {
    const uniqueName = `重名表${RUN.slice(-4)}`
    const first = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner, body: { name: uniqueName } })
    const second = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner, body: { name: uniqueName } })
    const dupField = await createField('数量', 'number', {})
    assert(first.json?.code === 0 && second.json?.code === 1002 && dupField.json?.code === 1002,
      `first=${first.json?.code}, second=${second.json?.code} (${second.text.slice(0, 120)}), field=${dupField.json?.code} (${dupField.text.slice(0, 120)})`)
    return `表重名=1002；字段重名=1002`
  })

  await testCase('TC-160-05', '5001 容量上限', async () => {
    const capTable = apiData(await req('POST', `/api/v1/bases/${base.id}/tables`, { token: tokens.owner, body: { name: '容量表' } }))?.table
    assert(capTable?.id, 'capacity table create failed')
    const existing = apiData(await req('GET', `/api/v1/tables/${capTable.id}/fields`, { token: tokens.owner }))?.field_list ?? []
    for (let index = existing.length; index < 100; index += 1) {
      const created = await req('POST', `/api/v1/tables/${capTable.id}/fields`, {
        token: tokens.owner, body: { name: `字段${index}`, type: 'text', config_map: {} },
      })
      assert(created.json?.code === 0, `field ${index} create failed: ${created.text}`)
    }
    const overflow = await req('POST', `/api/v1/tables/${capTable.id}/fields`, {
      token: tokens.owner, body: { name: '第101个字段', type: 'text', config_map: {} },
    })
    assert(overflow.json?.code === 5001, `overflow code=${overflow.json?.code}, ${overflow.text.slice(0, 120)}`)
    return `字段 100 上限后新增返回 5001`
  })

  await testCase('TC-160-06', '5002 附件', async () => {
    const exe = await uploadFile(tokens.owner, base.id, files.exeFile, 'attachment')
    const big = await uploadFile(tokens.owner, base.id, files.bigFile, 'attachment')
    assert(exe.json?.code === 5002, `.exe code=${exe.json?.code}`)
    assert(big.json?.code === 5002, `>20MB code=${big.json?.code}`)
    return `.exe=${exe.json?.code}; 21MB=${big.json?.code}`
  })

  await testCase('TC-160-08', '9001 服务不可用', async () => {
    throw new NotRun('需停止 core 进程制造依赖不可用；本轮为避免影响同一部署上的 UI 用例，未执行进程级故障注入')
  })

  await testCase('TC-180-01', '埋点批量上报', async () => {
    const eventId = `${RUN}-evt-1`.padEnd(16, 'x')
    const body = { events: [
      { event_id: eventId, event_name: 'fe_base_create', detail: { source: 'p1p2' }, client_at_ms: Date.now() },
      { event_id: `${RUN}-evt-2`.padEnd(16, 'y'), event_name: 'fe_cell_edit_submit', detail: { row: 1 }, client_at_ms: Date.now() },
    ] }
    const first = await req('POST', '/api/v1/tracking/batch', { token: tokens.owner, body })
    const second = await req('POST', '/api/v1/tracking/batch', { token: tokens.owner, body: { events: [body.events[0]] } })
    assert(first.json?.code === 0 && Number(apiData(first)?.accepted) === 2, `first=${first.text}`)
    assert(second.json?.code === 0 && Number(apiData(second)?.duplicated) === 1 && Number(apiData(second)?.accepted) === 0, `second=${second.text}`)
    return `首报 accepted=2；重报 duplicated=1`
  })

  await testCase('TC-180-02', '埋点未登录拒绝', async () => {
    const unauth = await req('POST', '/api/v1/tracking/batch', { body: { events: [{ event_id: `${RUN}-evt-3`.padEnd(16, 'z'), event_name: 'fe_login_success', detail: {}, client_at_ms: Date.now() }] } })
    assert(unauth.json?.code === 2001, `code=${unauth.json?.code}, ${unauth.text.slice(0, 120)}`)
    return `未登录 code=2001`
  })

  await testCase('TC-160-07', '5003 限速', async () => {
    let limited = null
    let okCount = 0
    for (let index = 0; index < 70; index += 1) {
      const eventId = `${RUN}-rl-${index}`.padEnd(16, 'a')
      const r = await req('POST', '/api/v1/tracking/batch', {
        token: tokens.owner,
        body: { events: [{ event_id: eventId, event_name: 'fe_perf_screen_ready', detail: { index }, client_at_ms: Date.now() }] },
      })
      if (r.json?.code === 5003) { limited = index; break }
      if (r.json?.code === 0) okCount += 1
    }
    assert(limited !== null, `no 5003 after ${okCount} requests`)
    return `第 ${limited + 1} 次请求触发 5003（成功 ${okCount} 次）`
  })

  // ================= T14 xls（放最后：该导入会触发 worker panic，避免污染其余用例） =================
  await testCase('TC-140-03', 'xls 导入', async () => {
    const result = await runImport(files.legacyXls, 0, ['text', 'number'], 'xls', `${RUN}-import-xls`)
    const rows = result.rows ?? []
    assert(result.stage === 'done' && result.task?.status === 'IMPORT_STATUS_DONE' && rows.length === 3,
      `stage=${result.stage}, status=${result.task?.status}, rows=${rows.length}, ok=${result.task?.ok_rows}, fail=${result.task?.fail_rows}, resp=${result.response?.text ?? ''}`)
    return `xls 预览/入库 3 行成功`
  })

  // ================= T08-11 所有者转移（最后执行） =================
  await testCase('TC-080-11', '所有者转移', async () => {
    const before = apiData(await req('GET', '/api/v1/enterprise', { token: tokens.owner }))
    const beforeOwner = before?.enterprise?.owner_id
    const transfer = await req('POST', '/api/v1/enterprise/owner/transfer', { token: tokens.owner, body: { successor_id: ctx.admin2Id, confirm: true } })
    assert(transfer.json?.code === 0, `transfer failed: ${transfer.text}`)
    const after = apiData(await req('GET', '/api/v1/enterprise', { token: tokens.admin2 }))
    assert(after?.enterprise?.owner_id === ctx.admin2Id && beforeOwner !== ctx.admin2Id, `owner=${after?.enterprise?.owner_id}`)
    let auditList = []
    const deadline = Date.now() + 15000
    while (Date.now() < deadline) {
      auditList = apiData(await req('GET', '/api/v1/enterprise/audits?limit=100', { token: tokens.admin2 }))?.audit_list ?? []
      if (auditList.some((item) => enumText(item.action).includes('owner_transferred'))) break
      await sleep(1000)
    }
    const audit = auditList.find((item) => enumText(item.action).includes('owner_transferred'))
    // 转回给原所有者，保持环境可继续使用
    await req('POST', '/api/v1/enterprise/owner/transfer', { token: tokens.admin2, body: { successor_id: ctx.ownerUserId, confirm: true } })
    assert(audit, 'owner transfer audit missing')
    return `owner_id ${beforeOwner} -> ${ctx.admin2Id}；审计已落；已转回`
  })

  // ================= 汇总 =================
  const summary = results.reduce((acc, item) => { acc[item.status] = (acc[item.status] || 0) + 1; return acc }, {})
  fs.writeFileSync(path.join(OUT_DIR, 'p1p2-api-results.json'), JSON.stringify({ run: RUN, base: BASE, ctx: { baseId: base.id, tableId: table1.id, linkToken }, results, summary }, null, 2))
  console.log('\nSUMMARY', JSON.stringify(summary))
  const failed = results.filter((item) => item.status !== 'PASS')
  if (failed.length > 0) console.log('NON-PASS:', failed.map((item) => `${item.id}:${item.status}`).join(', '))
}

main().catch((error) => {
  console.error('FATAL', error)
  fs.writeFileSync(path.join(OUT_DIR, 'p1p2-api-results.json'), JSON.stringify({ run: RUN, fatal: String(error?.stack || error), results }, null, 2))
  process.exit(1)
})
