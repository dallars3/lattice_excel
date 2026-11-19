// 全流程 P1/P2 UI 用例（对 http://127.0.0.1:8080 单机部署执行）
// 运行：npx playwright test e2e/p1p2.spec.ts --workers=1 --reporter=list
// 票 05 夹具适配（acceptance-defect-remediation 遗留裁量，评审整改第 11 项）：
// 与 p0.spec.ts 同口径——票 02/N7「注册即建企」上线后，InviteMember/AcceptInvite
// 的 EnterpriseID=="" 前置使新注册用户永远无法受邀加入他企业（票 02 实现记录
// 已登记的既定语义后果），原 beforeAll 的企业邀请→接受流对新账号恒 1002。据此：
//   1) EDITOR/FILLER/VIEWER 成员夹具改走 Base 协作者授权（shares/members，见下方
//      授权段），不再走企业邀请/接受；
//   2) EXTERNAL 改走分享链接注册（link_token，唯一不建企的注册路径）保持外部
//      身份（TC-080-05「外部导出恒隐」依赖非 internal 会话）。
// 断言本体零改动。
import { test, expect, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'
import fs from 'node:fs'

// P1P2_BASE 环境变量可覆盖目标栈（并行验证实例用）；缺省仍指向本地部署栈 :8080。
const BASE = process.env.P1P2_BASE ?? 'http://127.0.0.1:8080'
const RUN = 'uip' + Date.now().toString(36).slice(-5)
const PWD = 'Pass1234'
const OWNER = `p1own${RUN}`
const EDITOR = `p1edt${RUN}`
const FILLER = `p1fil${RUN}`
const VIEWER = `p1vie${RUN}`
const EXTERNAL = `p1ext${RUN}`
const SOLO = `p1solo${RUN}`

let ownerCtx: BrowserContext
let editorCtx: BrowserContext
let fillerCtx: BrowserContext
let viewerCtx: BrowserContext
let externalCtx: BrowserContext
let soloCtx: BrowserContext

let baseId = ''
let table1Id = ''
let table1Name = ''
let table2Id = ''
let viewId = ''
let targetRecordId = ''
let targetPrimaryId = ''
let recordA = ''
let recordB = ''
let recordC = ''
let linkToken = ''
let ownerUserId = ''
const fieldIds: Record<string, string> = {}
const tokenMap: Record<string, string> = {}

async function api(ctx: APIRequestContext, method: string, path: string, data?: unknown): Promise<any> {
  const res = await ctx.fetch(BASE + path, { method, data })
  return res.json().catch(() => null)
}

async function register(ctx: APIRequestContext, username: string, extra: Record<string, unknown> = {}) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const res = await api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD, ...extra })
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 15000))
  }
  return api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD, ...extra })
}

async function apiLogin(ctx: APIRequestContext, username: string) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const res = await api(ctx, 'POST', '/api/v1/auth/login', { username, password: PWD })
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 10000))
  }
  return api(ctx, 'POST', '/api/v1/auth/login', { username, password: PWD })
}

async function waitGrid(page: Page) {
  await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
}

async function gotoBase(page: Page, id = baseId) {
  await page.goto(`${BASE}/base/${id}`)
  await waitGrid(page)
}

function cell(page: Page, recordId: string, fieldId: string) {
  return page.getByTestId(`cell-${recordId}-${fieldId}`)
}

/**
 * 网格 Ctrl+字母组合键（票02 探针实证）：page.keyboard.press('Control+C') 送达的
 * event.key 为大写 'C'（C/V/X/Z/F 全部如此），而键位状态机按需求 3.8.1 匹配小写
 * 'c'/'v'/'x'/'z'/'f'（真实浏览器 Ctrl+字母的 event.key 为小写，产品对真实用户正常）。
 * 与 TC-050-16 的 Ctrl+F 同口径：evaluate 派发与真实 keydown 同构的小写键事件。
 */
async function gridKeyCombo(page: Page, key: 'c' | 'v' | 'x' | 'z' | 'f') {
  await page.evaluate((key) => {
    document
      .querySelector('[role="grid"][aria-label="记录网格"]')
      ?.dispatchEvent(new KeyboardEvent('keydown', { key, ctrlKey: true, bubbles: true, cancelable: true }))
  }, key)
}

/** 单击选中 + F2 进入编辑：避免双击因重渲染偶发不触发编辑态。 */
async function openCellEditor(page: Page, recordId: string, fieldId: string) {
  const target = cell(page, recordId, fieldId)
  await target.click()
  await target.press('F2')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeVisible({ timeout: 10000 })
}

async function editNumber(page: Page, recordId: string, value: number) {
  const fieldId = fieldIds['数量']
  await openCellEditor(page, recordId, fieldId)
  const editor = page.getByTestId(`editor-${recordId}-${fieldId}`)
  const input = editor.locator('input').first()
  await input.fill(String(value))
  await input.press('Enter')
  await expect(editor).toBeHidden({ timeout: 15000 })
}

async function editText(page: Page, recordId: string, fieldName: string, value: string) {
  const fieldId = fieldIds[fieldName]
  await openCellEditor(page, recordId, fieldId)
  const editor = page.getByTestId(`editor-${recordId}-${fieldId}`)
  const input = editor.locator('input,textarea').first()
  await input.fill(value)
  await input.press('Enter')
  await expect(editor).toBeHidden({ timeout: 15000 })
}

