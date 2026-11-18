// m2-fixes 批次 E2E 验收专项（票 09）：对 http://127.0.0.1:8080 live stack 执行。
// 覆盖票 01-08 修复的验收主线：建字段→维护选项→单选点击选中→多选连续勾选→URL 紧凑编辑→
// 表头对齐→公式实时更新→日历完整可见→抽屉提交桥→删除选项确认→公式表达式校验→三键回归。
// 运行：npx playwright test e2e/m2-fixes.spec.ts --workers=1 --reporter=list
// 可用 WM2F09_BASE=http://localhost:5199 覆盖目标（dev harness：vite dev + /api 代理到 8080）。
// 限速红线：注册 10/min、登录 20/min——每次运行仅 2 个账号（API 主账号 + UI 注册账号），
// 前缀 wm2f09 隔离；状态经 tmpdir 文件跨 worker 保留（用例失败触发 worker 重建后可续跑），
// 各建数据步骤带幂等守卫（已存在则跳过），可对本地栈重复执行（共享 dev 库不假设为空）。
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

import { test, expect, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'

const BASE = process.env.WM2F09_BASE ?? 'http://127.0.0.1:8080'
const SUF = Date.now().toString(36).slice(-6)
const PWD = 'Pass1234'
const OWNER = `wm2f09own${SUF}`
const UI_USER = `wm2f09ui${SUF}`
// 字段名不随运行后缀（每次运行新建独立表内唯一）；状态文件保证 worker 重建后引用一致。
const T_SELECT = '单选A'
const T_MULTI = '多选A'
const T_URL = '超链接A'
const T_DATE = '日期A'
const T_FORMULA = '公式A'
const TITLE_INIT = `标题初始${SUF}`
const TITLE_REALTIME = `标题实时${SUF}`
const FORMULA_PREFIX = 'M2F-'

// ---- 跨 worker 状态（worker 重建时 beforeAll 重跑；经文件恢复既有 Base/字段/记录） ----
const STATE_FILE = path.join(os.tmpdir(), 'wm2f09-e2e-state.json')
const STATE_TTL_MS = 20 * 60 * 1000

interface RunState {
  createdAt: number
  owner: string
  baseId: string
  tableId: string
  recordId: string
  fieldIds: Record<string, string>
  optionIds: Record<string, Record<string, string>>
}

let ownerCtx: BrowserContext
let baseId = ''
let tableId = ''
let recordId = ''
// 状态文件归属的账号：全新初始化=本次注册账号；状态恢复=原账号（模块 OWNER 后缀随进程变化，
// 直接落盘会让下一次恢复登录到从未注册的账号 → 2002 → 状态被全新初始化覆盖）。
let stateOwner = ''

const fieldIds: Record<string, string> = {}
// 选项名 → 选项 id（TC-WM2F-05 保存后由 API 回读登记）
const optionIds: Record<string, Record<string, string>> = {}

function loadState(): RunState | null {
  try {
    const raw = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) as RunState
    if (typeof raw.createdAt !== 'number' || Date.now() - raw.createdAt > STATE_TTL_MS) return null
    if (typeof raw.owner !== 'string' || raw.baseId === '' || raw.tableId === '') return null
    return raw
  } catch {
    return null
  }
}

function saveState(): void {
  if (stateOwner === '') return
  const state: RunState = {
    createdAt: Date.now(),
    owner: stateOwner,
    baseId,
    tableId,
    recordId,
    fieldIds,
    optionIds,
  }
  try {
    fs.writeFileSync(STATE_FILE, JSON.stringify(state))
  } catch {
    // 状态文件写失败不阻断用例（worker 重建后续跑退化为全新初始化）
  }
}

async function api(ctx: APIRequestContext, method: string, path: string, data?: unknown): Promise<any> {
  const res = await ctx.fetch(BASE + path, { method, data })
  return res.json().catch(() => null)
}

/** 注册限速 10/min：命中 5003 时等待滑窗后重试。 */
async function registerWithRetry(ctx: APIRequestContext, username: string, extra: Record<string, unknown> = {}) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const res = await api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD, ...extra })
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 15000))
  }
  return api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD, ...extra })
}

/** proto Value JSON → 普通值（string/number/bool/list/对象原样）。 */
function decodeCell(raw: unknown): unknown {
  if (raw === null || raw === undefined) return null
  if (typeof raw !== 'object') return raw
  const record = raw as Record<string, unknown>
  if ('string_value' in record) return record.string_value
  if ('number_value' in record) return record.number_value
  if ('double_value' in record) return record.double_value
  if ('int_value' in record) return record.int_value
  if ('date_value_ms' in record) return record.date_value_ms
  if ('bool_value' in record) return record.bool_value
  if ('list_value' in record) {
    const list = (record.list_value as Record<string, unknown> | undefined) ?? {}
    const items = (list.value_list as unknown[] | undefined) ?? []
    return items.map((item) => decodeCell(item))
  }
  // 结构对象（如 url {url, display_text}）：逐键解码
  const decoded: Record<string, unknown> = {}
  let empty = true
  for (const [key, value] of Object.entries(record)) {
    decoded[key] = decodeCell(value)
    empty = false
  }
  return empty ? null : decoded
}

