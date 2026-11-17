// 全流程 P0 UI 用例（对 http://127.0.0.1:8080 单机部署执行）
// 运行：npx playwright test e2e/p0.spec.ts --workers=1
import { test, expect, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'

const BASE = 'http://127.0.0.1:8080'
const RUN = 'ui' + Date.now().toString(36).slice(-6)
const PWD = 'Pass1234'
const OWNER = `uown${RUN}`
const FILLER = `ufil${RUN}`
const VIEWER = `uvie${RUN}`
const EDITOR = `uedt${RUN}`
const EXTERNAL = `uext${RUN}`

let ownerCtx: BrowserContext
let fillerCtx: BrowserContext
let viewerCtx: BrowserContext
let editorCtx: BrowserContext
let externalCtx: BrowserContext
let baseId = ''
let tableId = ''
let viewId = ''
const fieldIds: Record<string, string> = {}
let mainRecordId = ''

async function api(ctx: APIRequestContext, method: string, path: string, data?: unknown): Promise<any> {
  const res = await ctx.fetch(BASE + path, { method, data })
  return res.json().catch(() => null)
}

async function register(ctx: APIRequestContext, username: string, extra: Record<string, unknown> = {}) {
  return api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD, ...extra })
}

/** 注册限速 10/min：命中 5003 时等待滑窗后重试，避免整批用例被前置账号拖垮。 */
async function registerWithRetry(ctx: APIRequestContext, username: string, extra: Record<string, unknown> = {}) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const res = await register(ctx, username, extra)
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 15000))
  }
  return register(ctx, username, extra)
}

async function login(ctx: APIRequestContext, username: string, password = PWD) {
  return api(ctx, 'POST', '/api/v1/auth/login', { username, password })
}

async function waitWorkspace(page: Page) {
  await expect(page.locator('[role="grid"]')).toBeVisible({ timeout: 20000 })
  await expect(page.locator('[role="columnheader"]').first()).toBeVisible({ timeout: 20000 })
}

async function gotoBase(page: Page, id: string) {
  await page.goto(`${BASE}/base/${id}`)
  await waitWorkspace(page)
}

async function inviteAndAccept(targetCtx: BrowserContext, username: string) {
  const invite = await api(ownerCtx.request, 'POST', '/api/v1/enterprise/invites', { username_list: [username] })
  expect(invite?.code).toBe(0)
  const list = await api(targetCtx.request, 'GET', '/api/v1/invitations')
  const invitation = (list?.data?.invitation_list ?? []).find((item: any) => item.username === username)
  expect(invitation).toBeTruthy()
  expect((await api(targetCtx.request, 'POST', `/api/v1/invitations/${invitation.id}/accept`))?.code).toBe(0)
  expect((await login(targetCtx.request, username))?.code).toBe(0)
}

test.describe.configure({ timeout: 60000 })

