// 全流程 P0 用例执行器：API / WS 部分（对 http://127.0.0.1:8080 真实部署）
// 结果落 p0-api-results.json，供汇总报告使用。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import WebSocket from 'ws'

const BASE = process.env.FF_BASE || 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = 'ff' + Date.now().toString(36).slice(-6)
const OUT_DIR = path.dirname(fileURLToPath(import.meta.url))
const evidenceDir = path.join(OUT_DIR, 'evidence')
fs.mkdirSync(evidenceDir, { recursive: true })

const results = []
const ctx = {}

function record(id, name, status, detail) {
  results.push({ id, name, status, detail })
  const icon = status === 'PASS' ? 'PASS' : status === 'FAIL' ? 'FAIL' : 'BLOCKED'
  console.log(`[${icon}] ${id} ${name}${detail ? ' — ' + detail : ''}`)
}

async function req(method, urlPath, { token, body, headers = {}, redirect = 'follow', form } = {}) {
  const url = urlPath.startsWith('http') ? urlPath : BASE + urlPath
  const h = { ...headers }
  if (token) h.Authorization = `Bearer ${token}`
  let payload
  if (form !== undefined) {
    payload = form
  } else if (body !== undefined) {
    h['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  const response = await fetch(url, { method, headers: h, body: payload, redirect })
  const buf = Buffer.from(await response.arrayBuffer())
  let json = null
  const text = buf.toString('utf8')
  if ((response.headers.get('content-type') || '').includes('json')) {
    try { json = JSON.parse(text) } catch { /* ignore */ }
  }
  return { status: response.status, headers: response.headers, buf, text, json }
}

async function register(username, extra = {}) {
  return req('POST', '/api/v1/auth/register', { body: { username, password: PWD, ...extra } })
}

async function login(username, password = PWD) {
  const r = await req('POST', '/api/v1/auth/login', { body: { username, password } })
  if (r.json?.code === 0) return r.json.data.token
  throw new Error(`login ${username} failed: ${r.status} ${r.text}`)
}

function apiData(r) { return r.json?.data }

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

async function uploadFile(token, baseId, filePath, usage = 'import_temp') {
  const fileName = path.basename(filePath)
  const bytes = fs.readFileSync(filePath)
  const form = new FormData()
  form.append('usage', usage)
  form.append('base_id', baseId)
  form.append('file', new Blob([bytes]), fileName)
  const r = await req('POST', '/api/v1/files', { token, form })
  return r
}

function sleep(ms) { return new Promise((resolve) => setTimeout(resolve, ms)) }

function wsConnect(token, baseId, tableId) {
  const wsUrl = BASE.replace(/^http/, 'ws') + `/bases/${encodeURIComponent(baseId)}/tables/${encodeURIComponent(tableId)}/ws`
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(wsUrl, { headers: { Authorization: `Bearer ${token}` } })
    const frames = []
    const waiters = []
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
    ws.on('open', () => resolve({ ws, frames, waitFrame, close: () => ws.close() }))
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
  })
}

function frameKind(frame) {
  const keys = Object.keys(frame).filter((key) => key !== 'msg_id')
  return keys[0] || ''
}

async function createField(token, tableId, name, type, configMap = {}) {
  return req('POST', `/api/v1/tables/${tableId}/fields`, {
    token,
    body: { name, type, config_map: configMap },
  })
}

