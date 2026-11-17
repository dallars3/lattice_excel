const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const run = process.argv[2]

async function req(method, p, { token, body } = {}) {
  const h = {}
  if (token) h.Authorization = `Bearer ${token}`
  if (body !== undefined) h['Content-Type'] = 'application/json'
  const r = await fetch(BASE + p, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) })
  const text = await r.text()
  let json = null
  try { json = JSON.parse(text) } catch {}
  return { status: r.status, json, text }
}

const login = await req('POST', '/api/v1/auth/login', { body: { username: `p1own${run}`, password: PWD } })
console.log('login', login.json?.code)
const token = login.json?.data?.token
const home = await req('GET', '/api/v1/home', { token })
const all = [...(home.json?.data?.created_list ?? []), ...(home.json?.data?.recent_list ?? []), ...(home.json?.data?.star_list ?? []), ...(home.json?.data?.shared_list ?? [])]
const base = all.find((item) => item.name?.includes(run))
console.log('base', base?.id, base?.name)
const tables = await req('GET', `/api/v1/bases/${base.id}/tables`, { token })
const table = tables.json?.data?.table_list?.[0]
console.log('table', table?.id)
const fields = await req('GET', `/api/v1/tables/${table.id}/fields`, { token })
const byName = Object.fromEntries((fields.json?.data?.field_list ?? []).map((f) => [f.name, f.id]))
console.log('fields', Object.keys(byName).join(','))
console.log('--- create 数量=0 ---')
console.log((await req('POST', `/api/v1/bases/${base.id}/records`, { token, body: { table_id: table.id, cells_map: { [byName['标题']]: '记录B', [byName['数量']]: 0 }, client_request_id: `probe-${Date.now()}` } })).text)
console.log('--- create 数量=3 ---')
console.log((await req('POST', `/api/v1/bases/${base.id}/records`, { token, body: { table_id: table.id, cells_map: { [byName['标题']]: '记录C', [byName['数量']]: 3 }, client_request_id: `probe2-${Date.now()}` } })).text)
console.log('--- create only title ---')
console.log((await req('POST', `/api/v1/bases/${base.id}/records`, { token, body: { table_id: table.id, cells_map: { [byName['标题']]: '记录D' }, client_request_id: `probe3-${Date.now()}` } })).text)