test.beforeAll(async ({ browser }) => {
  // 注册限速（10/min）会让 5 账号初始化在 5003 重试上耗时超过默认 30s 钩子上限。
  test.setTimeout(180000)
  ownerCtx = await browser.newContext()
  expect((await registerWithRetry(ownerCtx.request, OWNER, { enterprise_name: `UI测试企业${RUN.slice(-4)}` }))?.code).toBe(0)
  expect((await login(ownerCtx.request, OWNER))?.code).toBe(0)

  for (const [name, holder] of [
    [FILLER, (c: BrowserContext) => { fillerCtx = c }],
    [VIEWER, (c: BrowserContext) => { viewerCtx = c }],
    [EDITOR, (c: BrowserContext) => { editorCtx = c }],
  ] as const) {
    const ctx = await browser.newContext()
    holder(ctx)
    expect((await registerWithRetry(ctx.request, name))?.code).toBe(0)
    expect((await login(ctx.request, name))?.code).toBe(0)
    await inviteAndAccept(ctx, name)
  }

  externalCtx = await browser.newContext()
  expect((await registerWithRetry(externalCtx.request, EXTERNAL))?.code).toBe(0)
  expect((await login(externalCtx.request, EXTERNAL))?.code).toBe(0)

  const created = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `工作台-${RUN}` })
  expect(created?.code).toBe(0)
  baseId = created.data.base.id
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/tables`)
  tableId = tables.data.table_list[0].id
  const views = await api(ownerCtx.request, 'GET', `/api/v1/tables/${tableId}/views`)
  viewId = views.data.view_list[0].id
  const fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${tableId}/fields`)
  for (const field of fields.data.field_list) fieldIds[field.name] = field.id

  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: FILLER, role: 'filler' }))?.code).toBe(0)
  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: VIEWER, role: 'viewer' }))?.code).toBe(0)
  expect((await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/shares/members`, { username: EDITOR, role: 'editor' }))?.code).toBe(0)

  const mainRecord = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
    table_id: tableId,
    cells_map: { [fieldIds['标题']]: '主记录' },
  })
  expect(mainRecord?.code).toBe(0)
  mainRecordId = mainRecord.data.record.id
})

test.afterAll(async () => {
  await ownerCtx?.close()
  await fillerCtx?.close()
  await viewerCtx?.close()
  await editorCtx?.close()
  await externalCtx?.close()
})

// ---------------- T00 部署与同源可达 ----------------

test('TC-000-01 | 入口可达', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  const response = await page.goto(BASE + '/')
  expect(response?.status()).toBe(200)
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible({ timeout: 15000 })
  await expect(page.getByText('当前为明文 HTTP 传输（无加密）')).toBeVisible()
  expect((await page.locator('#root').innerHTML()).length).toBeGreaterThan(0)
  await ctx.close()
})

test('TC-000-02 | history 回退', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  for (const path of ['/home', '/base/nonexistent-id', '/any/deep/link']) {
    const response = await page.request.get(BASE + path)
    expect(response.status(), path).toBe(200)
    expect(response.headers()['content-type'], path).toContain('text/html')
    const html = await response.text()
    expect(html, path).toContain('<div id="root">')
  }
  await page.goto(BASE + '/base/nonexistent-id')
  await expect(page.getByRole('heading', { name: '登录' })).toBeVisible({ timeout: 15000 })
  await ctx.close()
})

// ---------------- T01 认证与会话 ----------------

test('TC-010-01 | 直接注册建企业', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE + '/register')
  await expect(page.getByText('注册即为你新建一个企业')).toBeVisible()
  await page.getByLabel('用户名').fill(`ureg${RUN}`)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '注册', exact: true }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 20000 })
  await expect(page.getByRole('heading', { name: '协作多维表格' })).toBeVisible()
  const me = await api(ctx.request, 'GET', '/api/v1/auth/me')
  expect(me?.code).toBe(0)
  expect(me.data.enterprise?.id ?? me.data.user?.enterprise_id).toBeTruthy()
  await ctx.close()
})

test('TC-010-04 | 登录成功', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE + '/login')
  await page.getByLabel('用户名').fill(OWNER)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/home$/, { timeout: 20000 })
  await expect(page.getByRole('heading', { name: '协作多维表格' })).toBeVisible()
  // 期望顶栏通知铃铛与用户信息可见（M2 首页未实现则失败）
  await expect(page.getByRole('button', { name: /通知/ })).toBeVisible({ timeout: 5000 })
  // 首页卡片现按 H-21 显示可读修改人（缺失回退用户名），用户名会同时出现在
  // 顶栏菜单与卡片修改人处——定位收窄到顶栏用户菜单按钮。
  await expect(page.getByRole('button', { name: `用户菜单：${OWNER}` })).toBeVisible()
  await ctx.close()
})

test('TC-010-05 | 登录失败', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE + '/login')
  await page.getByLabel('用户名').fill(OWNER)
  await page.getByLabel('密码').fill('Wrong1234')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('form').getByRole('alert')).toHaveText('用户名或密码错误', { timeout: 10000 })
  await expect(page.getByText('如遗忘密码，账号将无法使用')).toBeVisible()
  await expect(page.getByRole('link', { name: /忘记密码/ })).toHaveCount(0)
  await ctx.close()
})

test('TC-010-07 | 会话过期重登', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE + '/login')
  await page.getByLabel('用户名').fill(OWNER)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/home$/)
  await gotoBase(page, baseId)
  await ctx.request.post(BASE + '/api/v1/auth/logout')
  await page.getByRole('button', { name: '+ 新增记录' }).click()
  const dialog = page.getByRole('dialog', { name: '登录状态已失效' })
  await expect(dialog).toBeVisible({ timeout: 15000 })
  await dialog.getByLabel('用户名').fill(OWNER)
  await dialog.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '重新登录' }).click()
  await expect(dialog).toHaveCount(0, { timeout: 15000 })
  await expect(page).toHaveURL(new RegExp(`/base/${baseId}$`))
  await ctx.close()
})

test('TC-010-08 | 登出', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE + '/login')
  await page.getByLabel('用户名').fill(OWNER)
  await page.getByLabel('密码').fill(PWD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/home$/)
  // 退出登录已归入顶栏用户信息菜单（票 07）。
  await page.getByRole('button', { name: /用户菜单/ }).click()
  await page.getByRole('menuitem', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/login$/, { timeout: 10000 })
  const me = await api(ctx.request, 'GET', '/api/v1/auth/me')
  expect(me?.code).toBe(2001)
  await ctx.close()
})

// ---------------- T02 首页与 Base 管理 ----------------

test('TC-020-01 | 首页分区聚合', async ({ page }) => {
  const b1 = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `聚合A-${RUN}` })
  const b2 = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `聚合B-${RUN}` })
  // 用 TC-010-01 注册的第二企业账号建 Base 并共享给 owner（内部 editor 无权建 Base）
  const sharerCtx = await page.context().browser()!.newContext()
  expect((await login(sharerCtx.request, `ureg${RUN}`))?.code).toBe(0)
  const peerBase = await api(sharerCtx.request, 'POST', '/api/v1/bases', { name: `共享C-${RUN}` })
  expect(peerBase?.code).toBe(0)
  expect((await api(sharerCtx.request, 'POST', `/api/v1/bases/${peerBase.data.base.id}/shares/members`, { username: OWNER, role: 'viewer' }))?.code).toBe(0)
  await api(ownerCtx.request, 'POST', `/api/v1/bases/${b1.data.base.id}/star`, { starred: true })
  const fresh = await ownerCtx.newPage()
  await fresh.goto(BASE + '/home')
  await expect(fresh.getByRole('heading', { name: '协作多维表格' })).toBeVisible()
  await expect(fresh.locator('section[aria-label="星标"]')).toContainText(`聚合A-${RUN}`)
  await expect(fresh.locator('section[aria-label="我创建的"]')).toContainText(`聚合A-${RUN}`)
  await expect(fresh.locator('section[aria-label="我创建的"]')).toContainText(`聚合B-${RUN}`)
  await expect(fresh.locator('section[aria-label="与我共享"]')).toContainText(`共享C-${RUN}`)
  await fresh.close()
  await sharerCtx.close()
})

test('TC-020-02 | 新建 Base', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await fresh.goto(BASE + '/home')
  await fresh.getByRole('button', { name: '新建多维表格' }).click()
  await expect(fresh).toHaveURL(/\/base\//, { timeout: 20000 })
  await waitWorkspace(fresh)
  await expect(fresh.locator('[role="tab"]').first()).toContainText('表格')
  await expect(fresh.getByText('主字段').first()).toBeVisible()
  const id = new URL(fresh.url()).pathname.split('/').pop() as string
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${id}/tables`)
  expect(tables.data.table_list.length).toBe(1)
  const views = await api(ownerCtx.request, 'GET', `/api/v1/tables/${tables.data.table_list[0].id}/views`)
  expect(views.data.view_list.length).toBe(1)
  await fresh.close()
})