function waitGrid(page: Page) {
  return Promise.all([
    expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 }),
    expect(page.getByRole('columnheader').first()).toBeVisible({ timeout: 20000 }),
  ])
}

async function gotoBase(page: Page) {
  await page.goto(`${BASE}/base/${baseId}?table=${tableId}`)
  await waitGrid(page)
}

function cell(page: Page, recordId: string, fieldId: string) {
  return page.getByTestId(`cell-${recordId}-${fieldId}`)
}

/** 单击选中 + F2 进入编辑：避免双击因重渲染偶发不触发编辑态（与 p1p2 同口径）。 */
async function openCellEditor(page: Page, recordId: string, fieldId: string) {
  const target = cell(page, recordId, fieldId)
  await target.click()
  await target.press('F2')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeVisible({ timeout: 10000 })
}

async function openDrawer(page: Page, recordId: string) {
  await page.getByRole('button', { name: `展开记录 ${recordId}` }).click()
  await expect(page.getByRole('dialog', { name: '记录详情' })).toBeVisible({ timeout: 10000 })
}

/** 字段 id 按名称解析；tries=1 供幂等探测（不等待）。 */
async function fieldIdByName(ctx: APIRequestContext, name: string, tries = 10): Promise<string | null> {
  for (let attempt = 0; attempt < tries; attempt += 1) {
    const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
    const found = (res?.data?.field_list ?? []).find((field: any) => field.name === name)
    if (found) return found.id as string
    if (tries === 1) return null
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  return null
}

/** 字段 config_map.option_list 当前名称集合（不等待、不抛错）。 */
async function optionNamesOf(ctx: APIRequestContext, fieldId: string): Promise<string[]> {
  const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
  const found = (res?.data?.field_list ?? []).find((field: any) => field.id === fieldId)
  return (found?.config_map?.option_list ?? [])
    .map((option: any) => (typeof option.name === 'string' ? option.name : ''))
    .filter((name: string) => name !== '')
}

/** 字段 config_map.option_list 当前 id 集合（不等待、不抛错）：重跑时存量值可能引用已删选项，重提交会被后端 1001 拒绝。 */
async function optionIdsOf(ctx: APIRequestContext, fieldId: string): Promise<Set<string>> {
  const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
  const found = (res?.data?.field_list ?? []).find((field: any) => field.id === fieldId)
  return new Set(
    (found?.config_map?.option_list ?? [])
      .map((option: any) => (typeof option.id === 'string' ? option.id : ''))
      .filter((id: string) => id !== ''),
  )
}

/** 字段 config_map.option_list 回读（保存对话框关闭后轮询至全部出现）。 */
async function optionMapByName(ctx: APIRequestContext, fieldId: string, names: string[]): Promise<Record<string, string>> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
    const found = (res?.data?.field_list ?? []).find((field: any) => field.id === fieldId)
    const list = found?.config_map?.option_list ?? []
    const map: Record<string, string> = {}
    for (const option of list) {
      if (typeof option.name === 'string' && typeof option.id === 'string') map[option.name] = option.id
    }
    if (names.every((name) => map[name] !== undefined)) return map
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`选项未全部落库：${names.join(',')}`)
}

async function readRecordCells(ctx: APIRequestContext, recordId: string): Promise<Record<string, unknown>> {
  const res = await api(ctx, 'GET', `/api/v1/records/${recordId}?base_id=${baseId}`)
  if (res?.code !== 0) throw new Error(`记录读取失败：${JSON.stringify(res)}`)
  return res.data.record.cells_map
}

async function recordReadable(ctx: APIRequestContext, id: string): Promise<boolean> {
  try {
    await readRecordCells(ctx, id)
    return true
  } catch {
    return false
  }
}