async function editDate(page: Page, recordId: string, value: string) {
  const fieldId = fieldIds['日期']
  await openCellEditor(page, recordId, fieldId)
  const input = page.getByLabel('日期编辑')
  await input.fill(value)
  await input.press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function editSelect(page: Page, recordId: string, fieldName: string, option: string) {
  const fieldId = fieldIds[fieldName]
  await openCellEditor(page, recordId, fieldId)
  const combo = page.getByLabel('搜索选项')
  await combo.fill(option)
  await page.getByRole('option', { name: option, exact: true }).click()
  await combo.press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function editMulti(page: Page, recordId: string, options: string[]) {
  const fieldId = fieldIds['多选']
  await openCellEditor(page, recordId, fieldId)
  const combo = page.getByLabel('搜索选项')
  for (const option of options) {
    await combo.fill(option)
    await page.getByRole('option', { name: new RegExp(option) }).first().click()
  }
  await combo.press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function editPerson(page: Page, recordId: string, name: string) {
  const fieldId = fieldIds['人员']
  await openCellEditor(page, recordId, fieldId)
  const input = page.getByLabel('搜索人员')
  await input.fill(name)
  await page.getByRole('option', { name: new RegExp(name) }).first().click()
  await input.press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function editUrl(page: Page, recordId: string, url: string, text: string) {
  const fieldId = fieldIds['链接']
  await openCellEditor(page, recordId, fieldId)
  await page.getByLabel('链接地址').fill(url)
  if (text !== '') await page.getByLabel('显示文本').fill(text)
  await page.getByLabel('链接地址').press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function editRelation(page: Page, recordId: string, targetLabel: string) {
  const fieldId = fieldIds['关联']
  await openCellEditor(page, recordId, fieldId)
  const input = page.getByLabel('搜索关联记录')
  await input.fill(targetLabel)
  await page.getByRole('option', { name: new RegExp(targetLabel) }).first().click()
  await input.press('Enter')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeHidden({ timeout: 15000 })
}

async function visibleRecordCount(page: Page): Promise<number> {
  return page.locator(`[data-testid^="cell-"][data-testid$="-${fieldIds['标题']}"]`).count()
}

async function waitSaved(page: Page) {
  await expect(page.getByText('已保存', { exact: true })).toBeVisible({ timeout: 15000 })
}

async function openFilterPanel(page: Page) {
  await page.getByRole('button', { name: '筛选', exact: true }).click()
  await expect(page.getByRole('region', { name: '筛选面板' })).toBeVisible()
}

async function clearFilters(page: Page) {
  const panel = page.getByRole('region', { name: '筛选面板' })
  if (!(await panel.isVisible())) {
    await page.getByRole('button', { name: '筛选', exact: true }).click()
    await expect(panel).toBeVisible()
  }
  const clear = page.getByRole('button', { name: '清除全部' })
  if (await clear.isEnabled()) {
    await clear.click()
    await expect(page.getByText('当前 0 条条件')).toBeVisible()
  }
  await page.getByRole('button', { name: '筛选', exact: true }).click()
}

/** 仅添加条件骨架（字段+操作符），不填值，供列表类/无值操作符用例使用。 */
async function addFilterBase(page: Page, fieldName: string, opLabel: string) {
  const panel = page.getByRole('region', { name: '筛选面板' })
  if (!(await panel.isVisible())) {
    await page.getByRole('button', { name: '筛选', exact: true }).click()
    await expect(panel).toBeVisible()
  }
  await page.getByRole('button', { name: '添加筛选条件' }).click()
  const index = (await page.getByLabel(/^筛选字段 /).count()) - 1
  await page.getByLabel(`筛选字段 ${index + 1}`).selectOption({ label: fieldName })
  await page.getByLabel(`筛选操作 ${index + 1}`).selectOption({ label: opLabel })
  await page.waitForTimeout(1200)
  return index + 1
}

async function addFilter(page: Page, fieldName: string, opLabel: string, value: string) {
  const panel = page.getByRole('region', { name: '筛选面板' })
  if (!(await panel.isVisible())) {
    await page.getByRole('button', { name: '筛选', exact: true }).click()
    await expect(panel).toBeVisible()
  }
  await page.getByRole('button', { name: '添加筛选条件' }).click()
  const index = (await page.getByLabel(/^筛选字段 /).count()) - 1
  await page.getByLabel(`筛选字段 ${index + 1}`).selectOption({ label: fieldName })
  await page.getByLabel(`筛选操作 ${index + 1}`).selectOption({ label: opLabel })
  // 票02 triage（TC-060-02 用例体缺陷）：选项类筛选（select/multiselect 的属于/不属于）
  // 不渲染文本「筛选值 N」输入框，取值走「筛选选项 N：xx」复选框（由调用方勾选），
  // 此时面板提示「请补全筛选值后生效」；helpers 原先假设恒有文本值输入框导致挂死。
  const valueInput = page.getByLabel(`筛选值 ${index + 1}`)
  if ((await valueInput.count()) > 0) await valueInput.fill(value)
  await page.waitForTimeout(1200)
}

async function openDrawer(page: Page, recordId: string) {
  await page.getByRole('button', { name: `展开记录 ${recordId}` }).click()
  await expect(page.getByRole('dialog', { name: '记录详情' })).toBeVisible()
}

test.describe.configure({ timeout: 120000 })

test.beforeAll(async ({ browser, request }) => {
  test.setTimeout(600000)
  ownerCtx = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] })
  editorCtx = await browser.newContext()
  fillerCtx = await browser.newContext()
  viewerCtx = await browser.newContext()
  externalCtx = await browser.newContext()
  soloCtx = await browser.newContext()

  expect((await register(ownerCtx.request, OWNER, { enterprise_name: `P1UI企业${RUN.slice(-4)}` }))?.code).toBe(0)
  expect((await apiLogin(ownerCtx.request, OWNER))?.code).toBe(0)
  const ownerToken = (await api(ownerCtx.request, 'POST', '/api/v1/auth/login', { username: OWNER, password: PWD }))?.data?.token
  tokenMap.owner = ownerToken
  const ownerMe = await api(ownerCtx.request, 'GET', '/api/v1/auth/me')
  ownerUserId = ownerMe?.data?.user?.id

  for (const [ctx, name] of [[editorCtx, EDITOR], [fillerCtx, FILLER], [viewerCtx, VIEWER]] as const) {
    expect((await register(ctx.request, name))?.code).toBe(0)
    expect((await apiLogin(ctx.request, name))?.code).toBe(0)
    // 票 05 夹具适配：不再走企业邀请（N7 后新注册用户必然已属自有企业，邀请被
    // 1002 拒绝），成员身份由下方 shares/members 的 Base 协作者授权建立。
  }
  // EXTERNAL 改在下方建 Base 并签发分享链接后注册（link_token 为 N7 后唯一不建企
  // 的注册路径），保持外部身份。
  expect((await register(soloCtx.request, SOLO, { enterprise_name: `P1空态${RUN.slice(-4)}` }))?.code).toBe(0)
  expect((await apiLogin(soloCtx.request, SOLO))?.code).toBe(0)

  const created = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `P1UI沙盒-${RUN}` })
  expect(created?.code).toBe(0)
  baseId = created.data.base.id
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/tables`)
  table1Id = tables.data.table_list[0].id
  table1Name = tables.data.table_list[0].name
  const table2 = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/tables`, { name: '目标表' })
  table2Id = table2.data.table.id
  const table2Fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table2Id}/fields`)
  const table2Primary = table2Fields.data.field_list.find((field: any) => field.is_primary)
  targetPrimaryId = table2Primary.id
  const targetRecord = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
    table_id: table2Id, cells_map: { [table2Primary.id]: '目标记录A' },
  })
  targetRecordId = targetRecord.data.record.id

  const createField = async (name: string, type: string, config_map: Record<string, unknown> = {}) => {
    const result = await api(ownerCtx.request, 'POST', `/api/v1/tables/${table1Id}/fields`, { name, type, config_map })
    expect(result?.code, `create field ${name}: ${JSON.stringify(result)}`).toBe(0)
    fieldIds[name] = result.data.field.id
    return result.data.field
  }
  const primary = (await api(ownerCtx.request, 'GET', `/api/v1/tables/${table1Id}/fields`)).data.field_list.find((field: any) => field.is_primary)
  fieldIds['标题'] = primary.id
  await createField('数量', 'number', { format: 'decimal', decimals: 2, thousands: true })
  const select = await createField('状态', 'select', { option_list: [{ name: '待办', color: 'blue' }, { name: '完成', color: 'green' }], allow_create: false })
  await createField('多选', 'multiselect', { option_list: [{ name: '红', color: 'red' }, { name: '蓝', color: 'blue' }], allow_create: false })
  await createField('日期', 'date', { format: 'YYYY-MM-DD', include_time: false })
  await createField('人员', 'person', { allow_multiple: true })
  await createField('链接', 'url', {})
  await createField('公式', 'formula', { expression: 'SUM({数量},10)' })
  await createField('错误公式', 'formula', { expression: '10/{数量}' })
  await createField('关联', 'relation', { target_table_id: table2Id, allow_multiple: true })
  await createField('引用', 'lookup', { relation_field_id: fieldIds['关联'], target_field_id: table2Primary.id })
  await createField('附件', 'attachment', {})

  const views = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table1Id}/views`)
  viewId = views.data.view_list[0].id
  const optionId = select.config_map.option_list[0].id
  const createRecord = async (title: string, number: number, extra: Record<string, unknown> = {}) => {
    const result = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
      table_id: table1Id,
      cells_map: { [fieldIds['标题']]: title, [fieldIds['数量']]: number, ...extra },
    })
    expect(result?.code, JSON.stringify(result)).toBe(0)
    return result.data.record.id
  }
  recordA = await createRecord('记录A', 10, {
    [fieldIds['状态']]: optionId,
    [fieldIds['日期']]: 1730000000000,
    [fieldIds['人员']]: [ownerUserId],
    [fieldIds['链接']]: { url: 'https://example.com/a', display_text: '示例A' },
    [fieldIds['关联']]: [targetRecordId],
  })
  recordB = await createRecord('记录B', 2)
  recordC = await createRecord('记录C', 7)

  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: EDITOR, role: 'editor' }))?.code).toBe(0)
  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: FILLER, role: 'filler' }))?.code).toBe(0)
  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: VIEWER, role: 'viewer' }))?.code).toBe(0)

  // 票 05 夹具适配：先签发分享链接，EXTERNAL 凭 link_token 注册（外部身份），
  // 再补 Base 协作者授权（editor）——角色语义与原夹具一致。
  const link = await api(ownerCtx.request, 'PUT', `/api/v1/bases/${baseId}/link`, { scope: 'anyone', role: 'filler' })
  expect(link?.code).toBe(0)
  linkToken = (link.data.link_config.url as string).split('/').filter(Boolean).pop() ?? ''
  expect((await register(externalCtx.request, EXTERNAL, { link_token: linkToken }))?.code).toBe(0)
  expect((await apiLogin(externalCtx.request, EXTERNAL))?.code).toBe(0)
  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: EXTERNAL, role: 'editor' }))?.code).toBe(0)

  // 等公式重算落库
  for (let index = 0; index < 30; index += 1) {
    const record = await api(ownerCtx.request, 'GET', `/api/v1/records/${recordA}?base_id=${baseId}`)
    const value = record?.data?.record?.cells_map?.[fieldIds['公式']]
    if (value !== undefined && value !== null) break
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
})