test('TC-020-03 | 删除 Base 强提示', async ({ page }) => {
  const temp = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `待删-${RUN}` })
  const tempId = temp.data.base.id
  const fresh = await ownerCtx.newPage()
  await fresh.goto(BASE + '/home')
  const cardMenu = fresh.getByRole('region', { name: '我创建的' }).getByRole('button', { name: `更多操作：待删-${RUN}` })
  await cardMenu.click()
  await fresh.getByRole('menuitem', { name: '删除' }).click()
  const dialog = fresh.getByRole('dialog', { name: '删除多维表格' })
  await expect(dialog).toContainText('删除后不可恢复（P0）')
  await dialog.getByRole('button', { name: '确认删除' }).click()
  await expect(fresh.getByRole('article', { name: `多维表格：待删-${RUN}` })).toHaveCount(0, { timeout: 15000 })
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${tempId}/tables`)
  expect(tables?.code).not.toBe(0)
  await fresh.close()
})

test('TC-020-04 | 外部无新建入口', async ({ page }) => {
  const fresh = await externalCtx.newPage()
  await fresh.goto(BASE + '/home')
  await expect(fresh.getByRole('heading', { name: '协作多维表格' })).toBeVisible({ timeout: 20000 })
  await expect(fresh.getByRole('button', { name: '新建多维表格' })).toHaveCount(0)
  await expect(fresh.locator('section[aria-label="我创建的"]')).toHaveCount(0)
  await expect(fresh.locator('section[aria-label="最近访问"]')).toHaveCount(0)
  await expect(fresh.locator('section[aria-label="星标"]')).toHaveCount(0)
  await expect(fresh.locator('section[aria-label="与我共享"]')).toBeVisible()
  await fresh.close()
})

// ---------------- T03 数据表管理 ----------------

test('TC-030-01 | 新增表', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const tableName = `新表-${RUN.slice(-3)}`
  await fresh.getByRole('button', { name: '新建数据表' }).click()
  const dialog = fresh.getByRole('dialog', { name: '新建数据表' })
  await dialog.getByLabel('数据表名称').fill(tableName)
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(fresh.locator('[role="tab"]').filter({ hasText: tableName })).toBeVisible({ timeout: 15000 })
  await expect(fresh.locator('[role="columnheader"]').first()).toBeVisible({ timeout: 15000 })
  await expect(fresh.getByText('主字段').first()).toBeVisible()
  await expect(fresh.getByRole('button', { name: '新建视图' })).toBeVisible()
  await fresh.close()
})

test('TC-030-02 | 最后一张表禁删', async ({ page }) => {
  const temp = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `单表-${RUN}` })
  const tempId = temp.data.base.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, tempId)
  await fresh.getByRole('button', { name: /表操作：/ }).click()
  const deleteItem = fresh.getByRole('menuitem', { name: '删除表' })
  await expect(deleteItem).toBeDisabled()
  await expect(fresh.getByText('至少保留一张表')).toBeVisible()
  await fresh.close()
})

// ---------------- T04 字段 ----------------

test('TC-040-01 | 新增字段', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByRole('button', { name: '新增字段' }).click()
  const dialog = fresh.getByRole('dialog', { name: '新增字段' })
  await dialog.getByLabel('字段类型').selectOption('number')
  await dialog.getByLabel('字段名称').fill('金额')
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(fresh.locator('[role="columnheader"]').filter({ hasText: '金额' })).toBeVisible({ timeout: 15000 })
  const fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${tableId}/fields`)
  expect((fields.data.field_list as any[]).some((f) => f.name === '金额')).toBe(true)
  await fresh.close()
})