/** UI 写入后的服务端值轮询（写成功 UI 才更新，正常一次即中；重试兜底读滞后）。 */
async function readCellDecoded(ctx: APIRequestContext, recordId: string, fieldId: string, tries = 6): Promise<unknown> {
  for (let attempt = 0; attempt < tries; attempt += 1) {
    const cells = await readRecordCells(ctx, recordId)
    const raw = cells[fieldId]
    if (raw !== undefined && raw !== null) return decodeCell(raw)
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  return null
}

/** 维护选项对话框（字段菜单 → 维护选项）。 */
async function openOptionsDialog(page: Page, fieldName: string) {
  await page.getByRole('button', { name: `字段操作：${fieldName}` }).click()
  await page.getByRole('menu', { name: `${fieldName} 字段菜单` }).getByRole('menuitem', { name: '维护选项' }).click()
  const dialog = page.getByRole('dialog', { name: `维护选项：${fieldName}` })
  await expect(dialog).toBeVisible({ timeout: 10000 })
  return dialog
}

/** 新增字段对话框流：选类型、命名、创建，断言列头出现。 */
async function createFieldViaUi(page: Page, type: string, name: string, expression?: string) {
  await page.getByRole('button', { name: '新增字段' }).click()
  const dialog = page.getByRole('dialog', { name: '新增字段' })
  await expect(dialog).toBeVisible({ timeout: 10000 })
  await dialog.getByLabel('字段类型').selectOption(type)
  await dialog.getByLabel('字段名称').fill(name)
  if (expression !== undefined) await dialog.getByLabel('公式表达式').fill(expression)
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(dialog).toBeHidden({ timeout: 15000 })
  await expect(page.getByRole('columnheader', { name: `字段：${name}` })).toBeVisible({ timeout: 15000 })
}

/** 幂等建字段：状态复用/重跑时字段已存在则跳过 UI 创建。 */
async function ensureFieldViaUi(page: Page, type: string, name: string, expression?: string) {
  const existing = await fieldIdByName(ownerCtx.request, name, 1)
  if (existing !== null) {
    fieldIds[name] = existing
    return
  }
  await createFieldViaUi(page, type, name, expression)
  fieldIds[name] = (await fieldIdByName(ownerCtx.request, name)) as string
}

/** 幂等补选项：只添加缺失名称；全部已存在则不开对话框。 */
async function ensureOptions(page: Page, fieldName: string, fieldId: string, names: string[]) {
  const current = await optionNamesOf(ownerCtx.request, fieldId)
  const missing = names.filter((name) => !current.includes(name))
  if (missing.length === 0) return
  const dialog = await openOptionsDialog(page, fieldName)
  for (const name of missing) {
    await dialog.getByLabel('新选项名称').fill(name)
    await dialog.getByRole('button', { name: '添加选项' }).click()
    await expect(dialog.getByLabel(`选项名称：${name}`)).toBeVisible({ timeout: 5000 })
  }
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toBeHidden({ timeout: 15000 })
}

/** 移除编辑器内「已删除的选项」失效标签：存量值引用已删选项时，后端拒绝携带失效 id 的重提交（1001），
 * 编辑器任何提交都会失败——先模拟用户点击移除失效标签，再进行后续勾选/提交。 */
async function removeStaleTags(editor: ReturnType<Page['getByTestId']>) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const stale = editor.getByRole('button', { name: '移除选项：已删除的选项' })
    const count = await stale.count()
    if (count === 0) return
    await stale.first().click()
    await expect(editor.getByRole('button', { name: '移除选项：已删除的选项' })).toHaveCount(count - 1, { timeout: 5000 })
  }
}

async function expectStoredSorted(ctx: APIRequestContext, fieldId: string, expectedIds: string[], description: string) {
  let stored: unknown = null
  for (let attempt = 0; attempt < 20; attempt += 1) {
    stored = await readCellDecoded(ctx, recordId, fieldId, 1)
    if (Array.isArray(stored) && (stored as string[]).length === expectedIds.length) break
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  expect(
    Array.isArray(stored) ? [...(stored as string[])].sort() : stored,
    description,
  ).toEqual([...expectedIds].sort())
}

test.describe.configure({ timeout: 90000 })

test.beforeAll(async ({ browser }) => {
  test.setTimeout(180000)
  ownerCtx = await browser.newContext()

  const state = loadState()
  if (state !== null) {
    // worker 重建/快速重跑恢复：登录既有账号并校验 Base/表仍可达，恢复字段与记录引用。
    // 登录限速 20/min（IP 滑窗）：命中 5003 时等待滑窗重试。
    let login: any = null
    for (let attempt = 0; attempt < 4; attempt += 1) {
      login = await api(ownerCtx.request, 'POST', '/api/v1/auth/login', { username: state.owner, password: PWD })
      if (login?.code === 0 || login?.code !== 5003) break
      await new Promise((resolve) => setTimeout(resolve, 12000))
    }
    if (login?.code === 0) {
      const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${state.baseId}/tables`)
      const fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${state.tableId}/fields`)
      const reachable = tables?.code === 0 && fields?.code === 0
        && (tables.data.table_list ?? []).some((table: any) => table.id === state.tableId)
        && (fields.data.field_list ?? []).some((field: any) => field.is_primary)
      if (reachable) {
        stateOwner = state.owner
        baseId = state.baseId
        tableId = state.tableId
        recordId = state.recordId
        Object.assign(fieldIds, state.fieldIds)
        for (const [key, value] of Object.entries(state.optionIds)) optionIds[key] = { ...value }
        console.log(`[wm2f09] 状态恢复：base=${baseId} record=${recordId || '(无)'} fields=${Object.keys(fieldIds).length}`)
        return
      }
      console.log(`[wm2f09] 状态校验失败（tables=${tables?.code} fields=${fields?.code}），走全新初始化`)
    } else {
      console.log(`[wm2f09] 状态账号登录失败（code=${login?.code}），走全新初始化`)
    }
  }

  expect((await registerWithRetry(ownerCtx.request, OWNER, { enterprise_name: `WM2F09企业${SUF}` }))?.code).toBe(0)
  stateOwner = OWNER
  expect((await api(ownerCtx.request, 'POST', '/api/v1/auth/login', { username: OWNER, password: PWD }))?.code).toBe(0)

  const created = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `WM2F09-${SUF}` })
  expect(created?.code, JSON.stringify(created)).toBe(0)
  baseId = created.data.base.id
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/tables`)
  tableId = tables.data.table_list[0].id
  const fields = await api(ownerCtx.request, 'GET', `/api/v1/tables/${tableId}/fields`)
  const primary = fields.data.field_list.find((field: any) => field.is_primary)
  fieldIds['标题'] = primary.id
  // 不在 beforeAll 保存状态：状态文件由各建数据步骤（TC-03/04/05/06）分阶段落盘，
  // 避免 worker 重建走全新初始化时用过早的空引用覆盖仍可恢复的旧状态。
})

test.afterAll(async () => {
  await ownerCtx?.close()
})

// ---------------- 主线：注册登录 → 建表 → 建字段 → 维护选项 ----------------

test('TC-WM2F-01 | 注册登录主线（UI 注册 → 进入首页）', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/register`)
  await page.getByLabel('用户名').fill(UI_USER)
  await page.getByLabel('密码').fill(PWD)
  // 注册限速 10/min：命中「操作过于频繁」时等待滑窗后重试提交
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await page.getByRole('button', { name: '注册', exact: true }).click()
    await page.waitForTimeout(1500)
    if (!page.url().includes('/register')) break
    const alert = page.locator('main').getByRole('alert').last()
    const text = await alert.innerText().catch(() => '')
    if (!text.includes('操作过于频繁')) break
    await page.waitForTimeout(15000)
  }
  await expect(page).toHaveURL(/\/home$/, { timeout: 20000 })
  await expect(page.getByRole('heading', { name: '协作多维表格' })).toBeVisible()
  const me = await api(ctx.request, 'GET', '/api/v1/auth/me')
  expect(me?.code).toBe(0)
  await ctx.close()
})

