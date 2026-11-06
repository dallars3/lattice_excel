const BASE = 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const RUN = process.argv[2] || 'p4ulbaf'

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

const ownerLogin = await req('POST', '/api/v1/auth/login', { body: { username: `p1own${RUN}`, password: PWD } })
const owner = ownerLogin.json?.data?.token
const peerLogin = await req('POST', '/api/v1/auth/login', { body: { username: `p1peer${RUN}`, password: PWD } })
const peer = peerLogin.json?.data?.token
console.log('logins', ownerLogin.json?.code, peerLogin.json?.code)

const home = await req('GET', '/api/v1/home', { token: owner })
const all = [...(home.json?.data?.created_list ?? []), ...(home.json?.data?.recent_list ?? []), ...(home.json?.data?.star_list ?? []), ...(home.json?.data?.shared_list ?? [])]
const base = all.find((item) => item.name?.includes(RUN.slice(-4)))
console.log('base', base?.id, base?.name)
const tables = await req('GET', `/api/v1/bases/${base.id}/tables`, { token: owner })
const table = tables.json?.data?.table_list?.[0]
const fields = await req('GET', `/api/v1/tables/${table.id}/fields`, { token: owner })
const fieldList = fields.json?.data?.field_list ?? []
const byName = Object.fromEntries(fieldList.map((f) => [f.name, f.id]))
const records = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&limit=10`, { token: owner })
const record = (records.json?.data?.record_list ?? [])[0]
console.log('record', record?.id, 'fields', fieldList.length)

console.log('--- duplicate base ---')
console.log((await req('POST', `/api/v1/bases/${base.id}/duplicate`, { token: owner })).text.slice(0, 200))

console.log('--- options baseline ---')
const selectField = fieldList.find((f) => f.name === '单选')
const optionList = (selectField?.config_map?.option_list ?? []).map((o) => ({ id: o.id, name: o.name, color: o.color }))
console.log(JSON.stringify(optionList))
console.log((await req('PUT', `/api/v1/fields/${selectField.id}/options`, { token: owner, body: { option_list: optionList, allow_create: false } })).text.slice(0, 300))

console.log('--- peer GET record ---')
const peerGet = await req('GET', `/api/v1/records/${record.id}?base_id=${base.id}`, { token: peer })
console.log(peerGet.text.slice(0, 200))
console.log('--- peer list records ---')
const peerList = await req('GET', `/api/v1/bases/${base.id}/records?table_id=${table.id}&limit=10`, { token: peer })
console.log(peerList.text.slice(0, 240))
console.log('--- peer stale write ---')
const currentVersion = Number(record.version)
const peerWrite = await req('PUT', `/api/v1/records/${record.id}/cells/${byName['数量']}?base_id=${base.id}`, { token: peer, body: { value: 91, expect_version: currentVersion } })
console.log(peerWrite.text.slice(0, 200))
const peerStale = await req('PUT', `/api/v1/records/${record.id}/cells/${byName['数量']}?base_id=${base.id}`, { token: peer, body: { value: 92, expect_version: currentVersion } })
console.log(peerStale.text.slice(0, 240))

console.log('--- duplicate table name ---')
const dupName = `probe重名${Date.now().toString(36).slice(-3)}`
const t1 = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: owner, body: { name: dupName } })
const t2 = await req('POST', `/api/v1/bases/${base.id}/tables`, { token: owner, body: { name: dupName } })
console.log('first', t1.json?.code, 'second', t2.json?.code, t2.text.slice(0, 160))