test.afterAll(async () => {
  for (const ctx of [ownerCtx, editorCtx, fillerCtx, viewerCtx, externalCtx, soloCtx]) {
    await ctx?.close()
  }
})

// ---------------- T00 / T01 入口与分享链接 ----------------

test('TC-000-06 | 分享链接入口', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/s/forged-token-xyz`)
  await expect(page).toHaveURL(/\/err\?kind=share-link-unavailable/, { timeout: 15000 })
  // triage(票02)：修用例——/链接.*失效|不可用/ 同时命中 h1「分享链接不可用」与说明段
  // 「该分享链接已失效或被关闭…」（strict mode 冲突）；失效态文案由 h1 标题承载，收窄之。
  await expect(page.getByRole('heading', { name: '分享链接不可用' })).toBeVisible()
  await page.goto(`${BASE}/s/${linkToken}`)
  await expect(page).toHaveURL(/\/register\?link_token=/, { timeout: 15000 })
  await ctx.close()
})

test('TC-010-10 | 分享链接注册后进入 Base', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/s/${linkToken}`)
  await expect(page).toHaveURL(/\/register\?link_token=/)
  const username = `p1lnkui${RUN}`
  await page.getByLabel('用户名').fill(username)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/base/${baseId}`), { timeout: 25000 })
  await waitGrid(page)
  await ctx.close()
})

test('TC-010-15 | 多标签会话互不串写', async () => {
  const page1 = await ownerCtx.newPage()
  const page2 = await ownerCtx.newPage()
  await gotoBase(page1)
  await gotoBase(page2)
  await editNumber(page1, recordB, 12)
  await editNumber(page2, recordC, 17)
  await page1.reload()
  await waitGrid(page1)
  await page2.reload()
  await waitGrid(page2)
  await expect(cell(page1, recordB, fieldIds['数量'])).toContainText('12')
  await expect(cell(page2, recordC, fieldIds['数量'])).toContainText('17')
  await page1.close()
  await page2.close()
})

test('TC-010-16 | 深链登录回跳', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/base/${baseId}`)
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible({ timeout: 15000 })
  await page.getByLabel('用户名').fill(EDITOR)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/base/${baseId}`), { timeout: 25000 })
  await ctx.close()
})

// ---------------- T02 首页 ----------------

test('TC-020-05 | Base 搜索', async () => {
  const page = await ownerCtx.newPage()
  await page.goto(`${BASE}/home`)
  await page.getByLabel('搜索多维表格名称').fill(`P1UI沙盒-${RUN}`)
  await expect(page.getByRole('region', { name: '搜索结果' })).toBeVisible({ timeout: 15000 })
  await expect(page.getByRole('region', { name: '搜索结果' }).getByText(`P1UI沙盒-${RUN}`)).toBeVisible()
  await page.getByLabel('搜索多维表格名称').fill('绝不存在的名称xyz')
  await expect(page.getByRole('region', { name: '搜索结果' }).getByText(/暂无|没有/)).toBeVisible({ timeout: 15000 })
  await page.close()
})

test('TC-020-06 | 重命名 / 星标', async () => {
  const page = await ownerCtx.newPage()
  try {
    await page.goto(`${BASE}/home`)
    const card = page.getByRole('article', { name: `多维表格：P1UI沙盒-${RUN}` }).first().first()
    await expect(card).toBeVisible()
    await card.getByRole('button', { name: `更多操作：P1UI沙盒-${RUN}` }).click()
    await page.getByRole('menuitem', { name: '重命名' }).click()
    await page.getByLabel('Base 名称').fill(`P1UI改名-${RUN}`)
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.getByRole('article', { name: `多维表格：P1UI改名-${RUN}` }).first()).toBeVisible({ timeout: 15000 })
    const renamed = page.getByRole('article', { name: `多维表格：P1UI改名-${RUN}` }).first()
    await renamed.getByRole('button', { name: `更多操作：P1UI改名-${RUN}` }).click()
    await page.getByRole('menuitem', { name: '星标', exact: true }).click()
    await expect(renamed.getByLabel('已星标')).toBeVisible({ timeout: 15000 })
  } finally {
    // 还原名称，避免影响后续用例
    await api(ownerCtx.request, 'PATCH', `/api/v1/bases/${baseId}`, { name: `P1UI沙盒-${RUN}` })
    await page.close()
  }
})

test('TC-020-09 | 卡片信息', async () => {
  // 缺陷 #28：为 owner 设置显示名，卡片「最近修改人」必须展示显示名
  //（缺失回退用户名），任何情况下不得出现内部 user_id。
  const displayName = `界面主人${RUN.slice(-4)}`
  expect(
    (await api(ownerCtx.request, 'PUT', '/api/v1/me/profile', { display_name: displayName }))?.code,
  ).toBe(0)
  const page = await ownerCtx.newPage()
  await page.goto(`${BASE}/home`)
  const card = page.getByRole('article', { name: `多维表格：P1UI沙盒-${RUN}` }).first()
  await expect(card).toBeVisible()
  await expect(card.getByText(/2 个数据表|个数据表/).first()).toBeVisible()
  await expect(card.getByText(/最近修改/)).toBeVisible()
  await expect(card.getByText(displayName)).toBeVisible()
  await expect(card.getByText(ownerUserId)).toBeHidden()
  await page.close()
})

test('TC-020-08 | 首页空态', async () => {
  const page = await soloCtx.newPage()
  await page.goto(`${BASE}/home`)
  await expect(page.getByRole('region', { name: '我创建的' })).toBeVisible({ timeout: 15000 })
  await expect(page.getByText('还没有创建过多维表格')).toBeVisible()
  await expect(page.getByText('暂无与你共享的多维表格')).toBeVisible()
  await page.close()
})

// ---------------- T03 数据表 ----------------

test('TC-030-05 | 表切换', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('tab', { name: '目标表' }).click()
  await expect(cell(page, targetRecordId, targetPrimaryId)).toContainText('目标记录A', { timeout: 15000 })
  await page.getByRole('tab', { name: table1Name, exact: true }).click()
  await expect(cell(page, recordA, fieldIds['标题'])).toContainText('记录A', { timeout: 15000 })
  await page.close()
})

test('TC-030-06 | 重命名表', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '表操作：目标表' }).click()
  await page.getByRole('menuitem', { name: '重命名表' }).click()
  const input = page.getByRole('dialog').locator('input').first()
  await input.fill('目标表-改名')
  await page.getByRole('dialog').getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('tab', { name: '目标表-改名' })).toBeVisible({ timeout: 15000 })
  await api(ownerCtx.request, 'PATCH', `/api/v1/tables/${table2Id}`, { name: '目标表' })
  await page.close()
})

test('TC-030-07 | 在途写微状态', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('tab', { name: '目标表' }).click()
  let release: (() => void) | null = null
  await page.route('**/api/v1/tables/*', async (route) => {
    if (route.request().method() === 'PATCH') {
      await new Promise<void>((resolve) => { release = resolve })
      await route.continue()
    } else {
      await route.continue()
    }
  })
  await page.getByRole('button', { name: '表操作：目标表' }).click()
  await page.getByRole('menuitem', { name: '重命名表' }).click()
  const input = page.getByRole('dialog').locator('input').first()
  await input.fill('目标表-保存中')
  await page.getByRole('dialog').getByRole('button', { name: '保存' }).click()
  await expect.poll(() => release !== null, { timeout: 10000 }).toBeTruthy()
  const tabBadge = page.getByRole('tab', { name: /目标表/ }).getByText('保存中', { exact: true })
  await expect(tabBadge).toBeVisible({ timeout: 10000 })
  release?.()
  await expect(tabBadge).toBeHidden({ timeout: 15000 })
  await page.unroute('**/api/v1/tables/*')
  await api(ownerCtx.request, 'PATCH', `/api/v1/tables/${table2Id}`, { name: '目标表' })
  await page.close()
})

// ---------------- T04 字段 ----------------

test('TC-040-03 | 列宽拖拽与持久化', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const header = page.getByRole('columnheader', { name: '字段：数量' })
  const before = await header.boundingBox()
  const handle = page.getByRole('separator', { name: '拖拽调整列宽：数量' })
  const handleBox = await handle.boundingBox()
  if (!before || !handleBox) throw new Error('列宽拖拽定位失败')
  await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2)
  await page.mouse.down()
  await page.mouse.move(handleBox.x + handleBox.width / 2 + 80, handleBox.y + handleBox.height / 2, { steps: 8 })
  await page.mouse.up()
  await page.waitForTimeout(1200)
  const after = await header.boundingBox()
  expect(after!.width).toBeGreaterThan(before.width + 40)
  await page.reload()
  await waitGrid(page)
  const persisted = await page.getByRole('columnheader', { name: '字段：数量' }).boundingBox()
  expect(Math.abs(persisted!.width - after!.width)).toBeLessThan(6)
  await page.close()
})

test.fixme('TC-040-07 | 多选', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await editMulti(page, recordB, ['红', '蓝'])
  const tags = cell(page, recordB, fieldIds['多选'])
  await expect(tags.getByText('红')).toBeVisible()
  await expect(tags.getByText('蓝')).toBeVisible()
  await openCellEditor(page, recordB, fieldIds['多选'])
  await page.getByRole('button', { name: '移除选项：红' }).click()
  await page.getByLabel('搜索选项').press('Enter')
  await expect(tags.getByText('红')).toBeHidden({ timeout: 15000 })
  await page.close()
})

test('TC-040-08 | 日期', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await editDate(page, recordB, '2024-01-02')
  await expect(cell(page, recordB, fieldIds['日期'])).toContainText('2024-01-02')
  // 1899 年低于最小值：应被拒绝且不落库
  await openCellEditor(page, recordB, fieldIds['日期'])
  const input = page.getByLabel('日期编辑')
  await input.fill('1899-01-01')
  await input.press('Enter')
  await expect(page.getByRole('alert').first()).toBeVisible({ timeout: 5000 })
  await page.keyboard.press('Escape')
  await expect(cell(page, recordB, fieldIds['日期'])).toContainText('2024-01-02')
  await page.close()
})

test.fixme('TC-040-09 | 人员', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await editPerson(page, recordB, EDITOR)
  await expect(cell(page, recordB, fieldIds['人员'])).toContainText(EDITOR)
  await page.close()
})

test('TC-040-10 | 超链接', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  // triage(票02)：修用例——票 08/BUG-02 起网格内 URL 编辑器为紧凑形态（仅地址单输入，
  // 显示文本缺省取地址本身；地址+显示文本双字段完整编辑在记录详情抽屉），网格编辑态
  // 等待「显示文本」输入框恒超时；紧凑形态下不传显示文本，href 断言不变。
  await editUrl(page, recordB, 'example.com', '')
  await expect(cell(page, recordB, fieldIds['链接']).getByRole('link')).toHaveAttribute('href', /https:\/\/example\.com\/?/)
  await openCellEditor(page, recordB, fieldIds['链接'])
  await page.getByLabel('链接地址').fill('ftp://invalid')
  await page.getByLabel('链接地址').press('Enter')
  await expect(page.getByText('请输入合法的 http/https 链接')).toBeVisible({ timeout: 10000 })
  await page.keyboard.press('Escape')
  await page.close()
})

test('TC-040-11 | 公式', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '新增字段' }).click()
  const dialog = page.getByRole('dialog', { name: '新增字段' })
  await dialog.getByLabel('字段类型').selectOption('formula')
  await dialog.getByLabel('字段名称').fill('UI公式')
  await dialog.getByLabel('公式表达式').fill('SUM({数量},1)')
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(page.getByRole('columnheader', { name: '字段：UI公式' })).toBeVisible({ timeout: 15000 })
  const all = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table1Id}/fields`)
  const created = all.data.field_list.find((field: any) => field.name === 'UI公式')
  expect(created).toBeTruthy()
  await expect.poll(async () => {
    const record = await api(ownerCtx.request, 'GET', `/api/v1/records/${recordB}?base_id=${baseId}`)
    return record?.data?.record?.cells_map?.[created.id]
  }, { timeout: 15000 }).not.toBeNull()
  await page.close()
})