test('TC-WM2F-02 | 新建数据表（UI 建表 → 网格与主字段可见）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '新建数据表' }).click()
  const dialog = page.getByRole('dialog', { name: '新建数据表' })
  await dialog.getByLabel('数据表名称').fill(`表A-${SUF}`)
  await dialog.getByRole('button', { name: '创建' }).click()
  await expect(page.locator('[role="tab"]').filter({ hasText: `表A-${SUF}` })).toBeVisible({ timeout: 15000 })
  await expect(page.getByRole('columnheader').first()).toBeVisible({ timeout: 15000 })
  await expect(page.getByText('主字段').first()).toBeVisible()
  await page.close()
})

test('TC-WM2F-03 | 新增单选/多选/URL/日期字段（字段对话框逐个创建）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await ensureFieldViaUi(page, 'select', T_SELECT)
  await ensureFieldViaUi(page, 'multiselect', T_MULTI)
  await ensureFieldViaUi(page, 'url', T_URL)
  await ensureFieldViaUi(page, 'date', T_DATE)
  saveState()
  await page.close()
})

test('TC-WM2F-04 | 新增公式字段（CONCATENATE 引用主字段，合法表达式创建成功）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await ensureFieldViaUi(page, 'formula', T_FORMULA, `CONCATENATE('${FORMULA_PREFIX}', {标题})`)
  saveState()
  await page.close()
})

test('TC-WM2F-05 | 维护选项：单选与多选添加选项并保存落库', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await ensureOptions(page, T_SELECT, fieldIds[T_SELECT], ['甲', '乙'])
  optionIds[T_SELECT] = await optionMapByName(ownerCtx.request, fieldIds[T_SELECT], ['甲', '乙'])

  await ensureOptions(page, T_MULTI, fieldIds[T_MULTI], ['红', '黄', '蓝', '紫'])
  optionIds[T_MULTI] = await optionMapByName(ownerCtx.request, fieldIds[T_MULTI], ['红', '黄', '蓝', '紫'])
  saveState()

  // 回归：重新打开维护选项，既有选项仍在
  const reopen = await openOptionsDialog(page, T_MULTI)
  await expect(reopen.getByLabel('选项名称：红')).toBeVisible()
  await expect(reopen.getByLabel('选项名称：紫')).toBeVisible()
  await reopen.getByRole('button', { name: '关闭' }).click()
  await expect(reopen).toBeHidden()
  await page.close()
})

// ---------------- 网格编辑主线（票 01/02/08 行为） ----------------