test('TC-040-02 | 主字段保护', async ({ page }) => {
  const temp = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `主字段-${RUN}` })
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, temp.data.base.id)
  await fresh.getByRole('button', { name: '字段操作：标题' }).click()
  const menu = fresh.getByRole('menu', { name: '标题 字段菜单' })
  await expect(menu.getByRole('menuitem', { name: '重命名字段' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '字段配置' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: '删除字段' })).toHaveCount(0)
  await expect(menu.getByRole('menuitem', { name: '更改类型' })).toHaveCount(0)
  await expect(menu.getByRole('menuitem', { name: '复制字段' })).toHaveCount(0)
  await menu.getByRole('menuitem', { name: '重命名字段' }).click()
  const dialog = fresh.getByRole('dialog', { name: '重命名字段' })
  await dialog.getByLabel('字段名称').fill('标题改')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(fresh.locator('[role="columnheader"]').first()).toContainText('标题改')
  await expect(fresh.getByText('主字段').first()).toBeVisible()
  await fresh.close()
})

test('TC-040-04 | 多行文本上限拦截', async ({ page }) => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${tableId}/fields`, { name: '长文本', type: 'text', config_map: {} })
  const fieldId = created.data.field.id
  const rec = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, { table_id: tableId, cells_map: { [fieldIds['标题']]: '上限记录' } })
  const recordId = rec.data.record.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByTestId(`cell-${recordId}-${fieldId}`).dblclick()
  const editor = fresh.getByTestId(`editor-${recordId}-${fieldId}`).locator('input')
  await editor.fill('x'.repeat(10001))
  await editor.press('Enter')
  await expect(fresh.getByRole('alert').filter({ hasText: '内容超出上限 10000 字符' })).toBeVisible({ timeout: 5000 })
  await editor.press('Escape')
  await fresh.close()
})

test('TC-040-05 | 数字格式与非法值', async ({ page }) => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${tableId}/fields`, {
    name: '金额二', type: 'number', config_map: { format: 'decimal', decimals: 2, thousands: true, currency: '¥' },
  })
  const fieldId = created.data.field.id
  const rec = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, { table_id: tableId, cells_map: { [fieldIds['标题']]: '数字记录' } })
  const recordId = rec.data.record.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const target = fresh.getByTestId(`cell-${recordId}-${fieldId}`)
  await target.dblclick()
  const editor = fresh.getByLabel('数字编辑')
  await editor.fill('abc')
  await expect(editor).toHaveValue('')
  await editor.fill('1234.567')
  await editor.press('Enter')
  await expect(target).toContainText('1,234.57', { timeout: 5000 })
  await fresh.close()
})

