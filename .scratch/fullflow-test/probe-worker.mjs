import net from 'node:net'
import fs from 'node:fs'

const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = process.argv[2] || 'p4u9efb'

async function req(method, p, { token, body, form } = {}) {
  const h = {}
  if (token) h.Authorization = `Bearer ${token}`
  let payload
  if (form) payload = form
  else if (body !== undefined) { h['Content-Type'] = 'application/json'; payload = JSON.stringify(body) }
  const r = await fetch(BASE + p, { method, headers: h, body: payload })
  const text = await r.text()
  let json = null
  try { json = JSON.parse(text) } catch {}
  return { status: r.status, json, text }
}

function workerAlive() {
  return new Promise((resolve) => {
    const socket = net.connect(9092, '127.0.0.1')
    const done = (ok) => { socket.destroy(); resolve(ok) }
    socket.setTimeout(1500)
    socket.on('connect', () => done(true))
    socket.on('error', () => done(false))
    socket.on('timeout', () => done(false))
  })
}

const login = await req('POST', '/api/v1/auth/login', { body: { username: `p1own${RUN}`, password: PWD } })
const token = login.json?.data?.token
console.log('login', login.json?.code, 'worker', await workerAlive())

const home = await req('GET', '/api/v1/home', { token })
const all = [...(home.json?.data?.created_list ?? []), ...(home.json?.data?.recent_list ?? []), ...(home.json?.data?.star_list ?? [])]
const base = all.find((item) => item.name?.includes(RUN.slice(-4)))
console.log('base', base?.id, 'worker', await workerAlive())
const tables = await req('GET', `/api/v1/bases/${base.id}/tables`, { token })
const table = tables.json?.data?.table_list?.[0]
const fields = await req('GET', `/api/v1/tables/${table.id}/fields`, { token })
console.log('table', table?.id, 'fields', fields.json?.data?.field_list?.length, 'worker', await workerAlive())

// 1) share.changed：调整协作者角色
const collaborators = await req('GET', `/api/v1/bases/${base.id}/shares/members`, { token })
const ext = (collaborators.json?.data?.collaborator_list ?? []).find((item) => String(item.username).startsWith('p1ext'))
console.log('ext', ext?.username, ext?.role)
if (ext) {
  const change = await req('PUT', `/api/v1/shares/members/${ext.id}/role`, { token, body: { base_id: base.id, role: 'viewer' } })
  console.log('share.changed ->', change.json?.code)
  await new Promise((r) => setTimeout(r, 1500))
  console.log('worker', await workerAlive())
  await req('PUT', `/api/v1/shares/members/${ext.id}/role`, { token, body: { base_id: base.id, role: 'editor' } })
  await new Promise((r) => setTimeout(r, 1500))
  console.log('worker after revert', await workerAlive())
}

// 2) 文件上传（attachment）
const form = new FormData()
form.append('usage', 'attachment')
form.append('base_id', base.id)
form.append('file', new Blob([Buffer.from('probe attachment')]), 'probe.txt')
const upload = await req('POST', '/api/v1/files', { token, form })
console.log('upload ->', upload.json?.code, upload.text.slice(0, 120))
await new Promise((r) => setTimeout(r, 1500))
console.log('worker', await workerAlive())

// 3) 导入
const csv = Buffer.from('名称,数量\n甲,1\n乙,2\n')
const form2 = new FormData()
form2.append('usage', 'import_temp')
form2.append('base_id', base.id)
form2.append('file', new Blob([csv]), 'probe.csv')
const upload2 = await req('POST', '/api/v1/files', { token, form: form2 })
console.log('import upload ->', upload2.json?.code)
const fileId = upload2.json?.data?.id
const preview = await req('POST', '/api/v1/imports/preview', { token, body: { file_id: fileId, sheet_index: 0 } })
console.log('preview ->', preview.json?.code, JSON.stringify(preview.json?.data?.preview?.column_list?.map((c) => c.name)))
const create = await req('POST', '/api/v1/imports', {
  token,
  body: { file_id: fileId, sheet_index: 0, first_row_header: true, column_config_list: [{ index: 0, name: '名称', type: 'text', ignore: false }, { index: 1, name: '数量', type: 'number', ignore: false }], client_request_id: `probe-import-${Date.now()}` },
})
console.log('import create ->', create.json?.code, create.text.slice(0, 160))
await new Promise((r) => setTimeout(r, 3000))
console.log('worker', await workerAlive())

// 4) 导出
const views = await req('GET', `/api/v1/tables/${table.id}/views`, { token })
const exportResp = await req('POST', '/api/v1/exports', { token, body: { base_id: base.id, table_id: table.id, view_id: views.json?.data?.view_list?.[0]?.id, format: 'csv', client_request_id: `probe-export-${Date.now()}` } })
console.log('export ->', exportResp.json?.code, exportResp.text.slice(0, 160))
await new Promise((r) => setTimeout(r, 2000))
console.log('worker', await workerAlive())