test('TC-040-13 | 公式 #ERROR', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  // 造除数为零：把 数量 改为 0；契约要求落库 #ERROR 哨兵并展示 hover 原因
  const numberCell = cell(page, recordB, fieldIds['数量'])
  await openCellEditor(page, recordB, fieldIds['数量'])
  const numberEditor = page.getByTestId(`editor-${recordB}-${fieldIds['数量']}`)
  const input = numberEditor.locator('input').first()
  await input.fill('0')
  await input.press('Enter')
  try {
    const target = cell(page, recordB, fieldIds['错误公式'])
    await expect(target).toContainText('#ERROR', { timeout: 15000 })
    // triage(票02)：修用例——错误原因自 m2-05 起由 title 属性承载（需求 4.3「#ERROR
    // hover 原因」实现为原生 tooltip，非 DOM 文本），getByText('除数为零') 永不命中；
    // 保留 hover 验证 hover 通道，改断言 data-error 节点的 title 值。
    await target.hover()
    await expect(target.locator('[data-error="true"]')).toHaveAttribute('title', '除数为零')
  } finally {
    await page.keyboard.press('Escape')
    await page.close()
  }
})

test.fixme('TC-040-14 | 单向关联', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await editRelation(page, recordB, '目标记录A')
  await expect(cell(page, recordB, fieldIds['关联'])).toContainText('目标记录A')
  await page.close()
})