test('TC-040-06 | 单选选项', async ({ page }) => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${tableId}/fields`, {
    name: '状态二', type: 'select',
    config_map: { option_list: [{ name: '待办', color: 'blue' }, { name: '完成', color: 'green' }], allow_create: false },
  })
  expect(created?.code).toBe(0)
  const fieldId = created.data.field.id
  const options = created.data.field.config_map?.option_list ?? []
  expect(options.length).toBe(2)
  expect(options[0].color).toBeTruthy()
  const rec = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, { table_id: tableId, cells_map: { [fieldIds['标题']]: '选项记录' } })
  const recordId = rec.data.record.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByTestId(`cell-${recordId}-${fieldId}`).dblclick()
  await expect(fresh.getByRole('listbox', { name: '选项列表' })).toBeVisible()
  const search = fresh.getByRole('combobox', { name: '搜索选项' })
  await search.fill('不存在的选项')
  await search.press('Enter')
  await expect(fresh.getByRole('alert').filter({ hasText: '无匹配选项，请联系表格管理员添加' })).toBeVisible()
  await fresh.keyboard.press('Escape')
  await fresh.close()
})

test('TC-040-12 | 公式环检测', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByRole('button', { name: '新增字段' }).click()
  const dialog = fresh.getByRole('dialog', { name: '新增字段' })
  await dialog.getByLabel('字段类型').selectOption('formula')
  await dialog.getByLabel('字段名称').fill('循环公式')
  await dialog.getByLabel('公式表达式').fill('{循环公式}')
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(dialog.getByRole('alert')).toBeVisible({ timeout: 10000 })
  await expect(dialog).toBeVisible()
  await fresh.close()
})

// ---------------- T05 记录 ----------------

test('TC-050-01 | 新增记录三入口', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const before = await fresh.locator('[data-record-id]').count()
  await fresh.getByRole('button', { name: '+ 新增记录' }).click()
  await expect(fresh.locator('[data-record-id]')).toHaveCount(before + 1, { timeout: 8000 })
  await expect(fresh.locator('[data-testid^="editor-"]')).toBeVisible()
  await fresh.keyboard.press('Escape')
  await fresh.getByRole('button', { name: '首行新增记录' }).click()
  await expect(fresh.locator('[data-record-id]')).toHaveCount(before + 2, { timeout: 8000 })
  await fresh.keyboard.press('Escape')
  await fresh.locator('[data-record-id]').last().locator('[role="gridcell"]').first().click()
  await fresh.locator('[role="grid"]').press('ArrowDown')
  await expect(fresh.locator('[data-record-id]')).toHaveCount(before + 3, { timeout: 8000 })
  await fresh.close()
})

test('TC-050-02 | 三通道编辑', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const target = fresh.locator('[data-record-id]').first().locator('[role="gridcell"]').first()
  await target.click()
  await expect(fresh.locator('[data-testid^="editor-"]')).toHaveCount(0)
  await target.dblclick()
  await expect(fresh.locator('[data-testid^="editor-"]')).toBeVisible()
  await fresh.keyboard.press('Escape')
  await target.click()
  await fresh.keyboard.press('F2')
  await expect(fresh.locator('[data-testid^="editor-"]')).toBeVisible()
  await fresh.keyboard.press('Escape')
  await target.click()
  await fresh.keyboard.press('Enter')
  await expect(fresh.locator('[data-testid^="editor-"]')).toBeVisible()
  await fresh.keyboard.press('Escape')
  await target.click()
  await fresh.keyboard.type('x')
  await expect(fresh.locator('[data-testid^="editor-"]')).toBeVisible()
  await fresh.keyboard.press('Escape')
  await fresh.close()
})

test('TC-050-03 | 键位提交', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const first = fresh.locator('[data-record-id]').first().locator('[role="gridcell"]').first()
  await first.dblclick()
  let editor = fresh.locator('[data-testid^="editor-"] input').first()
  await editor.fill('Enter提交')
  await editor.press('Enter')
  await expect(first).toContainText('Enter提交')
  await first.dblclick()
  editor = fresh.locator('[data-testid^="editor-"] input').first()
  await editor.fill('Tab提交')
  await editor.press('Tab')
  await expect(first).toContainText('Tab提交')
  await first.dblclick()
  editor = fresh.locator('[data-testid^="editor-"] input').first()
  await editor.fill('Esc取消')
  await editor.press('Escape')
  await expect(first).not.toContainText('Esc取消')
  await fresh.close()
})

test('TC-050-09 | 删除与 7s 撤销', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const before = await fresh.locator('[data-record-id]').count()
  await fresh.locator('[data-record-id]').first().locator('input[type="checkbox"]').check()
  await fresh.getByRole('button', { name: '删除', exact: true }).click()
  await fresh.getByRole('dialog', { name: '删除记录' }).getByRole('button', { name: '确认删除' }).click()
  await expect(fresh.getByText('已删除 1 条', { exact: true })).toBeVisible({ timeout: 8000 })
  await expect(fresh.locator('[data-record-id]')).toHaveCount(before - 1)
  await fresh.getByRole('button', { name: '撤销' }).click()
  await expect(fresh.locator('[data-record-id]')).toHaveCount(before, { timeout: 8000 })
  await fresh.locator('[data-record-id]').first().locator('input[type="checkbox"]').check()
  await fresh.getByRole('button', { name: '删除', exact: true }).click()
  await fresh.getByRole('dialog', { name: '删除记录' }).getByRole('button', { name: '确认删除' }).click()
  await expect(fresh.getByText('已删除 1 条', { exact: true })).toBeVisible()
  await fresh.waitForTimeout(8500)
  await expect(fresh.getByRole('button', { name: '撤销' })).toHaveCount(0)
  await fresh.close()
})

test('TC-050-12 | 校验失败不丢内容', async ({ page }) => {
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${tableId}/fields`, { name: '网址', type: 'url', config_map: {} })
  const fieldId = created.data.field.id
  const rec = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, { table_id: tableId, cells_map: { [fieldIds['标题']]: '校验记录' } })
  const recordId = rec.data.record.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByTestId(`cell-${recordId}-${fieldId}`).dblclick()
  const urlInput = fresh.getByLabel('链接地址')
  await urlInput.fill('ftp://invalid')
  await urlInput.press('Enter')
  await expect(fresh.getByRole('alert').filter({ hasText: '请输入合法的 http/https 链接' })).toBeVisible()
  await expect(urlInput).toHaveValue('ftp://invalid')
  await urlInput.press('Escape')
  await fresh.close()
})