async function main() {
  // ---------- 0. 环境与账号准备 ----------
  const owner = `own${RUN}`
  const peer = `peer${RUN}`
  const filler = `fill${RUN}`
  const viewer = `view${RUN}`

  const health = await req('GET', '/healthz')
  const ready = await req('GET', '/readyz')
  record('TC-000-03', '健康检查 /healthz /readyz', health.status === 200 && ready.status === 200 ? 'PASS' : 'FAIL',
    `healthz=${health.status}, readyz=${ready.status}, readyz_body=${JSON.stringify(ready.text)}`)

  const unauth = await req('GET', '/api/v1/auth/me')
  record('TC-000-04', 'API 路由优先（未登录 JSON 信封）', unauth.json?.code === 2001 ? 'PASS' : 'FAIL',
    `status=${unauth.status}, code=${unauth.json?.code}`)

  // 注册校验（TC-010-02）：用户名过短 / 用户名含非法字符 / 纯数字密码
  const bad1 = await req('POST', '/api/v1/auth/register', { body: { username: 'ab', password: PWD } })
  const bad2 = await req('POST', '/api/v1/auth/register', { body: { username: `bad-${RUN}`, password: PWD } })
  const badPwd = await req('POST', '/api/v1/auth/register', { body: { username: `good${RUN}`, password: '12345678' } })
  record('TC-010-02', '注册字段校验',
    bad1.json?.code === 1001 && bad2.json?.code === 1001 && badPwd.json?.code === 1001 ? 'PASS' : 'FAIL',
    `short=${bad1.json?.code}, illegalChar=${bad2.json?.code}, digitsPwd=${badPwd.json?.code}`)

  // 注册四个账号（owner/peer 建企业；filler/viewer 无企业，后续邀请）
  const regOwner = await register(owner, { enterprise_name: `测试企业${RUN.slice(-4)}` })
  const regPeer = await register(peer, { enterprise_name: `对测企业${RUN.slice(-4)}` })
  const regFiller = await register(filler, {})
  const regViewer = await register(viewer, {})
  if (regOwner.json?.code !== 0) throw new Error(`owner register failed: ${regOwner.text}`)
  if (regPeer.json?.code !== 0) throw new Error(`peer register failed: ${regPeer.text}`)
  if (regFiller.json?.code !== 0) throw new Error(`filler register failed: ${regFiller.text}`)
  if (regViewer.json?.code !== 0) throw new Error(`viewer register failed: ${regViewer.text}`)
  ctx.owner = owner; ctx.peer = peer; ctx.filler = filler; ctx.viewer = viewer

  // 重复用户名（TC-010-03）
  const dup = await register(owner, { enterprise_name: `重复企业${RUN.slice(-4)}` })
  record('TC-010-03', '用户名重复', dup.json?.code === 1002 ? 'PASS' : 'FAIL',
    `code=${dup.json?.code}, message=${dup.json?.message}`)

  const ownerToken = await login(owner)
  const peerToken = await login(peer)
  let fillerToken = await login(filler)
  let viewerToken = await login(viewer)

  // 邀请 filler/viewer 加入 owner 企业并接受
  const invite = await req('POST', '/api/v1/enterprise/invites', { token: ownerToken, body: { username_list: [filler, viewer] } })
  const invitationList = apiData(invite)?.invitation_list ?? []
  ctx.inviteCodes = { invite: invite.json?.code, count: invitationList.length }
  const invitationIds = new Map(invitationList.map((item) => [item.username, item.id]))
  for (const [username, token] of [[filler, fillerToken], [viewer, viewerToken]]) {
    const invId = invitationIds.get(username)
    if (invId !== undefined) {
      await req('POST', `/api/v1/invitations/${invId}/accept`, { token })
    }
  }
  fillerToken = await login(filler)
  viewerToken = await login(viewer)

  // ---------- 1. Base / 表 / 视图 / 字段默认结构（TC-020-02、TC-030-01） ----------
  const baseResp = await req('POST', '/api/v1/bases', { token: ownerToken, body: { name: `沙盒-${RUN}` } })
  const base = apiData(baseResp)?.base
  if (base?.id === undefined) throw new Error(`create base failed: ${baseResp.text}`)
  ctx.baseId = base.id

  const tablesResp = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: ownerToken })
  const tables = apiData(tablesResp)?.table_list ?? []
  const firstTable = tables[0]
  ctx.tableId = firstTable?.id
  const fieldsResp = await req('GET', `/api/v1/tables/${firstTable.id}/fields`, { token: ownerToken })
  const fields = apiData(fieldsResp)?.field_list ?? []
  const viewsResp = await req('GET', `/api/v1/tables/${firstTable.id}/views`, { token: ownerToken })
  const views = apiData(viewsResp)?.view_list ?? []
  ctx.viewId = views[0]?.id
  record('TC-020-02', '新建 Base 默认结构（API 侧）',
    base?.id !== undefined && tables.length === 1 && views.length === 1 && fields.length >= 1 && fields.some((f) => f.is_primary === true) ? 'PASS' : 'FAIL',
    `tables=${tables.length}, views=${views.length}, fields=${fields.length}, primary=${fields.some((f) => f.is_primary)}`)

  const newTableResp = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: ownerToken, body: { name: '表二' } })
  const table2 = apiData(newTableResp)?.table
  const t2Fields = apiData(await req('GET', `/api/v1/tables/${table2.id}/fields`, { token: ownerToken }))?.field_list ?? []
  const t2Views = apiData(await req('GET', `/api/v1/tables/${table2.id}/views`, { token: ownerToken }))?.view_list ?? []
  record('TC-030-01', '新增表自带默认视图与主字段（API 侧）',
    t2Fields.length === 1 && t2Fields[0]?.is_primary === true && t2Views.length === 1 ? 'PASS' : 'FAIL',
    `fields=${t2Fields.length}, primary=${t2Fields[0]?.is_primary}, views=${t2Views.length}`)
  ctx.table2Id = table2.id

  // 最后一张表禁删（API 侧）：先删表二，再删表一
  const delT2 = await req('DELETE', `/api/v1/tables/${table2.id}`, { token: ownerToken, body: { confirm: true } })
  const delLast = await req('DELETE', `/api/v1/tables/${firstTable.id}`, { token: ownerToken, body: { confirm: true } })
  record('TC-030-02', '最后一张表禁删（API 侧）', delLast.json?.code === 4003 ? 'PASS' : 'FAIL',
    `deleteSecond=${delT2.json?.code}, deleteLast=${delLast.json?.code}, message=${delLast.json?.message}`)

  // ---------- 2. 协作者 ----------
  const addFiller = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: ownerToken, body: { username: filler, role: 'filler' } })
  const addViewer = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: ownerToken, body: { username: viewer, role: 'viewer' } })
  const addPeer = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: ownerToken, body: { username: peer, role: 'viewer' } })
  ctx.collaborators = { filler: addFiller.json?.code, viewer: addViewer.json?.code, peer: addPeer.json?.code }
  if (addFiller.json?.code !== 0 || addViewer.json?.code !== 0) {
    record('SETUP-COLLAB', '协作者添加', 'BLOCKED', `filler=${addFiller.text}, viewer=${addViewer.text}`)
  }

  // ---------- 3. 字段类型与主字段保护（TC-040-01/02） ----------
  const numberFieldResp = await createField(ownerToken, firstTable.id, '数量', 'number', { format: 'decimal', decimals: 2, thousands: true })
  const dateFieldResp = await createField(ownerToken, firstTable.id, '日期', 'date', { include_time: true, format: 'YYYY-MM-DD' })
  const selectFieldResp = await createField(ownerToken, firstTable.id, '状态', 'select', { option_list: [{ name: '待办', color: 'blue' }, { name: '完成', color: 'green' }], allow_create: false })
  const urlFieldResp = await createField(ownerToken, firstTable.id, '链接', 'url', {})
  const numberField = apiData(numberFieldResp)?.field
  const dateField = apiData(dateFieldResp)?.field
  const selectField = apiData(selectFieldResp)?.field
  const urlField = apiData(urlFieldResp)?.field
  ctx.numberFieldId = numberField?.id
  ctx.dateFieldId = dateField?.id
  ctx.selectFieldId = selectField?.id
  ctx.urlFieldId = urlField?.id
  const fieldsAfter = apiData(await req('GET', `/api/v1/tables/${firstTable.id}/fields`, { token: ownerToken }))?.field_list ?? []
  record('TC-040-01', '新增字段（API 侧）',
    [numberField, dateField, selectField, urlField].every((f) => f?.id !== undefined) && fieldsAfter.length === fields.length + 4 ? 'PASS' : 'FAIL',
    `codes=${[numberFieldResp, dateFieldResp, selectFieldResp, urlFieldResp].map((r) => r.json?.code).join('/')}, fields=${fields.length}->${fieldsAfter.length}`)

  const primaryField = fieldsAfter.find((f) => f.is_primary)
  const delPrimary = await req('DELETE', `/api/v1/fields/${primaryField.id}`, { token: ownerToken, body: { confirm: true } })
  const convertPrimary = await req('POST', `/api/v1/fields/${primaryField.id}/convert`, { token: ownerToken, body: { target_type: 'number', confirm: true } })
  const renamePrimary = await req('PATCH', `/api/v1/fields/${primaryField.id}`, { token: ownerToken, body: { name: '标题（改名）' } })
  const fieldsAfterProtect = apiData(await req('GET', `/api/v1/tables/${firstTable.id}/fields`, { token: ownerToken }))?.field_list ?? []
  const protectedField = fieldsAfterProtect.find((f) => f.id === primaryField.id)
  record('TC-040-02', '主字段保护（API 侧）',
    delPrimary.json?.code !== 0 && convertPrimary.json?.code !== 0 && renamePrimary.json?.code === 0 &&
      protectedField?.is_primary === true && fieldsAfterProtect[0]?.id === primaryField.id ? 'PASS' : 'FAIL',
    `delete=${delPrimary.json?.code}, convert=${convertPrimary.json?.code}, rename=${renamePrimary.json?.code}, stillPrimary=${protectedField?.is_primary}, first=${fieldsAfterProtect[0]?.id === primaryField.id}`)
  ctx.primaryFieldId = primaryField.id

  // ---------- 4. 记录写入与读回（TC-050-04） ----------
  const optionId = selectField?.config_map?.option_list?.[0]?.id
  const createRecordResp = await req('POST', `/api/v1/bases/${base.id}/records`, {
    token: ownerToken,
    body: {
      table_id: firstTable.id,
      cells_map: {
        [primaryField.id]: '首条记录',
        [numberField.id]: 1234.5,
        [dateField.id]: 1730000000000,
        [selectField.id]: optionId,
        [urlField.id]: { url: 'https://example.com', display_text: '示例' },
      },
      client_request_id: `${RUN}-rec-1`,
    },
  })
  const record1 = apiData(createRecordResp)?.record
  const readBack = apiData(await req('GET', `/api/v1/records/${record1?.id}?base_id=${base.id}`, { token: ownerToken }))?.record
  const cells = decodeCells(readBack)
  const urlOk = typeof cells[urlField.id] === 'string' ? JSON.parse(cells[urlField.id] || '{}') : cells[urlField.id]
  const roundtripOk =
    cells[primaryField.id] === '首条记录' &&
    Number(cells[numberField.id]) === 1234.5 &&
    Number(cells[dateField.id]) === 1730000000000 &&
    cells[selectField.id] === optionId &&
    (urlOk?.url === 'https://example.com')
  record('TC-050-04', '提交落库正确性（多类型读回）', roundtripOk ? 'PASS' : 'FAIL',
    `create=${createRecordResp.json?.code}, cells=${JSON.stringify(cells)}`)

  // ---------- 5. 删除 + 7 秒撤销（TC-050-09 API 侧） ----------
  const delRec = await req('DELETE', `/api/v1/records?base_id=${base.id}`, { token: ownerToken, body: { record_id_list: [record1.id] } })
  const undoRec = await req('POST', `/api/v1/records/${record1.id}/undo?base_id=${base.id}`, { token: ownerToken })
  record('TC-050-09', '删除与撤销（API 侧）',
    delRec.json?.code === 0 && undoRec.json?.code === 0 && apiData(undoRec)?.record?.id === record1.id ? 'PASS' : 'FAIL',
    `delete=${delRec.json?.code}, undo=${undoRec.json?.code}`)

  // ---------- 6. 权限拒绝（TC-080-04 / TC-080-01 API 侧） ----------
  const versionNow = Number(apiData(await req('GET', `/api/v1/records/${record1.id}?base_id=${base.id}`, { token: ownerToken }))?.record?.version)
  const viewerWrite = await req('PUT', `/api/v1/records/${record1.id}/cells/${numberField.id}?base_id=${base.id}`, {
    token: viewerToken, body: { value: 99, expect_version: versionNow },
  })
  const fillerDeleteTable = await req('DELETE', `/api/v1/tables/${firstTable.id}`, { token: fillerToken, body: { confirm: true } })
  const fillerEditOther = await req('PUT', `/api/v1/records/${record1.id}/cells/${numberField.id}?base_id=${base.id}`, {
    token: fillerToken, body: { value: 88, expect_version: versionNow },
  })
  record('TC-080-04', '越权直调被拒', viewerWrite.json?.code === 3001 && fillerDeleteTable.json?.code === 3001 && fillerEditOther.json?.code === 3001 ? 'PASS' : 'FAIL',
    `viewerUpdate=${viewerWrite.json?.code}, fillerDeleteTable=${fillerDeleteTable.json?.code}, fillerEditOther=${fillerEditOther.json?.code}`)

  const viewerQuery = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${firstTable.id}&view_id=${ctx.viewId}`, { token: viewerToken })
  record('TC-080-02', '可查看只读（API 侧可读）', viewerQuery.json?.code === 0 ? 'PASS' : 'FAIL', `query=${viewerQuery.json?.code}`)

  const fillerCreate = await req('POST', `/api/v1/bases/${base.id}/records`, {
    token: fillerToken, body: { table_id: firstTable.id, cells_map: { [primaryField.id]: 'filler 自己的记录' } },
  })
  const fillerOwn = apiData(fillerCreate)?.record
  const fillerList = apiData(await req('GET', `/api/v1/bases/${base.id}/records?table_id=${firstTable.id}&view_id=${ctx.viewId}&limit=100`, { token: fillerToken }))?.record_list ?? []
  record('TC-080-01', '可填写数据范围（仅自己记录）',
    fillerCreate.json?.code === 0 && fillerList.every((item) => item.created_by === fillerOwn?.created_by) && fillerList.length >= 1 ? 'PASS' : 'FAIL',
    `create=${fillerCreate.json?.code}, visible=${fillerList.length}, creators=${[...new Set(fillerList.map((r) => r.created_by))].join(',')}`)

  // ---------- 7. 冲突与幂等（TC-100-01/02） ----------
  const freshRead = apiData(await req('GET', `/api/v1/records/${record1.id}?base_id=${base.id}`, { token: ownerToken }))?.record
  const baseVersion = Number(freshRead.version)
  const firstWrite = await req('PUT', `/api/v1/records/${record1.id}/cells/${numberField.id}?base_id=${base.id}`, {
    token: ownerToken, body: { value: 111, expect_version: baseVersion },
  })
  const secondWrite = await req('PUT', `/api/v1/records/${record1.id}/cells/${numberField.id}?base_id=${base.id}`, {
    token: ownerToken, body: { value: 222, expect_version: baseVersion },
  })
  const conflictData = secondWrite.json?.data
  record('TC-100-01', '版本冲突 4001',
    firstWrite.json?.code === 0 && secondWrite.json?.code === 4001 && conflictData?.their_value !== undefined && conflictData?.their_version !== undefined ? 'PASS' : 'FAIL',
    `first=${firstWrite.json?.code}, second=${secondWrite.json?.code}, data=${JSON.stringify(conflictData)}`)

  const idemBody = { table_id: firstTable.id, cells_map: { [primaryField.id]: '幂等记录' }, client_request_id: `${RUN}-idem-1` }
  const idem1 = await req('POST', `/api/v1/bases/${base.id}/records`, { token: ownerToken, body: idemBody })
  const idem2 = await req('POST', `/api/v1/bases/${base.id}/records`, { token: ownerToken, body: idemBody })
  const allRows = apiData(await req('GET', `/api/v1/bases/${base.id}/records?table_id=${firstTable.id}&view_id=${ctx.viewId}&limit=100`, { token: ownerToken }))?.record_list ?? []
  const idemRows = allRows.filter((row) => decodeCells(row)[primaryField.id] === '幂等记录')
  record('TC-100-02', '幂等重发', idem1.json?.code === 0 && idem2.json?.code === 0 && idemRows.length === 1 ? 'PASS' : 'FAIL',
    `first=${idem1.json?.code}, second=${idem2.json?.code}, rows=${idemRows.length}`)

  // ---------- 8. 删除表后不可达（TC-030-03） ----------
  const t3 = apiData(await req('POST', `/api/v1/bases/${base.id}/tables`, { token: ownerToken, body: { name: '待删表' } }))?.table
  let t3Fields = apiData(await req('GET', `/api/v1/tables/${t3.id}/fields`, { token: ownerToken }))?.field_list ?? []
  if (t3Fields.length === 0) {
    // 新表无默认字段（当前实现），补建一个以验证删除级联
    await createField(ownerToken, t3.id, '标题', 'text', {})
    t3Fields = apiData(await req('GET', `/api/v1/tables/${t3.id}/fields`, { token: ownerToken }))?.field_list ?? []
  }
  await req('POST', `/api/v1/bases/${base.id}/records`, { token: ownerToken, body: { table_id: t3.id, cells_map: { [t3Fields[0].id]: '待删数据' } } })
  const delT3 = await req('DELETE', `/api/v1/tables/${t3.id}`, { token: ownerToken, body: { confirm: true } })
  const queryDeleted = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${t3.id}&view_id=${ctx.viewId}`, { token: ownerToken })
  record('TC-030-03', '删除表后记录/视图不可达',
    delT3.json?.code === 0 && queryDeleted.json?.code !== 0 ? 'PASS' : 'FAIL',
    `delete=${delT3.json?.code}, queryAfter=${queryDeleted.json?.code}`)

  // ---------- 9. WS（TC-090-01/02/08） ----------
  try {
    const a1 = await wsConnect(ownerToken, base.id, firstTable.id)
    await a1.ws.send(JSON.stringify({ msg_id: 'j1', join: { base_id: base.id, table_id: firstTable.id } }))
    const snap1 = await a1.waitFrame((f) => f.snapshot !== undefined)
    const a8 = await wsConnect(peerToken, base.id, firstTable.id)
    // peer 不是协作者，但 WS 仅校验会话；join 仍可成功（记录帧语义由接口鉴权兜底）
    await a8.ws.send(JSON.stringify({ msg_id: 'j2', join: { base_id: base.id, table_id: firstTable.id } }))
    const snap2 = await a8.waitFrame((f) => f.snapshot !== undefined)
    const presence = await a1.waitFrame((f) => f.presence !== undefined && f.presence.online === true, 5000).catch(() => null)
    const members2 = snap2.snapshot?.member_list ?? []
    record('TC-090-01', 'join/snapshot',
      snap1.snapshot !== undefined && snap2.snapshot !== undefined && members2.length >= 2 && presence !== null ? 'PASS' : 'FAIL',
      `seq1=${snap1.snapshot?.seq}, seq2=${snap2.snapshot?.seq}, members2=${members2.length}, presence=${presence !== null}`)

    const beforeCount = a8.frames.length
    const upd = await req('PUT', `/api/v1/records/${record1.id}/cells/${numberField.id}?base_id=${base.id}`, {
      token: ownerToken, body: { value: 321, expect_version: Number(firstWrite.json?.data?.version ?? 0) },
    })
    const changed = await a8.waitFrame((f) => f.cell_changed !== undefined && f.cell_changed.record_id === record1.id, 4000).catch(() => null)
    record('TC-090-02', 'cell_changed 广播',
      upd.json?.code === 0 && changed !== null && Number(changed.cell_changed.seq) >= 1 ? 'PASS' : 'FAIL',
      `update=${upd.json?.code}, frame=${changed === null ? 'none' : JSON.stringify(changed.cell_changed)}, frames=${a8.frames.length - beforeCount}`)

    // TC-090-08：外部协作者在线时移除 → session_revoked + 连接终止
    const ext = `ext${RUN}`
    const extReg = await register(ext, {}) // 外部账号（无企业）
    const extToken = extReg.json?.code === 0 ? await login(ext) : ''
    const addExt = await req('POST', `/api/v1/bases/${base.id}/shares/members`, { token: ownerToken, body: { username: ext, role: 'editor' } })
    const extWs = await wsConnect(extToken, base.id, firstTable.id)
    await extWs.ws.send(JSON.stringify({ msg_id: 'j3', join: { base_id: base.id, table_id: firstTable.id } }))
    await extWs.waitFrame((f) => f.snapshot !== undefined)
    const collaborators = apiData(await req('GET', `/api/v1/bases/${base.id}/shares/members`, { token: ownerToken }))?.collaborator_list ?? []
    const extEntry = collaborators.find((item) => item.username === ext)
    const removed = await req('DELETE', `/api/v1/shares/members/${extEntry?.id ?? ext}`, { token: ownerToken, body: { base_id: base.id } })
    const revoked = await extWs.waitFrame((f) => f.session_revoked !== undefined, 65000).catch(() => null)
    let closed = false
    await new Promise((resolve) => {
      const timer = setTimeout(() => resolve(), 3000)
      extWs.ws.on('close', () => { closed = true; clearTimeout(timer); resolve() })
      if (extWs.ws.readyState === WebSocket.CLOSED) { closed = true; clearTimeout(timer); resolve() }
    })
    record('TC-090-08', '会话收回 session_revoked',
      addExt.json?.code === 0 && removed.json?.code === 0 && revoked !== null && closed ? 'PASS' : 'FAIL',
      `addExt=${addExt.json?.code}, remove=${removed.json?.code}, frame=${revoked === null ? 'none' : JSON.stringify(revoked.session_revoked)}, closed=${closed}`)

    a1.close(); a8.close(); extWs.close()
  } catch (error) {
    record('TC-090-01', 'join/snapshot', 'BLOCKED', String(error))
    record('TC-090-02', 'cell_changed 广播', 'BLOCKED', String(error))
    record('TC-090-08', '会话收回', 'BLOCKED', String(error))
  }

  // ---------- 10. 分享链接（TC-130-01/02） ----------
  const linkUpdate = await req('PUT', `/api/v1/bases/${base.id}/link`, { token: ownerToken, body: { scope: 'anyone', role: 'filler' } })
  const linkData = apiData(linkUpdate)?.link_config
  const linkUrl = linkData?.url ?? ''
  const oldToken = linkUrl.split('/').pop()
  const anonVisit = await req('GET', `/s/${oldToken}`, { redirect: 'manual' })
  const anonLocation = anonVisit.headers.get('location') || ''

  const linkUser = `lnk${RUN}`
  const linkReg = await register(linkUser, { link_token: oldToken })
  // 注册响应携带 link_context 的会话票据（重新登录会丢失链接上下文）
  const linkSessionToken = linkReg.json?.data?.token ?? ''
  let linkAccess = null
  if (linkSessionToken !== '') {
    linkAccess = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSessionToken })
  }
  record('TC-130-01', '任何人链接开通→进入（API/入口侧）',
    linkUpdate.json?.code === 0 && anonVisit.status === 302 && anonLocation.includes('/register?link_token=') && linkReg.json?.code === 0 && linkAccess?.json?.code === 0 ? 'PASS' : 'FAIL',
    `link=${linkUpdate.json?.code}, url=${linkUrl}, anon=${anonVisit.status} ${anonLocation}, register=${linkReg.json?.code}, baseAccess=${linkAccess?.json?.code}`)

  const reset = await req('POST', `/api/v1/bases/${base.id}/link/reset`, { token: ownerToken })
  const newUrl = apiData(reset)?.link_config?.url ?? ''
  const oldVisitAfterReset = await req('GET', `/s/${oldToken}`, { redirect: 'manual', token: linkSessionToken })
  const oldLocation = oldVisitAfterReset.headers.get('location') || ''
  // 旧链接会话：最多等 65s 让 55s 缓存过期 + 1 分钟熔断生效
  let staleDenied = false
  let staleDetail = ''
  const deadline = Date.now() + 70000
  while (Date.now() < deadline) {
    const probe = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: linkSessionToken })
    if (probe.json?.code !== 0) { staleDenied = true; staleDetail = `code=${probe.json?.code} after ${Math.round((Date.now() - (deadline - 70000)) / 1000)}s`; break }
    await sleep(5000)
  }
  record('TC-130-02', '重置链接失效',
    reset.json?.code === 0 && newUrl !== '' && newUrl !== linkUrl && oldVisitAfterReset.status === 302 && oldLocation.includes('/err?kind=share-link-unavailable') && staleDenied ? 'PASS' : 'FAIL',
    `reset=${reset.json?.code}, newDiff=${newUrl !== linkUrl}, oldVisit=${oldVisitAfterReset.status} ${oldLocation}, staleSession=${staleDetail || 'still allowed after 70s'}`)

  // ---------- 11. 导入导出（TC-140-01/02/09） ----------
  const fixtures = path.join(OUT_DIR, 'fixtures')
  const csvPath = path.join(fixtures, 'import.csv')
  const xlsxPath = path.join(fixtures, 'import.xlsx')

  const importOnce = async (filePath, label, columnTypes) => {
    const up = await uploadFile(ownerToken, base.id, filePath, 'import_temp')
    const fileId = apiData(up)?.id
    if (up.json?.code !== 0) return { ok: false, detail: `upload=${up.json?.code} ${up.text}` }
    const preview = await req('POST', '/api/v1/imports/preview', { token: ownerToken, body: { file_id: fileId, sheet_index: 0 } })
    const previewCols = apiData(preview)?.preview?.column_list ?? []
    const colConfigs = previewCols.map((col, index) => ({
      index: col.index ?? index,
      name: col.name || `列${index + 1}`,
      type: columnTypes[index] ?? 'text',
      ignore: false,
    }))
    const create = await req('POST', '/api/v1/imports', {
      token: ownerToken,
      body: { file_id: fileId, sheet_index: 0, first_row_header: true, column_config_list: colConfigs, client_request_id: `${RUN}-${label}-import` },
    })
    const taskId = apiData(create)?.import_task?.id
    if (create.json?.code !== 0) return { ok: false, detail: `create=${create.json?.code} ${create.text}` }
    let task = apiData(create)?.import_task
    for (let i = 0; i < 60; i += 1) {
      await sleep(1000)
      const status = await req('GET', `/api/v1/imports/${taskId}`, { token: ownerToken })
      task = apiData(status)?.import_task
      if (task?.status === 'IMPORT_STATUS_DONE' || task?.status === 'IMPORT_STATUS_FAILED') break
    }
    let rows = []
    if (task?.base_id) {
      // DONE 与 import.finished 解锁消费之间有毫秒级窗口：立即读表会撞上
      // 「导入中不可访问」3001 而拿到空列表，轮询等待解锁完成（≤15s）。
      let importedTables = []
      for (let i = 0; i < 15 && importedTables.length === 0; i += 1) {
        await sleep(1000)
        importedTables = apiData(await req('GET', `/api/v1/bases/${task.base_id}/tables`, { token: ownerToken }))?.table_list ?? []
      }
      if (importedTables[0]) {
        const importedViews = apiData(await req('GET', `/api/v1/tables/${importedTables[0].id}/views`, { token: ownerToken }))?.view_list ?? []
        const query = new URLSearchParams({ table_id: importedTables[0].id, limit: '100' })
        if (importedViews[0]?.id) query.set('view_id', importedViews[0].id)
        rows = apiData(await req('GET', `/api/v1/bases/${task.base_id}/records?${query.toString()}`, { token: ownerToken }))?.record_list ?? []
      }
    }
    return { ok: task?.status === 'IMPORT_STATUS_DONE' && rows.length === 3, detail: `previewCols=${previewCols.length}, create=${create.json?.code}, status=${task?.status}, base=${task?.base_id}, rows=${rows.length}, ok_rows=${task?.ok_rows}, fail_rows=${task?.fail_rows}`, task, rows }
  }

  const csvImport = await importOnce(csvPath, 'csv', ['text', 'number'])
  record('TC-140-02', 'csv 导入', csvImport.ok ? 'PASS' : 'FAIL', csvImport.detail)
  const xlsxImport = await importOnce(xlsxPath, 'xlsx', ['text', 'number'])
  record('TC-140-01', 'xlsx 导入全流程', xlsxImport.ok ? 'PASS' : 'FAIL', xlsxImport.detail)

  // 导出：筛选 + 隐藏字段
  const hiddenField = urlField
  const cfg = {
    view_config: {
      hidden_field_list: [hiddenField.id],
      frozen_field_list: [primaryField.id],
      filter_list: [{ field_id: primaryField.id, op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: '首条' } }],
      sort_list: [],
      group_by_list: [],
      row_height: 'ROW_HEIGHT_MEDIUM',
      field_width_map: { [primaryField.id]: 200 },
    },
  }
  const cfgPut = await req('PUT', `/api/v1/views/${ctx.viewId}/config`, { token: ownerToken, body: cfg })
  const exportResp = await req('POST', '/api/v1/exports', {
    token: ownerToken,
    body: { base_id: base.id, table_id: firstTable.id, view_id: ctx.viewId, format: 'csv', client_request_id: `${RUN}-export-1` },
  })
  const exportTask = apiData(exportResp)?.export_task
  let exportFile = null
  for (let i = 0; i < 30; i += 1) {
    const download = await req('GET', `/api/v1/exports/${exportTask?.id}/download`, { token: ownerToken })
    if (download.status === 200 && download.buf.length > 0) { exportFile = download; break }
    await sleep(1000)
  }
  const fileName = exportFile?.headers.get('x-file-name') || ''
  const csvText = exportFile?.buf?.toString('utf8') ?? ''
  const nameOk = /^.+_.+_.+_\d{8}\.csv$/.test(fileName)
  const rowsOk = csvText.includes('首条记录') && !csvText.includes('filler 自己的记录') && !csvText.includes('幂等记录')
  const hiddenOk = !csvText.includes('https://example.com') && !csvText.includes('示例')
  record('TC-140-09', '导出范围与命名',
    cfgPut.json?.code === 0 && exportResp.json?.code === 0 && exportFile !== null && nameOk && rowsOk && hiddenOk ? 'PASS' : 'FAIL',
    `cfg=${cfgPut.json?.code}, export=${exportResp.json?.code}, file=${fileName || 'none'}, filterRows=${rowsOk}, hidden=${hiddenOk}, head=${JSON.stringify(csvText.slice(0, 120))}`)

  // ---------- 12. 登出（TC-010-08） ----------
  const logoutToken = await login(owner)
  const logout = await req('POST', '/api/v1/auth/logout', { token: logoutToken })
  const afterLogout = await req('GET', '/api/v1/auth/me', { token: logoutToken })
  record('TC-010-08', '登出后会话失效', logout.json?.code === 0 && afterLogout.json?.code === 2001 ? 'PASS' : 'FAIL',
    `logout=${logout.json?.code}, meAfter=${afterLogout.json?.code}`)

  // ---------- 13. 错误码 2001 / 2002（TC-160-01/02） ----------
  const noSessionWrite = await req('POST', `/api/v1/bases/${base.id}/records`, { body: { table_id: firstTable.id, cells_map: {} } })
  record('TC-160-01', '2001 未登录', noSessionWrite.json?.code === 2001 ? 'PASS' : 'FAIL',
    `code=${noSessionWrite.json?.code}, status=${noSessionWrite.status}`)

  const lockUser = `lk${RUN}`
  await register(lockUser, { enterprise_name: `锁测企业${RUN.slice(-3)}` })
  const wrongMessages = []
  for (let i = 0; i < 6; i += 1) {
    const attempt = await req('POST', '/api/v1/auth/login', { body: { username: lockUser, password: 'Wrong1234' } })
    wrongMessages.push({ code: attempt.json?.code, message: attempt.json?.message })
  }
  const badCred = wrongMessages[0]?.message === '用户名或密码错误'
  const locked = wrongMessages.slice(-1)[0]?.message === '失败次数过多，请稍后再试'
  record('TC-160-02', '2002 失败/锁定区分', badCred && locked ? 'PASS' : 'FAIL',
    JSON.stringify(wrongMessages))

  // ---------- 14. 删除 Base（TC-020-03 API 侧） ----------
  const delBase = await req('DELETE', `/api/v1/bases/${base.id}`, { token: ownerToken, body: { confirm: true } })
  const afterDelete = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: ownerToken })
  record('TC-020-03', '删除 Base 后不可访问（API 侧）', delBase.json?.code === 0 && afterDelete.json?.code !== 0 ? 'PASS' : 'FAIL',
    `delete=${delBase.json?.code}, tablesAfter=${afterDelete.json?.code}`)

  fs.writeFileSync(path.join(OUT_DIR, 'p0-api-results.json'), JSON.stringify({ run: RUN, base: BASE, results }, null, 2))
  const summary = results.reduce((acc, item) => { acc[item.status] = (acc[item.status] || 0) + 1; return acc }, {})
  console.log('\nSUMMARY', JSON.stringify(summary))
  const failed = results.filter((item) => item.status !== 'PASS')
  if (failed.length > 0) {
    console.log('NON-PASS:', failed.map((item) => item.id).join(', '))
  }
}

main().catch((error) => {
  console.error('FATAL', error)
  fs.writeFileSync(path.join(OUT_DIR, 'p0-api-results.json'), JSON.stringify({ run: RUN, fatal: String(error), results }, null, 2))
  process.exit(1)
})