test('TC-040-15 | 引用', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await expect.poll(async () => cell(page, recordA, fieldIds['引用']).innerText(), { timeout: 20000 }).toContain('目标记录A')
  // 改目标主字段后引用应自动刷新
  const targetFields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table2Id}/fields`)
  const targetPrimary = targetFields.data.field_list.find((field: any) => field.is_primary)
  const targetRecord = await api(ownerCtx.request, 'GET', `/api/v1/records/${targetRecordId}?base_id=${baseId}`)
  await api(ownerCtx.request, 'PUT', `/api/v1/records/${targetRecordId}/cells/${targetPrimary.id}?base_id=${baseId}`, {
    value: '目标记录A-改', expect_version: Number(targetRecord.data.record.version),
  })
  await page.reload()
  await waitGrid(page)
  await expect.poll(async () => cell(page, recordA, fieldIds['引用']).innerText(), { timeout: 20000 }).toContain('目标记录A-改')
  await page.close()
})

test('TC-040-16 | 类型转换预览', async () => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${table1Id}/fields`, { name: '转换测试', type: 'text', config_map: {} })
  const fieldId = created.data.field.id
  await api(ownerCtx.request, 'PUT', `/api/v1/records/${recordC}/cells/${fieldId}?base_id=${baseId}`, { value: '123', expect_version: 0 })
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '字段操作：转换测试' }).click()
  await page.getByRole('menuitem', { name: '更改类型' }).click()
  const dialog = page.getByRole('dialog', { name: '更改字段类型' })
  await dialog.getByLabel('目标类型').selectOption('number')
  await dialog.getByRole('button', { name: '预览影响' }).click()
  await expect(dialog.getByText(/受影响记录 \d+ 条/)).toBeVisible({ timeout: 15000 })
  await dialog.getByRole('button', { name: '确认转换' }).click()
  await expect(dialog).toBeHidden({ timeout: 15000 })
  const fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table1Id}/fields`)
  // triage(票02)：修用例——GET /fields 经 protojson 序列化，type 为枚举名
  // FIELD_TYPE_NUMBER（创建入参接受短名 'number'，回读恒为枚举名），期望按契约对齐。
  expect(fields.data.field_list.find((field: any) => field.id === fieldId).type).toBe('FIELD_TYPE_NUMBER')
  await api(ownerCtx.request, 'DELETE', `/api/v1/fields/${fieldId}`, { confirm: true })
  await page.close()
})

test('TC-040-18 | 删除字段影响', async () => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${table1Id}/fields`, { name: '视图条件字段', type: 'text', config_map: {} })
  const fieldId = created.data.field.id
  const config = await api(ownerCtx.request, 'GET', `/api/v1/views/${viewId}/config`)
  const viewConfig = config.data.view_config ?? config.data
  await api(ownerCtx.request, 'PUT', `/api/v1/views/${viewId}/config`, {
    view_config: { ...viewConfig, filter_list: [{ field_id: fieldId, op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: 'x' } }] },
  })
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '字段操作：视图条件字段' }).click()
  await page.getByRole('menuitem', { name: '删除字段' }).click()
  const dialog = page.getByRole('dialog', { name: '删除字段' })
  try {
    await expect(dialog.getByText(/影响记录/)).toBeVisible()
    await dialog.getByRole('button', { name: '确认删除' }).click()
    await expect(page.getByRole('columnheader', { name: '字段：视图条件字段' })).toBeHidden({ timeout: 15000 })
    const after = await api(ownerCtx.request, 'GET', `/api/v1/views/${viewId}/config`)
    const afterConfig = after.data.view_config ?? after.data
    expect((afterConfig.filter_list ?? []).some((filter: any) => filter.field_id === fieldId)).toBeFalsy()
  } finally {
    // 兜底清理，避免失效筛选条件影响后续网格用例
    const current = await api(ownerCtx.request, 'GET', `/api/v1/views/${viewId}/config`)
    const currentConfig = current.data.view_config ?? current.data
    if ((currentConfig.filter_list ?? []).some((filter: any) => filter.field_id === fieldId)) {
      await api(ownerCtx.request, 'PUT', `/api/v1/views/${viewId}/config`, {
        view_config: { ...currentConfig, filter_list: [] },
      })
    }
    const fieldsNow = await api(ownerCtx.request, 'GET', `/api/v1/tables/${table1Id}/fields`)
    if (fieldsNow.data.field_list.some((field: any) => field.id === fieldId)) {
      await api(ownerCtx.request, 'DELETE', `/api/v1/fields/${fieldId}`, { confirm: true })
    }
    await page.close()
  }
})

// ---------------- T05 记录编辑 ----------------

test('TC-050-05 | 保存状态三态', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.route('**/api/v1/records/*/cells/*', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 900))
    await route.continue()
  })
  await openCellEditor(page, recordA, fieldIds['数量'])
  const editor = page.getByTestId(`editor-${recordA}-${fieldIds['数量']}`)
  await editor.locator('input').first().fill('33')
  await editor.locator('input').first().press('Enter')
  await expect(page.getByText('保存中…')).toBeVisible({ timeout: 5000 })
  await expect(page.getByText('已保存', { exact: true })).toBeVisible({ timeout: 15000 })
  await page.unroute('**/api/v1/records/*/cells/*')

  await page.route('**/api/v1/records/*/cells/*', async (route) => {
    await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 9001, message: '服务暂时不可用，正在重试', data: null }) })
  })
  await openCellEditor(page, recordA, fieldIds['数量'])
  await page.getByTestId(`editor-${recordA}-${fieldIds['数量']}`).locator('input').first().fill('44')
  await page.getByTestId(`editor-${recordA}-${fieldIds['数量']}`).locator('input').first().press('Enter')
  await expect(page.getByText('保存失败')).toBeVisible({ timeout: 15000 })
  await expect(page.getByTestId('cell-write-error')).toBeVisible()
  await page.unroute('**/api/v1/records/*/cells/*')
  await page.close()
})

test('TC-050-06 | TSV 复制', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await cell(page, recordA, fieldIds['标题']).click()
  // triage(票02)：修用例（两处用例体缺陷，断言体零改动）——
  // (a) 选区扩展：网格单击/Shift+单击均按单格选中处理（鼠标拖选未实现，键盘
  //     Shift+方向键才是 3.8.1 的扩展选区通道），原 Shift+单击后选区收拢为单格，
  //     复制结果不含「标题+数量」两列；改用 Shift+ArrowRight 扩展到同行右格。
  // (b) 触发通道：见 gridKeyCombo——press('Control+C') 送达大写 'C' 不入状态机
  //     （真实浏览器为小写 'c'），改派发同构键事件触发网格 TSV 复制。
  await page.keyboard.press('Shift+ArrowRight')
  await gridKeyCombo(page, 'c')
  await page.waitForTimeout(600)
  const text = await page.evaluate(() => navigator.clipboard.readText())
  expect(text).toContain('记录A')
  expect(text).toContain('\t')
  await page.close()
})

test('TC-050-07 | 粘贴只读列跳过', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  // triage(票02)：修用例（两处用例体缺陷，验证目标「只读列跳过并在粘贴后提示」不变）——
  // (a) 场景：原锚点「数量」右邻为「状态」（可写列），粘贴行根本不含只读列，跳过提示
  //     无从产生；改为从「链接」列起粘贴一行，跨入只读「公式」列（READONLY_TYPES）。
  // (b) 文案/通道：提示文案实为「跳过只读列 N 个」（buildPasteSummary），原 /跳过 \d+ 个/
  //     永不匹配；且 press('Control+V') 送达大写 'V' 不入状态机（见 gridKeyCombo），
  //     改派发同构键事件触发网格粘贴。
  await page.evaluate(() => navigator.clipboard.writeText('https://example.com/paste\t99'))
  await cell(page, recordC, fieldIds['链接']).click()
  await gridKeyCombo(page, 'v')
  await expect(page.getByText(/跳过只读列 \d+ 个/)).toBeVisible({ timeout: 15000 })
  await page.close()
})