// ---------------- T06 视图 ----------------

test('TC-060-01 | 视图 CRUD', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByRole('button', { name: '新建视图' }).click()
  let dialog = fresh.getByRole('dialog', { name: '新建视图' })
  await dialog.getByLabel('视图名称').fill('视图A')
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(fresh.getByRole('button', { name: '视图A', exact: true })).toBeVisible({ timeout: 15000 })
  await fresh.getByRole('button', { name: '视图操作：视图A' }).click()
  await fresh.getByRole('menuitem', { name: '重命名视图' }).click()
  dialog = fresh.getByRole('dialog', { name: '重命名视图' })
  await dialog.getByLabel('视图名称').fill('视图B')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(fresh.getByRole('button', { name: '视图B', exact: true })).toBeVisible()
  await fresh.getByRole('button', { name: '视图操作：视图B' }).click()
  await fresh.getByRole('menuitem', { name: '复制视图' }).click()
  const copyName = '视图B 的副本'
  await expect(fresh.getByRole('button', { name: copyName, exact: true })).toBeVisible({ timeout: 15000 })
  await fresh.getByRole('button', { name: `视图操作：${copyName}` }).click()
  await fresh.getByRole('menuitem', { name: '删除视图' }).click()
  dialog = fresh.getByRole('dialog', { name: '删除视图' })
  await dialog.getByRole('button', { name: '确认删除' }).click()
  await expect(fresh.getByRole('button', { name: copyName, exact: true })).toHaveCount(0, { timeout: 15000 })
  await fresh.close()
})