test('TC-WM2F-06 | 新增记录并输入主字段（公式初始计算落库）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)

  if (recordId === '' || !(await recordReadable(ownerCtx.request, recordId))) {
    const before = await page.locator('[data-record-id]').count()
    await page.getByRole('button', { name: '+ 新增记录' }).click()
    await expect(page.locator('[data-record-id]')).toHaveCount(before + 1, { timeout: 10000 })
    await expect(page.locator('[data-testid^="editor-"]')).toBeVisible()
    const editor = page.locator('[data-testid^="editor-"] input').first()
    await editor.fill(TITLE_INIT)
    await editor.press('Enter')
    await expect(page.locator('[data-testid^="editor-"]')).toHaveCount(0)

    const row = page.locator('[data-record-id]').filter({ hasText: TITLE_INIT })
    await expect(row).toBeVisible()
    const createdRecordId = await row.first().getAttribute('data-record-id')
    expect(createdRecordId).toBeTruthy()
    recordId = createdRecordId as string
    saveState()
  }

  // 等公式初始计算落库（期望值跟随当前服务端标题，状态复用时标题可能是上轮修改值）
  const cells = await readRecordCells(ownerCtx.request, recordId)
  const currentTitle = String(decodeCell(cells[fieldIds['标题']]) ?? '')
  expect(currentTitle, '主字段应有值').not.toBe('')
  let formulaValue: unknown = null
  for (let attempt = 0; attempt < 40; attempt += 1) {
    formulaValue = await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_FORMULA], 1)
    if (formulaValue === `${FORMULA_PREFIX}${currentTitle}`) break
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  expect(String(formulaValue), '公式初始计算应在 20s 内落库').toBe(`${FORMULA_PREFIX}${currentTitle}`)
  await page.close()
})

test('TC-WM2F-07 | 网格单选：点击选项即提交收起（票 02）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openCellEditor(page, recordId, fieldIds[T_SELECT])
  const editor = page.getByTestId(`editor-${recordId}-${fieldIds[T_SELECT]}`)
  // 选项面板完整可见（票 01 portal 化）
  const panel = page.getByRole('listbox', { name: '选项列表' })
  await expect(panel).toBeVisible()
  const panelBox = await panel.boundingBox()
  expect(panelBox).toBeTruthy()
  expect(panelBox!.x).toBeGreaterThanOrEqual(0)
  expect(panelBox!.y).toBeGreaterThanOrEqual(0)

  await page.getByRole('option', { name: '甲', exact: true }).click()
  // 点击即提交并收起编辑器（修复前仅写本地草稿、从不提交）
  await expect(editor).toBeHidden({ timeout: 10000 })
  await expect(cell(page, recordId, fieldIds[T_SELECT])).toContainText('甲', { timeout: 10000 })
  // 值确实写入服务端（修复前单元格显示空/丢失）
  const stored = await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_SELECT])
  expect(stored).toBe(optionIds[T_SELECT]['甲'])
  await page.close()
})

test('TC-WM2F-08 | 网格多选：连续勾选 2 个后回车保存（票 02）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openCellEditor(page, recordId, fieldIds[T_MULTI])
  const editor = page.getByTestId(`editor-${recordId}-${fieldIds[T_MULTI]}`)
  const combo = editor.getByLabel('搜索选项')
  // 重跑场景：存量值可能引用已删选项（失效标签），先移除再勾选，否则提交被后端 1001 拒绝
  await removeStaleTags(editor)

  // 目标集合 = 现值 ∪ {红, 黄}，并剔除配置中已不存在的失效选项 id（重提交会被后端 1001 拒绝）
  const before = (await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_MULTI])) as unknown
  const configIdSet = await optionIdsOf(ownerCtx.request, fieldIds[T_MULTI])
  const beforeIds = (Array.isArray(before) ? (before as string[]) : []).filter((id) => configIdSet.has(id))
  const targetIds = [...new Set([...beforeIds, optionIds[T_MULTI]['红'], optionIds[T_MULTI]['黄']])]

  for (const name of ['红', '黄']) {
    if (beforeIds.includes(optionIds[T_MULTI][name])) continue
    await page.getByRole('option', { name: new RegExp(name) }).click()
    await expect(editor).toBeVisible() // 点击为勾选切换且编辑器保持打开（票 02）
    await expect(page.getByRole('option', { name: new RegExp(name) })).toHaveAttribute('aria-selected', 'true')
  }

  // 搜索词为空回车：提交完整已选集合（修复前提交值落后一拍）
  await combo.press('Enter')
  await expect(editor).toBeHidden({ timeout: 10000 })
  await expect(cell(page, recordId, fieldIds[T_MULTI])).toContainText('红', { timeout: 10000 })
  await expect(cell(page, recordId, fieldIds[T_MULTI])).toContainText('黄')

  await expectStoredSorted(ownerCtx.request, fieldIds[T_MULTI], targetIds, '多选连续勾选值应完整落库')
  await page.close()
})

