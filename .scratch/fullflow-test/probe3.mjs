const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = process.argv[2] || 'uipvs8h0'

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

const login = await req('POST', '/api/v1/auth/login', { body: { username: `p1own${RUN}`, password: PWD } })
const token = login.json?.data?.token
const home = await req('GET', '/api/v1/home', { token })
const all = [...(home.json?.data?.created_list ?? []), ...(home.json?.data?.recent_list ?? []), ...(home.json?.data?.star_list ?? [])]
const base = all.find((item) => item.name?.includes(RUN.slice(-4)))
console.log('base', base?.id, base?.name)
const tables = await req('GET', `/api/v1/bases/${base.id}/tables`, { token })
const table = tables.json?.data?.table_list?.find((t) => t.name === '表格') ?? tables.json?.data?.table_list?.[0]
const fields = await req('GET', `/api/v1/tables/${table.id}/fields`, { token })
const byName = Object.fromEntries((fields.json?.data?.field_list ?? []).map((f) => [f.name, f]))
console.log('multi', JSON.stringify(byName['多选']?.config_map), 'type', byName['多选']?.type)
console.log('person', JSON.stringify(byName['人员']?.config_map), 'type', byName['人员']?.type)
const records = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&limit=10`, { token })
const record = (records.json?.data?.record_list ?? []).find((r) => JSON.stringify(r.cells_map).includes('记录B')) ?? (records.json?.data?.record_list ?? [])[1]
console.log('record', record?.id, 'version', record?.version)
const optionIds = (byName['多选']?.config_map?.option_list ?? []).map((o) => o.id)
console.log('optionIds', optionIds)
console.log('multi write', (await req('PUT', `/api/v1/records/${record.id}/cells/${byName['多选'].id}?base_id=${base.id}`, { token, body: { value: optionIds, expect_version: Number(record.version) } })).text)
const after = await req('GET', `/api/v1/records/${record.id}?base_id=${base.id}`, { token })
console.log('read', after.text.slice(0, 400))
console.log('person write', (await req('PUT', `/api/v1/records/${record.id}/cells/${byName['人员'].id}?base_id=${base.id}`, { token, body: { value: ['01M6R9A15M2QR7MV9V84CMWNHP'], expect_version: Number(record.version) + 1 } })).text)