test('TC-060-07 | 冻结列', async ({ page }) => {
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  const headers = fresh.locator('[role="columnheader"]')
  expect(await headers.count()).toBeGreaterThanOrEqual(3)
  await fresh.getByRole('button', { name: '字段显隐' }).click()
  const panel = fresh.getByRole('region', { name: '字段显隐面板' })
  await panel.getByLabel('冻结到第 N 列').selectOption('3')
  const first = headers.nth(0)
  const third = headers.nth(2)
  expect(await first.evaluate((el) => getComputedStyle(el).position)).toBe('sticky')
  expect(await third.evaluate((el) => getComputedStyle(el).position)).toBe('sticky')
  const before = await first.boundingBox()
  await fresh.locator('[role="grid"]').evaluate((el) => { el.scrollLeft = 400 })
  await fresh.waitForTimeout(500)
  const after = await first.boundingBox()
  expect(Math.abs((after?.x ?? 0) - (before?.x ?? 0))).toBeLessThan(2)
  await fresh.close()
})

// ---------------- T07 抽屉 ----------------

test('TC-070-01 | 抽屉字段区', async ({ page }) => {
  const longText = '长文本'.repeat(80)
  const created = await api(ownerCtx.request, 'POST', `/api/v1/tables/${tableId}/fields`, { name: '抽屉长文', type: 'text', config_map: {} })
  const fieldId = created.data.field.id
  const rec = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
    table_id: tableId,
    cells_map: { [fieldIds['标题']]: '抽屉记录', [fieldId]: longText },
  })
  const recordId = rec.data.record.id
  const fresh = await ownerCtx.newPage()
  await gotoBase(fresh, baseId)
  await fresh.getByRole('button', { name: `展开记录 ${recordId}` }).click()
  const drawer = fresh.getByRole('dialog', { name: '记录详情' })
  await expect(drawer).toBeVisible()
  const text = await drawer.getByTestId(`drawer-field-${fieldId}`).innerText()
  expect(text.includes(longText)).toBe(true)
  await fresh.close()
})

// ---------------- T08 权限矩阵 ----------------