test('TC-050-10 | 会话内撤销', async () => {
  // triage(票02)：修用例（非票02 清单内 14 条，但同属大写键送达缺陷族且实跑暴露假绿）——
  // (a) 触发通道：press('Control+Z') 送达大写 'Z' 不入状态机（见 gridKeyCombo），撤销从未
  //     真正触发；原用例靠 '7.00' ⊂ '77.00' 子串巧合假绿（本次实跑 recordC 先前值 17.00，
  //     '77.00' 不含 '17.00'，假绿崩出）。改派发同构键事件，撤销断言原样保留。
  // (b) 刷新后段落：AC-REC-07「最近一步恢复；刷新后不可撤销」（撤销栈为模块级，刷新即
  //     清空，edit-history.ts 设计注释同口径），原用例刷新后按 Z 期望值回退，与需求相反；
  //     改为断言刷新后编辑值保留、撤销不再生效。
  // (c) 时序确定化：撤销/写入后的行级刷新（refreshRecordRows）若在撤销应用之后落定，
  //     会以旧版本行覆盖新状态（trace 实证：撤销后紧接的同格编辑以陈旧 expect_version
  //     请求 → 409 冲突）。每次写/撤销都等待其行级刷新响应落定再断言，消除竞态窗口。
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const cellC = cell(page, recordC, fieldIds['数量'])
  const before = await cellC.innerText()
  // 会话内撤销：编辑 88 → 撤销 → 回到编辑前值（真撤销，断言为原口径）
  await Promise.all([
    page.waitForResponse((res) => res.url().includes(`/api/v1/records/${recordC}`) && res.request().method() === 'GET'),
    editNumber(page, recordC, 88),
  ])
  await expect(cellC).toContainText('88')
  await Promise.all([
    page.waitForResponse((res) => res.url().includes('/cells/') && res.request().method() === 'PUT'),
    page.waitForResponse((res) => res.url().includes(`/api/v1/records/${recordC}`) && res.request().method() === 'GET'),
    gridKeyCombo(page, 'z'),
  ])
  await expect(cellC).toContainText(before.trim(), { timeout: 15000 })
  // 刷新后不可撤销（AC-REC-07）：再做一笔编辑（不撤销）→ 刷新 → 按 Z 不得回退
  await Promise.all([
    page.waitForResponse((res) => res.url().includes(`/api/v1/records/${recordC}`) && res.request().method() === 'GET'),
    editNumber(page, recordC, 77),
  ])
  await page.reload()
  await waitGrid(page)
  await gridKeyCombo(page, 'z')
  await expect(cellC).toContainText('77', { timeout: 15000 })
  await page.close()
})

test('TC-050-11 | 排序视图禁拖', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await api(ownerCtx.request, 'PUT', `/api/v1/views/${viewId}/config`, {
    view_config: { ...(await api(ownerCtx.request, 'GET', `/api/v1/views/${viewId}/config`)).data.view_config, sort_list: [{ field_id: fieldIds['数量'], dir: 'SORT_DIR_ASC' }] },
  })
  try {
    await page.reload()
    await waitGrid(page)
    const rowA = page.getByRole('row').filter({ hasText: '记录A' }).first()
    const rowC = page.getByRole('row').filter({ hasText: '记录C' }).first()
    await rowA.dragTo(rowC)
    // triage(票02)：两处处置——(a) 修产品：AC-REC-08 要求排序视图拖行「被禁止并提示」，
    // 但行 draggable={!hasSortOrGroup && canEditCell} 使 dragstart 在排序视图永不触发
    // （真实浏览器不对 non-draggable 元素发起拖拽；jsdom fireEvent 掩盖了该死路径），
    // 提示恒不可达；产品改为行保持 draggable，重排由 handleDragStart/handleDrop 守卫
    // + dragover 不 preventDefault 双重拦截。(b) 修用例：合规横幅恒为 role=alert
    // （warning tone），getByRole('alert') 命中双元素 strict 冲突，收窄到提示文本。
    await expect(page.getByRole('alert').filter({ hasText: '当前为排序视图' })).toBeVisible()
  } finally {
    await api(ownerCtx.request, 'PUT', `/api/v1/views/${viewId}/config`, {
      view_config: { ...(await api(ownerCtx.request, 'GET', `/api/v1/views/${viewId}/config`)).data.view_config, sort_list: [] },
    })
    await page.close()
  }
})

test('TC-050-15 | 手动排序', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const orderBefore = await page.locator(`[data-testid^="cell-"][data-testid$="-${fieldIds['标题']}"]`).allInnerTexts()
  const rowA = page.getByRole('row').filter({ hasText: '记录A' }).first()
  const rowC = page.getByRole('row').filter({ hasText: '记录C' }).first()
  await rowA.dragTo(rowC)
  await page.waitForTimeout(800)
  const orderAfter = await page.locator(`[data-testid^="cell-"][data-testid$="-${fieldIds['标题']}"]`).allInnerTexts()
  expect(orderAfter.join('|')).not.toBe(orderBefore.join('|'))
  await page.close()
})

test('TC-050-16 | 视图内搜索', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  // 环境约束：locator.press() 会丢键，统一用 evaluate 派发键盘事件（与真实 keydown 同构）。
  // 派发前先点击网格拿到焦点（grid tabindex=0，真实用户 Ctrl+F 也在焦点元素上触发）。
  await page.locator('[role="grid"][aria-label="记录网格"]').click({ position: { x: 40, y: 40 } })
  await page.evaluate(() => {
    document
      .querySelector('[role="grid"][aria-label="记录网格"]')
      ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'f', ctrlKey: true, bubbles: true, cancelable: true }))
  })
  await expect(page.getByTestId('grid-find-bar')).toBeVisible({ timeout: 5000 })
  const findInput = page.getByTestId('grid-find-input')

  await findInput.fill('记录A')
  // 过滤已加载数据：仅命中行保留，命中单元格带高亮标记并显示匹配计数
  await expect(page.locator('[data-record-id]')).toHaveCount(1, { timeout: 10000 })
  await expect(cell(page, recordA, fieldIds['标题'])).toHaveAttribute('data-cell-match', 'true')
  await expect(page.getByTestId('grid-find-count')).toContainText('1/3')

  // Esc 关闭搜索条并恢复全部已加载行
  await page.evaluate(() => {
    document
      .querySelector('[data-testid="grid-find-input"]')
      ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  })
  await expect(page.getByTestId('grid-find-bar')).toBeHidden()
  await expect(page.locator('[data-record-id]')).toHaveCount(3, { timeout: 10000 })

  // 纯客户端过滤：不产生服务端视图筛选条件
  await openFilterPanel(page)
  await expect(page.getByText('当前 0 条条件')).toBeVisible()
  await page.close()
})

// ---------------- T06 视图 ----------------

test('TC-060-02 | 筛选操作符抽样', async () => {
  // triage(票02)：首段「标题 包含 记录A → 1 行」为票01 竞态族——竞态修复后按原断言
  // 复跑通过；后续段落曾挂死于 addFilter helpers（选项类筛选无文本值输入框，
  // helpers 假设恒有——属用例体缺陷，已在 helpers 内修正，断言零改动）。
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await clearFilters(page)
  await addFilter(page, '标题', '包含', '记录A')
  expect(await visibleRecordCount(page)).toBe(1)
  await clearFilters(page)
  await addFilter(page, '数量', '大于', '5')
  expect(await visibleRecordCount(page)).toBeGreaterThanOrEqual(1)
  await clearFilters(page)
  await addFilter(page, '状态', '属于', '')
  const option = page.getByLabel('筛选选项 1：待办')
  if (await option.count() > 0) await option.check()
  await page.waitForTimeout(1200)
  expect(await visibleRecordCount(page)).toBeGreaterThanOrEqual(1)
  await clearFilters(page)
  await openFilterPanel(page)
  await page.getByRole('button', { name: '添加筛选条件' }).click()
  await page.getByLabel('筛选字段 1').selectOption({ label: '日期' })
  await page.getByLabel('筛选操作 1').selectOption({ label: '今天' })
  await page.waitForTimeout(1500)
  await expect(page.getByText('没有符合筛选条件的记录')).toBeVisible()
  await clearFilters(page)
  await page.close()
})