test('TC-WM2F-09 | 网格 URL：紧凑单输入框编辑并渲染链接（票 08）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openCellEditor(page, recordId, fieldIds[T_URL])
  const editor = page.getByTestId(`editor-${recordId}-${fieldIds[T_URL]}`)
  // 紧凑形态：只有地址输入框，无显示文本输入（修复前双输入框挤压窄列）
  await expect(editor.getByLabel('链接地址')).toBeVisible()
  await expect(editor.getByLabel('显示文本')).toHaveCount(0)
  const urlInput = editor.getByLabel('链接地址')
  await urlInput.fill('https://example.com/wm2f09')
  await urlInput.press('Enter')
  await expect(editor).toBeHidden({ timeout: 10000 })

  const target = cell(page, recordId, fieldIds[T_URL])
  await expect(target).toContainText('https://example.com/wm2f09', { timeout: 10000 })
  // 显示文本缺省取地址本身，渲染为链接
  expect(await target.locator('a').getAttribute('href')).toBe('https://example.com/wm2f09')
  await page.close()
})

test('TC-WM2F-10 | 日期日历完整可见且点选即写入（票 01）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openCellEditor(page, recordId, fieldIds[T_DATE])
  const calendar = page.getByRole('dialog', { name: '日期选择器' })
  await expect(calendar).toBeVisible()

  // 修复前日历被单元格 overflow:hidden 裁剪（240×179 只露 ~7px）：断言完整落在视口内
  const box = await calendar.boundingBox()
  expect(box).toBeTruthy()
  const viewport = page.viewportSize()!
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height)
  expect(box!.width).toBeGreaterThanOrEqual(200)
  expect(box!.height).toBeGreaterThanOrEqual(150)

  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  await calendar.getByRole('button', { name: today, exact: true }).click()
  await expect(page.getByTestId(`editor-${recordId}-${fieldIds[T_DATE]}`)).toBeHidden({ timeout: 10000 })
  await expect(cell(page, recordId, fieldIds[T_DATE])).toContainText(today, { timeout: 10000 })

  const stored = await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_DATE])
  expect(stored === null || stored === undefined, '日期值应已落库').toBe(false)
  await page.close()
})

// ---------------- 表头对齐（票 05）与公式实时更新（票 06） ----------------

test('TC-WM2F-11 | 表头行首占位与数据行几何对齐（票 05）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)

  const headerRow = page.getByRole('row', { name: '字段表头' })
  // 行首 64px 占位存在，可勾选时含全选复选框（修复前表头无占位、整行右移 64px）
  const placeholder = headerRow.locator('div').first()
  await expect(placeholder).toBeVisible()
  await expect(headerRow.getByLabel('全选记录')).toBeVisible()
  const placeholderBox = await placeholder.boundingBox()
  expect(Math.abs((placeholderBox?.width ?? 0) - 64)).toBeLessThan(2)

  const row = page.locator(`[data-record-id="${recordId}"]`)
  const rowHeader = row.locator('div').first()
  const rowHeaderBox = await rowHeader.boundingBox()
  expect(Math.abs((rowHeaderBox?.width ?? 0) - 64)).toBeLessThan(2)

  // 逐列几何对齐：列头 x 与数据行单元格 x 偏移为 0（主字段 + 首个普通字段抽样）
  for (const fieldName of ['标题', T_SELECT]) {
    const header = page.getByRole('columnheader', { name: `字段：${fieldName}` })
    const headerBox = await header.boundingBox()
    const cellBox = await cell(page, recordId, fieldIds[fieldName]).boundingBox()
    expect(headerBox).toBeTruthy()
    expect(cellBox).toBeTruthy()
    expect(Math.abs(headerBox!.x - cellBox!.x), `${fieldName} 列头/单元格 x 偏移`).toBeLessThan(2)
  }

  // 全选复选框进入既有记录选择状态（勾选后行复选框选中，再点取消）
  const rowCheckbox = row.locator('input[type="checkbox"]').first()
  await headerRow.getByLabel('全选记录').click()
  await expect(rowCheckbox).toBeChecked()
  await headerRow.getByLabel('全选记录').click()
  await expect(rowCheckbox).not.toBeChecked()
  await page.close()
})

test('TC-WM2F-12 | 公式实时更新：改引用列后无需刷新即更新（票 06）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const formulaCell = cell(page, recordId, fieldIds[T_FORMULA])
  // 修复前：公式列停留旧值/「计算中…」直到切换页面重挂载
  await expect(formulaCell).not.toContainText(TITLE_REALTIME)

  await openCellEditor(page, recordId, fieldIds['标题'])
  const editor = page.getByTestId(`editor-${recordId}-${fieldIds['标题']}`)
  await editor.locator('input').first().fill(TITLE_REALTIME)
  await editor.locator('input').first().press('Enter')
  await expect(editor).toBeHidden({ timeout: 10000 })

  // 本行公式列立即跟随更新（无 page.reload/gotoBase）
  await expect(formulaCell).toContainText(`${FORMULA_PREFIX}${TITLE_REALTIME}`, { timeout: 15000 })
  const stored = await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_FORMULA])
  expect(stored).toBe(`${FORMULA_PREFIX}${TITLE_REALTIME}`)
  await page.close()
})

// ---------------- 抽屉主线（票 03）与选项删除确认（票 04） ----------------

