const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = process.argv[2] || 'uipwxjws'

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
const records = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&limit=5`, { token })
const record = records.json?.data?.record_list?.[0]
console.log('record', record?.id)

const created = await req('POST', `/api/v1/tables/${table.id}/fields`, { token, body: { name: `转换探针${Date.now().toString(36).slice(-3)}`, type: 'text', config_map: {} } })
console.log('create field', created.json?.code, created.text.slice(0, 120))
const field = created.json?.data?.field
if (field) {
  console.log('set value', (await req('PUT', `/api/v1/records/${record.id}/cells/${field.id}?base_id=${base.id}`, { token, body: { value: '123', expect_version: Number(record.version) } })).text.slice(0, 160))
  const preview = await req('POST', `/api/v1/fields/${field.id}/convert`, { token, body: { target_type: 'number', confirm: false } })
  console.log('convert preview', preview.json?.code, preview.text.slice(0, 220))
  const impact = await req('DELETE', `/api/v1/fields/${field.id}`, { token, body: { confirm: false } })
  console.log('delete impact', impact.json?.code, impact.text.slice(0, 220))
  const cleanup = await req('DELETE', `/api/v1/fields/${field.id}`, { token, body: { confirm: true } })
  console.log('cleanup', cleanup.json?.code)
}