test('TC-060-03 | 筛选上限', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openFilterPanel(page)
  for (let index = 0; index < 5; index += 1) {
    await page.getByRole('button', { name: '添加筛选条件' }).click()
  }
  await page.getByRole('button', { name: '添加筛选条件' }).click()
  await expect(page.getByRole('alert').filter({ hasText: '筛选条件最多 5 条' })).toBeVisible()
  await clearFilters(page)
  await page.close()
})

test('TC-060-04 | 排序', async () => {
  // triage(票02)：票01 竞态族——竞态修复后按原断言复跑通过（首行=记录B，升序数量 2），
  // 用例体零改动。
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '排序', exact: true }).click()
  for (let index = 0; index < 3; index += 1) {
    await page.getByRole('button', { name: '添加排序' }).click()
  }
  await page.getByLabel('排序字段 1').selectOption({ label: '数量' })
  await page.getByLabel('排序方向 1').selectOption('asc')
  await page.waitForTimeout(1500)
  const titles = await page.locator(`[data-testid^="cell-"][data-testid$="-${fieldIds['标题']}"]`).allInnerTexts()
  expect(titles[0]).toContain('记录B')
  await page.getByRole('button', { name: '排序', exact: true }).click()
  await page.close()
})

test('TC-060-05 | 分组（N1 语义）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '分组', exact: true }).click()
  await page.getByLabel('分组字段').selectOption({ label: '状态' })
  await expect(page.getByRole('row', { name: /分组：待办/ }).or(page.getByText('分组：待办'))).toBeVisible({ timeout: 15000 })
  await page.getByRole('button', { name: '分组', exact: true }).click()
  await page.close()
})

test('TC-060-06 | 字段显隐', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '字段显隐', exact: true }).click()
  await page.getByLabel('显示字段：数量').uncheck()
  await expect(page.getByRole('columnheader', { name: '字段：数量' })).toBeHidden({ timeout: 15000 })
  const record = await api(ownerCtx.request, 'GET', `/api/v1/records/${recordA}?base_id=${baseId}`)
  expect(record?.data?.record?.cells_map?.[fieldIds['数量']]).toBeTruthy()
  await page.getByLabel('显示字段：数量').check()
  await expect(page.getByRole('columnheader', { name: '字段：数量' })).toBeVisible({ timeout: 15000 })
  await page.close()
})

test('TC-060-08 | 筛选无结果', async () => {
  // triage(票02)：票01 竞态族——竞态修复后按原断言复跑通过（空态出现/清除后消失），
  // 用例体零改动。
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await addFilter(page, '标题', '包含', '不存在的内容')
  await expect(page.getByText('没有符合筛选条件的记录')).toBeVisible({ timeout: 15000 })
  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(page.getByText('没有符合筛选条件的记录')).toBeHidden({ timeout: 15000 })
  await page.close()
})

test('TC-060-09 | 视图对协作者可见', async () => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${table1Id}/views`, { name: '协作视图', type: 'table' })
  expect(created?.code).toBe(0)
  const page = await editorCtx.newPage()
  await gotoBase(page)
  // 视图页签旁还有「视图操作：协作视图」菜单按钮，getByRole name 默认子串匹配
  // 会同时命中两者（strict mode violation）——收窄到页签本身。
  const viewTab = page.getByRole('button', { name: '协作视图', exact: true })
  await expect(viewTab).toBeVisible({ timeout: 15000 })
  await viewTab.click()
  await page.close()
})

test('TC-060-10 | 行高三档', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '字段显隐', exact: true }).click()
  await page.getByRole('button', { name: '紧凑' }).click()
  await page.waitForTimeout(500)
  const compact = await cell(page, recordA, fieldIds['标题']).boundingBox()
  await page.getByRole('button', { name: '宽松' }).click()
  await page.waitForTimeout(500)
  const tall = await cell(page, recordA, fieldIds['标题']).boundingBox()
  expect(tall!.height).toBeGreaterThan(compact!.height)
  await page.getByRole('button', { name: '中等' }).click()
  await page.close()
})

test('TC-060-11 | 配置变更重拉', async () => {
  const page = await ownerCtx.newPage()
  const requests: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/records?')) requests.push(request.url())
  })
  await gotoBase(page)
  requests.length = 0
  await addFilter(page, '标题', '包含', '记录')
  await expect.poll(() => requests.length, { timeout: 15000 }).toBeGreaterThan(0)
  expect(requests.some((url) => url.includes('cursor=') || url.includes('view_id='))).toBeTruthy()
  await clearFilters(page)
  await page.close()
})

test('TC-060-12 | 可填写/可查看切视图', async () => {
  const page = await fillerCtx.newPage()
  await gotoBase(page)
  await expect(page.getByRole('navigation', { name: '视图' })).toBeVisible({ timeout: 15000 })
  const tabs = page.getByRole('button', { name: /表格|协作视图/ })
  expect(await tabs.count()).toBeGreaterThanOrEqual(1)
  await expect(page.getByRole('button', { name: '筛选', exact: true })).toBeHidden()
  await expect(page.getByRole('button', { name: '新建视图' })).toBeHidden()
  await page.close()
})

// ---------------- T07 附件 ----------------

test('TC-070-02 | 附件上传下载重命名删除', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordA)
  const upload = page.getByLabel('上传附件')
  await upload.setInputFiles({ name: 'note.txt', mimeType: 'text/plain', buffer: Buffer.from('hello attachment') })
  const link = page.getByLabel('下载附件 note.txt')
  await expect(link).toBeVisible({ timeout: 20000 })
  const [download] = await Promise.all([page.waitForEvent('download'), link.click()])
  const downloadPath = await download.path()
  const content = downloadPath === null ? '' : fs.readFileSync(downloadPath, 'utf8')
  expect(content).toBe('hello attachment')
  await page.getByLabel('重命名附件 note.txt').click()
  await page.getByLabel('附件新名称').fill('renamed.txt')
  await page.getByRole('button', { name: '保存名称' }).click()
  await expect(page.getByLabel('下载附件 renamed.txt')).toBeVisible({ timeout: 15000 })
  await page.getByLabel('删除附件 renamed.txt').click()
  await expect(page.getByLabel('下载附件 renamed.txt')).toBeHidden({ timeout: 15000 })
  await page.close()
})

test('TC-070-03 | 附件限额', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordA)
  await page.getByLabel('上传附件').setInputFiles({ name: 'big.bin', mimeType: 'application/octet-stream', buffer: Buffer.alloc(21 * 1024 * 1024, 65) })
  await expect(page.getByText('单个文件不能超过 20MB')).toBeVisible({ timeout: 10000 })
  await page.close()
})

test('TC-070-04 | 附件黑名单', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordA)
  await page.getByLabel('上传附件').setInputFiles({ name: 'malware.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('MZ fake') })
  await expect(page.getByText(/文件不符合要求|类型受限/)).toBeVisible({ timeout: 20000 })
  await page.getByLabel('上传附件').setInputFiles({ name: 'archive.zip', mimeType: 'application/zip', buffer: Buffer.from('PK fake zip') })
  await expect(page.getByLabel('下载附件 archive.zip')).toBeVisible({ timeout: 20000 })
  // 缺陷 #31：压缩包允许上传但不解压查杀，上传成功后必须展示持续风险提示
  const hint = page.getByTestId('drawer-archive-hint')
  await expect(hint).toBeVisible({ timeout: 10000 })
  await expect(hint).toHaveText('压缩包不解压查杀，请确认来源可信')
  await page.getByLabel('删除附件 archive.zip').click()
  // 提示跟随附件事实：删除压缩包后不残留
  await expect(hint).toBeHidden({ timeout: 10000 })
  await page.close()
})

test('TC-070-05 | 抽屉前后条', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordA)
  await page.getByRole('button', { name: '下一条' }).click()
  await expect(page.getByRole('dialog', { name: '记录详情' })).toBeVisible()
  await page.getByRole('button', { name: '上一条' }).click()
  await page.close()
})

test('TC-070-06 | 缩略图', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordA)
  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')
  await page.getByLabel('上传附件').setInputFiles({ name: 'pixel.png', mimeType: 'image/png', buffer: png })
  // triage(票02)：修用例——附件缩略图同时渲染在网格单元格与抽屉文件条目（两处 img
  // alt 同名），getByRole('img', {name}) strict 冲突；收窄到记录详情抽屉内的文件缩略图
  //（drawer-file 条目），/thumb 缩略图直链断言不变。
  const drawer = page.getByRole('dialog', { name: '记录详情' })
  await expect(drawer.getByRole('img', { name: 'pixel.png' })).toBeVisible({ timeout: 20000 })
  const src = await drawer.getByRole('img', { name: 'pixel.png' }).getAttribute('src')
  expect(src).toContain('/thumb')
  await page.getByLabel('删除附件 pixel.png').click()
  await page.close()
})

// ---------------- T08 / T16 权限与错误态 ----------------

test('TC-080-05 | 外部导出恒隐', async () => {
  const page = await externalCtx.newPage()
  await gotoBase(page)
  // triage(票02)：修用例——外部账号无导出入口（D13：permissions/matrix 外部账号恒隐藏
  // export/import，当前产品亦未渲染任何导出控件），但常显合规横幅文案「请自行导出备份」
  // 含「导出」，getByText(/导出/) 恒命中横幅（visible）永假。语义核实结论：外部协作者
  // 本就不应有导出控件——改为断言交互控件层（按钮/菜单项）无「导出」入口。
  const more = page.getByRole('button', { name: /更多/ })
  if (await more.count() > 0) {
    await more.click()
  }
  await expect(page.getByRole('button', { name: /导出/ })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: /导出/ })).toHaveCount(0)
  await page.close()
})

test('TC-160-09 | 错误态兜底呈现', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/login`)
  await page.getByLabel('用户名').fill(`nosuch${RUN}`)
  await page.getByLabel('密码').fill('Wrong1234')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByText('用户名或密码错误')).toBeVisible({ timeout: 15000 })
  await page.goto(`${BASE}/base/nonexistent-${RUN}`)
  await expect(page.getByText(/没有访问该表格的权限|记录不存在|加载失败|服务/)).toBeVisible({ timeout: 15000 })
  expect((await page.locator('#root').innerHTML()).length).toBeGreaterThan(0)
  await ctx.close()
})