test('TC-WM2F-13 | 抽屉：勾选多选 → 切换字段 → 重进值保留（票 03）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await openDrawer(page, recordId)
  const drawer = page.getByRole('dialog', { name: '记录详情' })

  const blueId = optionIds[T_MULTI]['蓝']
  const before = (await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_MULTI])) as unknown
  const configIdSet = await optionIdsOf(ownerCtx.request, fieldIds[T_MULTI])
  // 剔除配置中已不存在的失效选项 id（重提交会被后端 1001 拒绝）
  const beforeIds = (Array.isArray(before) ? (before as string[]) : []).filter((id) => configIdSet.has(id))
  const configNames = await optionNamesOf(ownerCtx.request, fieldIds[T_MULTI])

  // 打开多选编辑器，勾选「蓝」（现值已含则跳过 toggle，规避反选）
  await drawer.getByRole('button', { name: `编辑字段：${T_MULTI}` }).click()
  const editor = page.getByTestId(`drawer-editor-${fieldIds[T_MULTI]}`)
  await expect(editor).toBeVisible()
  // 重跑场景：存量值可能引用已删选项（失效标签），先移除再勾选，否则提交被后端 1001 拒绝
  await removeStaleTags(editor)
  if (configNames.includes('蓝') && !beforeIds.includes(blueId)) {
    await page.getByRole('option', { name: /蓝/ }).click()
    await expect(page.getByRole('option', { name: /蓝/ })).toHaveAttribute('aria-selected', 'true')
  }

  // 直接切换到另一字段：在途草稿按最新草稿提交（修复前切换即卸载丢草稿）。
  // 选面板上方的单选字段——打开的选项弹层会盖住多选下方的字段按钮（正常浮层行为），点上方不被遮挡。
  await drawer.getByRole('button', { name: `编辑字段：${T_SELECT}` }).click()
  await expect(page.getByTestId(`drawer-editor-${fieldIds[T_SELECT]}`)).toBeVisible({ timeout: 10000 })
  await page.keyboard.press('Escape')
  await expect(page.getByTestId(`drawer-editor-${fieldIds[T_SELECT]}`)).toBeHidden({ timeout: 10000 })

  // 等切换字段触发的提交落库，再重进编辑器（避免在途提交收起重开的编辑器）
  const expectedIds = configNames.includes('蓝') ? [...new Set([...beforeIds, blueId])] : beforeIds
  await expectStoredSorted(ownerCtx.request, fieldIds[T_MULTI], expectedIds, '切换字段后多选草稿应已提交落库')

  // 抽屉内 URL 编辑保留双字段完整形态（票 08）
  await drawer.getByRole('button', { name: `编辑字段：${T_URL}` }).click()
  await expect(page.getByTestId(`drawer-editor-${fieldIds[T_URL]}`)).toBeVisible({ timeout: 10000 })
  await expect(drawer.getByLabel('链接地址')).toBeVisible()
  await expect(drawer.getByLabel('显示文本')).toBeVisible()
  await page.keyboard.press('Escape')

  // 重进多选编辑器：切换字段时提交的勾选已保留
  await drawer.getByRole('button', { name: `编辑字段：${T_MULTI}` }).click()
  await expect(page.getByTestId(`drawer-editor-${fieldIds[T_MULTI]}`)).toBeVisible()
  await expect(editor).toContainText('红')
  await expect(editor).toContainText('黄')
  if (expectedIds.includes(blueId)) await expect(editor).toContainText('蓝')
  await page.keyboard.press('Escape')

  await drawer.getByRole('button', { name: '关闭记录详情' }).click()
  await expect(drawer).toBeHidden({ timeout: 10000 })

  await expect(cell(page, recordId, fieldIds[T_MULTI])).toContainText('红', { timeout: 15000 })
  const stored = await readCellDecoded(ownerCtx.request, recordId, fieldIds[T_MULTI])
  expect([...(stored as string[])].sort()).toEqual([...expectedIds].sort())
  await page.close()
})

test('TC-WM2F-14 | 删除未被引用的选项不弹确认（票 04）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const current = await optionNamesOf(ownerCtx.request, fieldIds[T_SELECT])
  test.skip(!current.includes('乙'), '状态复用：乙 已在先前运行删除')
  const dialog = await openOptionsDialog(page, T_SELECT)
  // 「乙」无记录引用：直接移除（不再弹「已被记录使用」确认）
  await dialog.getByLabel('删除选项：乙').click()
  await expect(dialog.getByLabel('选项名称：乙')).toHaveCount(0, { timeout: 5000 })
  await expect(dialog.getByText('已被记录使用')).toHaveCount(0)
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toBeHidden({ timeout: 15000 })

  const names = await optionNamesOf(ownerCtx.request, fieldIds[T_SELECT])
  expect(names).toEqual(['甲'])
  await page.close()
})

