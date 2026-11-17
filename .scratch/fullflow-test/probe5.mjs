const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = process.argv[2] || 'uipxhav6'

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
const tables = await req('GET', `/api/v1/bases/${base.id}/tables`, { token })
const table = tables.json?.data?.table_list?.find((t) => t.name === '表格') ?? tables.json?.data?.table_list?.[0]
const views = await req('GET', `/api/v1/tables/${table.id}/views`, { token })
const view = views.json?.data?.view_list?.[0]
const fields = await req('GET', `/api/v1/tables/${table.id}/fields`, { token })
const title = (fields.json?.data?.field_list ?? []).find((f) => f.is_primary)
const number = (fields.json?.data?.field_list ?? []).find((f) => f.name === '数量')
const cfgResp = await req('GET', `/api/v1/views/${view.id}/config`, { token })
const config = cfgResp.json?.data?.view_config ?? {}
console.log('base', base.id, 'view', view.id, 'fields', title?.id, number?.id)

const filterCfg = { ...config, filter_list: [{ field_id: title.id, op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: '记录A' } }], sort_list: [] }
console.log('put filter', (await req('PUT', `/api/v1/views/${view.id}/config`, { token, body: { view_config: filterCfg } })).json?.code)
const filtered = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&view_id=${view.id}&limit=100`, { token })
console.log('filtered rows', (filtered.json?.data?.record_list ?? []).length, filtered.text.slice(0, 160))

const sortCfg = { ...config, filter_list: [], sort_list: [{ field_id: number.id, dir: 'SORT_DIR_ASC' }] }
console.log('put sort', (await req('PUT', `/api/v1/views/${view.id}/config`, { token, body: { view_config: sortCfg } })).json?.code)
const sorted = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&view_id=${view.id}&limit=100`, { token })
const titles = (sorted.json?.data?.record_list ?? []).map((r) => Object.values(r.cells_map ?? {}).find((v) => v.string_value)?.string_value)
console.log('sorted titles', JSON.stringify(titles))
await req('PUT', `/api/v1/views/${view.id}/config`, { token, body: { view_config: { ...config, filter_list: [], sort_list: [] } } })