test('TC-080-01 | 可填写数据范围', async ({ page }) => {
  const created = await api(fillerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
    table_id: tableId, cells_map: { [fieldIds['标题']]: 'filler 记录' },
  })
  expect(created?.code).toBe(0)
  const fresh = await fillerCtx.newPage()
  await gotoBase(fresh, baseId)
  await expect(fresh.getByText('仅显示你创建的记录')).toBeVisible()
  await expect(fresh.locator('[data-record-id]')).toHaveCount(1, { timeout: 15000 })
  await expect(fresh.getByRole('button', { name: '+ 新增记录' })).toBeVisible()
  await fresh.close()
})

test('TC-080-02 | 可查看只读', async ({ page }) => {
  const fresh = await viewerCtx.newPage()
  await gotoBase(fresh, baseId)
  await expect(fresh.getByRole('button', { name: '+ 新增记录' })).toHaveCount(0)
  await expect(fresh.getByRole('button', { name: '新增字段' })).toHaveCount(0)
  await expect(fresh.getByRole('button', { name: '新建视图' })).toHaveCount(0)
  await expect(fresh.getByRole('button', { name: '新建数据表' })).toHaveCount(0)
  await expect(fresh.locator('[data-record-id]').first()).toBeVisible()
  await expect(fresh.locator('[role="columnheader"]').first()).toBeVisible()
  await fresh.close()
})

test('TC-080-03 | 入口显隐矩阵抽样', async ({ page }) => {
  const managerPage = await ownerCtx.newPage()
  await gotoBase(managerPage, baseId)
  await expect(managerPage.getByRole('button', { name: '分享' })).toBeVisible()
  await managerPage.getByRole('button', { name: '更多' }).click()
  await expect(managerPage.getByRole('menu', { name: 'Base 操作菜单' })).toContainText('重命名')
  await managerPage.close()

  const editorPage = await editorCtx.newPage()
  await gotoBase(editorPage, baseId)
  await expect(editorPage.getByRole('button', { name: '分享' })).toBeVisible()
  await editorPage.getByRole('button', { name: '更多' }).click()
  await expect(editorPage.getByRole('menu', { name: 'Base 操作菜单' })).not.toContainText('重命名')
  await expect(editorPage.getByRole('button', { name: '新增字段' })).toHaveCount(0)
  await editorPage.close()

  for (const ctx of [fillerCtx, viewerCtx]) {
    const p = await ctx.newPage()
    await gotoBase(p, baseId)
    await expect(p.getByRole('button', { name: '分享' })).toHaveCount(0)
    await expect(p.getByRole('button', { name: '更多' })).toHaveCount(0)
    await p.close()
  }
  test.info().annotations.push({ type: 'blocked-half', description: '导入/导出/分享弹窗 UI 属 M4，未实现；仅抽样验证 更多/分享/表头入口' })
})

// ---------------- T13 分享入口 ----------------

test('TC-130-01 | 任何人链接开通→进入（UI 侧）', async ({ browser }) => {
  const link = await api(ownerCtx.request, 'PUT', `/api/v1/bases/${baseId}/link`, { scope: 'anyone', role: 'filler' })
  expect(link?.code).toBe(0)
  const token = (link.data.link_config.url as string).split('/').pop()
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/s/${token}`)
  await expect(page).toHaveURL(/\/register\?link_token=/, { timeout: 20000 })
  await page.getByLabel('用户名').fill(`ulnk${RUN}`)
  await page.getByLabel('密码').fill(PWD)
  // 注册限速 10/min：命中「操作过于频繁」时等待滑窗后重试提交
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await page.getByRole('button', { name: '注册', exact: true }).click()
    await page.waitForTimeout(1500)
    const url = page.url()
    if (!url.includes('/register')) break
    const alert = page.locator('main').getByRole('alert').last()
    const text = await alert.innerText().catch(() => '')
    if (!text.includes('操作过于频繁')) break
    await page.waitForTimeout(15000)
  }
  // 期望：注册后直接进入该 Base（实际实现进入 /home）
  await expect(page).toHaveURL(new RegExp(`/base/${baseId}`), { timeout: 20000 })
  await expect(page.locator('[role="grid"]')).toBeVisible({ timeout: 20000 })
  await ctx.close()
})