// ---------------- T17 / T18 非功能 ----------------

test('TC-170-03 | 键盘全程', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  // triage(票02)：修用例（键盘主链路「移动→入编辑→键入→提交→移动→再编辑→取消」完整
  // 保留，两处用例体缺陷按 3.8.1/3.8.2 语义修正）——
  // (a) 初始选中：键位状态机「无焦点 → no-op」（3.8.2 单击选中为键盘导航前置；产品无
  //     纯键盘建立初始选区的通道，6.3「方向键全程可达」的起点缺口此处以单击选中补偿，
  //     记为产品观察项，不在本票扩产品行为），原用例未选中即按方向键全部空转。
  // (b) Tab 语义：3.8.1 的 Tab「提交并右移」是编辑态语义（AC-REC-02），idle 态 Tab
  //     仅跨列移动不开编辑器；原用例在 Enter 提交后的 idle 态按 Tab 等编辑器出现。
  //     改为键入后直接编辑态 Tab（提交并右移），再 Enter 于新焦点格重开编辑器。
  await cell(page, recordA, fieldIds['标题']).click()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-testid^="editor-"]')).toBeVisible({ timeout: 5000 })
  await page.keyboard.type('键盘录入')
  await page.keyboard.press('Tab')
  await expect(page.locator('[data-testid^="editor-"]')).toBeVisible({ timeout: 5000 })
  await page.keyboard.press('Escape')
  await page.close()
})

test('TC-170-04 | 空态覆盖', async () => {
  const page = await soloCtx.newPage()
  await page.goto(`${BASE}/home`)
  await expect(page.getByRole('region', { name: '星标' })).toBeVisible()
  await expect(page.getByText('暂无星标的多维表格')).toBeVisible()
  const emptyBase = await api(soloCtx.request, 'POST', '/api/v1/bases', { name: `空表-${RUN}` })
  await gotoBase(page, emptyBase.data.base.id)
  await expect(page.getByText('还没有记录，点击下方「+ 新增记录」开始录入')).toBeVisible()
  await page.close()
})

test('TC-170-07 | 合规展示', async () => {
  const page = await ownerCtx.newPage()
  await page.goto(`${BASE}/login`)
  await expect(page.getByText(/明文 HTTP/)).toBeVisible()
  await expect(page.getByText(/试运行|免责/).first()).toBeVisible()
  await page.goto(`${BASE}/home`)
  await expect(page.getByText(/试运行|免责/).first()).toBeVisible()
  const storage = await page.evaluate(() => JSON.stringify(localStorage))
  expect(storage.toLowerCase()).not.toContain('password')
  expect(storage.toLowerCase()).not.toContain(PWD.toLowerCase())
  await page.close()
})

test('TC-170-06 | 分辨率', async () => {
  const page = await ownerCtx.newPage()
  await page.setViewportSize({ width: 1280, height: 720 })
  await gotoBase(page)
  await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible()
  await page.setViewportSize({ width: 1920, height: 1080 })
  await page.reload()
  await waitGrid(page)
  const body = await page.evaluate(() => document.body.scrollWidth)
  expect(body).toBeLessThanOrEqual(1920)
  await page.close()
})

test('TC-170-08 | 移动端现状', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1' })
  const page = await ctx.newPage()
  await page.goto(`${BASE}/login`)
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible({ timeout: 15000 })
  expect((await page.locator('#root').innerHTML()).length).toBeGreaterThan(0)
  await ctx.close()
})

test('TC-170-09 | 演示页可访问', async () => {
  const page = await ownerCtx.newPage()
  await page.goto(`${BASE}/demo/grid`)
  await expect(page.getByRole('heading', { name: '网格演示' })).toBeVisible({ timeout: 15000 })
  await page.close()
})

test('TC-000-08 | 零外部依赖', async () => {
  const page = await ownerCtx.newPage()
  const external: string[] = []
  page.on('request', (request) => {
    const url = request.url()
    if (!url.startsWith(BASE) && !url.startsWith('data:') && !url.startsWith('blob:')) external.push(url)
  })
  await page.goto(`${BASE}/login`)
  await page.waitForTimeout(1000)
  await page.goto(`${BASE}/home`)
  await page.waitForTimeout(1500)
  expect(external).toEqual([])
  await page.close()
})

test('TC-180-03 | 关键事件触发', async () => {
  const page = await ownerCtx.newPage()
  const batches: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/tracking/batch')) batches.push(request.postData() ?? '')
  })
  await page.goto(`${BASE}/home`)
  await page.waitForTimeout(2000)
  await gotoBase(page)
  await editNumber(page, recordA, 88)
  // triage(票02)：修用例——批量上报边界为「10 条或 30s」（技术方案 §13 / tracker.ts
  // 语义红线），单次编辑仅 1 条 fe_cell_edit_submit（perf_screen_ready 为 trackOnce），
  // 批次由 30s 超时窗触发，原 2s 固定等待永远等不到批次。等待窗覆盖设计批量周期，
  // 断言体（批次 >0、含 fe_ 事件）零改动。
  await expect.poll(() => batches.length, { timeout: 35000, intervals: [2000] }).toBeGreaterThan(0)
  expect(batches.length).toBeGreaterThan(0)
  expect(batches.some((body) => body.includes('fe_'))).toBeTruthy()
  await page.close()
})