test('TC-WM2F-15 | 删除被引用选项出现确认弹窗；存量值兜底显示（票 04）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const current = await optionNamesOf(ownerCtx.request, fieldIds[T_MULTI])
  test.skip(!current.includes('蓝'), '状态复用：蓝 已在先前运行删除')
  const dialog = await openOptionsDialog(page, T_MULTI)
  // 「蓝」已被记录引用（TC-WM2F-13）：先出现确认弹窗
  await dialog.getByLabel('删除选项：蓝').click()
  const confirmBox = dialog.getByText('已被记录使用')
  await expect(confirmBox).toBeVisible({ timeout: 5000 })
  await expect(confirmBox).toContainText('蓝')

  // 取消则不删
  await dialog.getByRole('button', { name: '取消' }).click()
  await expect(dialog.getByText('已被记录使用')).toHaveCount(0)
  await expect(dialog.getByLabel('选项名称：蓝')).toBeVisible()

  // 确认后删除并保存
  await dialog.getByLabel('删除选项：蓝').click()
  await dialog.getByRole('button', { name: '确认删除' }).click()
  await expect(dialog.getByLabel('选项名称：蓝')).toHaveCount(0)
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toBeHidden({ timeout: 15000 })

  // 存量值不再显示原始 ID 长串，转为「已删除的选项」灰色标签
  const target = cell(page, recordId, fieldIds[T_MULTI])
  await expect(target).toContainText('已删除的选项', { timeout: 15000 })
  await expect(target).toContainText('红')
  await expect(target).toContainText('黄')
  expect(await target.innerText()).not.toContain(optionIds[T_MULTI]['蓝'])

  // 编辑器内的失效 ID 标签同文案
  await openCellEditor(page, recordId, fieldIds[T_MULTI])
  const editor = page.getByTestId(`editor-${recordId}-${fieldIds[T_MULTI]}`)
  await expect(editor).toContainText('已删除的选项')
  await page.keyboard.press('Escape')
  await expect(editor).toBeHidden()
  await page.close()
})

// ---------------- 键盘三键回归与公式校验（票 07） ----------------

test('TC-WM2F-16 | 键盘三键回归：Enter 提交下移 / Tab 提交右移 / Esc 取消', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  const titleField = fieldIds['标题']
  const target = cell(page, recordId, titleField)

  // Enter 提交
  await openCellEditor(page, recordId, titleField)
  let editor = page.getByTestId(`editor-${recordId}-${titleField}`)
  await editor.locator('input').first().fill(`回车${SUF}`)
  await editor.locator('input').first().press('Enter')
  await expect(editor).toBeHidden({ timeout: 10000 })
  await expect(target).toContainText(`回车${SUF}`)

  // Tab 提交
  await openCellEditor(page, recordId, titleField)
  editor = page.getByTestId(`editor-${recordId}-${titleField}`)
  await editor.locator('input').first().fill(`Tab${SUF}`)
  await editor.locator('input').first().press('Tab')
  await expect(editor).toBeHidden({ timeout: 10000 })
  await expect(target).toContainText(`Tab${SUF}`)

  // Esc 取消不落盘（既有 p0 TC-050-03 的 not.toContainText 断言存在乐观渲染竞态——
  // 此处先等编辑器收起再断言单元格，并加服务端读校验：取消值绝不落库）
  await openCellEditor(page, recordId, titleField)
  editor = page.getByTestId(`editor-${recordId}-${titleField}`)
  await editor.locator('input').first().fill(`取消${SUF}`)
  await page.keyboard.press('Escape')
  await expect(editor).toBeHidden({ timeout: 10000 })
  await expect(target).not.toContainText(`取消${SUF}`)
  await expect(target).toContainText(`Tab${SUF}`)
  const storedAfterEsc = await readCellDecoded(ownerCtx.request, recordId, titleField, 1)
  expect(storedAfterEsc, 'Esc 取消的草稿不得写入服务端').toBe(`Tab${SUF}`)
  await page.close()
})

test('TC-WM2F-17 | 创建未知函数（CONCAT）公式字段被拒绝且行内提示可见（票 07）', async () => {
  const page = await ownerCtx.newPage()
  await gotoBase(page)
  await page.getByRole('button', { name: '新增字段' }).click()
  const dialog = page.getByRole('dialog', { name: '新增字段' })
  await expect(dialog).toBeVisible({ timeout: 10000 })
  await dialog.getByLabel('字段类型').selectOption('formula')
  await dialog.getByLabel('字段名称').fill(`坏公式${SUF}`)
  await dialog.getByLabel('公式表达式').fill(`CONCAT({标题}, '-x')`)
  await dialog.getByRole('button', { name: '创建' }).click()

  // 行内提示（对话框内 alert，非全局横幅），文案含「未知函数: CONCAT」
  const alert = dialog.getByRole('alert')
  await expect(alert).toBeVisible({ timeout: 15000 })
  await expect(alert).toContainText('未知函数: CONCAT')
  // 对话框保持打开（拒绝创建），字段未落库
  await expect(dialog).toBeVisible()
  await expect(page.getByRole('columnheader', { name: `字段：坏公式${SUF}` })).toHaveCount(0)
  await dialog.getByRole('button', { name: '取消' }).click()
  await expect(dialog).toBeHidden({ timeout: 10000 })
  await page.close()
})
