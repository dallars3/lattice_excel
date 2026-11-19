// acceptance-defect-remediation 批次 E2E 验收专项（票 05，最终验收门）：对 http://127.0.0.1:8080 live stack 执行。
// 覆盖三个缺陷修复的验收主线（前置票 01 测试库隔离护栏为服务侧保障，不在 E2E 断言范围）：
//   TC-AD-01（N7/票02）：空企业名注册 → 默认建企 → 完整首页（新建入口与聚合区）→ auth/me 有企业 ID → 能建表建字段
//   TC-AD-02（N5/票03）：含存量记录的表 → 后加公式字段 → 存量行公式列出现计算值（轮询等待回填）
//   TC-AD-03（N2/票04）：owner-only 企业人员候选含所有者 → 选择后值写入并持久化
// 运行：npx playwright test e2e/ad-fixes.spec.ts --workers=1 --reporter=list
// 可用 AD05_BASE=http://localhost:5199 覆盖目标（dev harness：vite dev + /api 代理到 8080）。
// 限速红线：注册 10/min、登录 20/min（IP 滑窗）——整轮仅 1 个账号（API 注册 + API 登录 + UI 登录各一次），
// 前缀 ad05 独立；「空企业名注册」必须走 API 注册（不带 enterprise_name）命中后端默认建企分支——
// 注册页 UI 对 create 路径恒上送派生默认名（前端历史兜底，早于票02），UI 无法触达后端默认分支。
// 状态经 tmpdir 文件跨 worker 保留（用例失败触发 worker 重建后可续跑），各建数据步骤带幂等守卫
// （表/记录/字段已存在则跳过），可对本地栈重复执行（共享 dev 库不假设为空，断言不依赖库空）。
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

import { test, expect, type APIRequestContext, type BrowserContext, type Page } from '@playwright/test'

const BASE = process.env.AD05_BASE ?? 'http://127.0.0.1:8080'
const SUF = Date.now().toString(36).slice(-6)
const PWD = 'Pass1234'
const OWNER = `ad05own${SUF}`
// 字段/表名不随运行后缀（每次运行新建独立 Base 内唯一）；状态文件保证 worker 重建后引用一致。
const T_FORMULA = '公式回填'
const T_PERSON = '负责人'
const STOCK_TABLE = '存量表'
const FORMULA_PREFIX = 'AD5-'
const STOCK_RECORD_TARGET = 3
const PERSON_RECORD_TITLE = `人员记录-${SUF}`

// ---- 跨 worker 状态（worker 重建时 beforeAll 重跑；经文件恢复既有 Base/表/记录引用） ----
const STATE_FILE = path.join(os.tmpdir(), 'ad05-e2e-state.json')
const STATE_TTL_MS = 20 * 60 * 1000

interface RunState {
  createdAt: number
  owner: string
  ownerUserId: string
  baseId: string
  homeTableId: string
  stockTableId: string
  stockRecords: Array<{ id: string; title: string }>
  personRecordId: string
}

let ownerCtx: BrowserContext
let ownerUserId = ''
// 本轮实际使用的账号：全新初始化=本次注册账号；状态恢复=原账号（模块 OWNER 后缀随进程
// 变化，恢复后必须沿用原账号登录/断言，否则 UI 登录落到从未注册的账号上）。
let ownerName = ''
let baseId = ''
// homeTableId：Base 默认表（TC-AD-03 人员字段所在表）；stockTableId：存量表（TC-AD-02 公式回填所在表）。
let homeTableId = ''
let stockTableId = ''
let stockRecords: Array<{ id: string; title: string }> = []
let personRecordId = ''
// 状态文件归属的账号（写盘判定用）。
let stateOwner = ''

function loadState(): RunState | null {
  try {
    const raw = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8')) as RunState
    if (typeof raw.createdAt !== 'number' || Date.now() - raw.createdAt > STATE_TTL_MS) return null
    if (typeof raw.owner !== 'string' || raw.ownerUserId === '' || raw.baseId === '') return null
    if (raw.homeTableId === '' || raw.stockTableId === '' || raw.personRecordId === '') return null
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
    ownerUserId,
    baseId,
    homeTableId,
    stockTableId,
    stockRecords,
    personRecordId,
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
async function registerWithRetry(ctx: APIRequestContext, username: string): Promise<any> {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const res = await api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD })
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 15000))
  }
  return api(ctx, 'POST', '/api/v1/auth/register', { username, password: PWD })
}

/** 登录限速 20/min：命中 5003 时等待滑窗后重试。 */
async function loginWithRetry(ctx: APIRequestContext, username: string): Promise<any> {
  for (let attempt = 0; attempt < 4; attempt += 1) {
    const res = await api(ctx, 'POST', '/api/v1/auth/login', { username, password: PWD })
    if (res?.code !== 5003) return res
    await new Promise((resolve) => setTimeout(resolve, 12000))
  }
  return api(ctx, 'POST', '/api/v1/auth/login', { username, password: PWD })
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
  // 结构对象：逐键解码
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

async function gotoTable(page: Page, tableId: string) {
  await page.goto(`${BASE}/base/${baseId}?table=${tableId}`)
  await waitGrid(page)
}

function cell(page: Page, recordId: string, fieldId: string) {
  return page.getByTestId(`cell-${recordId}-${fieldId}`)
}

/** 单击选中 + F2 进入编辑：避免双击因重渲染偶发不触发编辑态（与 m2-fixes/p1p2 同口径）。 */
async function openCellEditor(page: Page, recordId: string, fieldId: string) {
  const target = cell(page, recordId, fieldId)
  await target.click()
  await target.press('F2')
  await expect(page.getByTestId(`editor-${recordId}-${fieldId}`)).toBeVisible({ timeout: 10000 })
}

/** 字段 id 按名称解析；tries=1 供幂等探测（不等待）。 */
async function fieldIdByName(ctx: APIRequestContext, tableId: string, name: string, tries = 10): Promise<string | null> {
  for (let attempt = 0; attempt < tries; attempt += 1) {
    const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
    const found = (res?.data?.field_list ?? []).find((field: any) => field.name === name)
    if (found) return found.id as string
    if (tries === 1) return null
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  return null
}

async function primaryFieldId(ctx: APIRequestContext, tableId: string): Promise<string> {
  const res = await api(ctx, 'GET', `/api/v1/tables/${tableId}/fields`)
  const primary = (res?.data?.field_list ?? []).find((field: any) => field.is_primary)
  expect(primary, `表 ${tableId} 应有主字段`).toBeTruthy()
  return primary.id as string
}

/** 新增字段对话框流：选类型、命名、创建，断言列头出现（与 m2-fixes 同口径）。 */
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

/** 幂等建字段：状态复用/重跑时字段已存在则跳过 UI 创建，返回字段 id。 */
async function ensureFieldViaUi(page: Page, tableId: string, type: string, name: string, expression?: string) {
  const existing = await fieldIdByName(ownerCtx.request, tableId, name, 1)
  if (existing !== null) return existing
  await createFieldViaUi(page, type, name, expression)
  const created = await fieldIdByName(ownerCtx.request, tableId, name)
  expect(created, `字段 ${name} 创建后应可回读`).toBeTruthy()
  return created as string
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

/** 幂等建存量表（TC-AD-02）：状态复用按 id 校验，缺失时按名称收养或新建。 */
async function ensureStockTable(): Promise<void> {
  const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/tables`)
  expect(tables?.code, JSON.stringify(tables)).toBe(0)
  const list = tables.data.table_list ?? []
  if (stockTableId !== '' && list.some((table: any) => table.id === stockTableId)) return
  const byName = list.find((table: any) => table.name === STOCK_TABLE)
  if (byName) {
    stockTableId = byName.id as string
    return
  }
  const created = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/tables`, { name: STOCK_TABLE })
  expect(created?.code, JSON.stringify(created)).toBe(0)
  stockTableId = created.data.table.id
}

/** 幂等补存量记录（TC-AD-02 前置）：先于公式字段创建——N5 回填语义的触发前提。
 * 标题跟随本轮 SUF 生成（恢复路径沿用状态内既有记录，不重复创建）。 */
async function ensureStockRecords(): Promise<void> {
  const readable = await Promise.all(stockRecords.map(async (item) => recordReadable(ownerCtx.request, item.id)))
  stockRecords = stockRecords.filter((_, index) => readable[index])
  const primaryId = await primaryFieldId(ownerCtx.request, stockTableId)
  let index = 1
  while (stockRecords.length < STOCK_RECORD_TARGET) {
    const title = `存量${index}-${SUF}`
    index += 1
    if (stockRecords.some((item) => item.title === title)) continue
    const created = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
      table_id: stockTableId,
      cells_map: { [primaryId]: title },
    })
    expect(created?.code, JSON.stringify(created)).toBe(0)
    stockRecords.push({ id: created.data.record.id as string, title })
  }
  saveState()
}

/** 幂等补人员编辑目标记录（TC-AD-03）。 */
async function ensurePersonRecord(): Promise<void> {
  if (personRecordId !== '' && (await recordReadable(ownerCtx.request, personRecordId))) return
  const primaryId = await primaryFieldId(ownerCtx.request, homeTableId)
  const created = await api(ownerCtx.request, 'POST', `/api/v1/bases/${baseId}/records`, {
    table_id: homeTableId,
    cells_map: { [primaryId]: PERSON_RECORD_TITLE },
  })
  expect(created?.code, JSON.stringify(created)).toBe(0)
  personRecordId = created.data.record.id
  saveState()
}

test.describe.configure({ timeout: 90000 })

test.beforeAll(async ({ browser }) => {
  test.setTimeout(180000)
  ownerCtx = await browser.newContext()

  const state = loadState()
  let restored = false
  if (state !== null) {
    // worker 重建/快速重跑恢复：登录既有账号并校验 Base/默认表仍可达。
    const login = await loginWithRetry(ownerCtx.request, state.owner)
    if (login?.code === 0) {
      const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${state.baseId}/tables`)
      const list = tables?.data?.table_list ?? []
      if (tables?.code === 0 && list.some((table: any) => table.id === state.homeTableId)) {
        stateOwner = state.owner
        ownerName = state.owner
        ownerUserId = state.ownerUserId
        baseId = state.baseId
        homeTableId = state.homeTableId
        stockTableId = state.stockTableId
        stockRecords = state.stockRecords ?? []
        personRecordId = state.personRecordId
        restored = true
        console.log(`[ad05] 状态恢复：owner=${ownerName} base=${baseId} tables=${homeTableId}/${stockTableId} records=${stockRecords.length}`)
      } else {
        console.log(`[ad05] 状态校验失败（tables=${tables?.code}），走全新初始化`)
      }
    } else {
      console.log(`[ad05] 状态账号登录失败（code=${login?.code}），走全新初始化`)
    }
  }

  if (!restored) {
    // N7 主线：API 注册且不带 enterprise_name → 后端默认分支应自动建企（注册页 UI 恒上送
    // 派生默认名，触达不了该分支，故注册只能走 API；UI 登录在 TC-AD-01 内完成）。
    const registered = await registerWithRetry(ownerCtx.request, OWNER)
    expect(registered?.code, JSON.stringify(registered)).toBe(0)
    expect(registered?.data?.user?.enterprise_id ?? '', '注册响应应已携带默认企业 ID').not.toBe('')
    stateOwner = OWNER
    ownerName = OWNER
    const login = await loginWithRetry(ownerCtx.request, OWNER)
    expect(login?.code, JSON.stringify(login)).toBe(0)

    const me = await api(ownerCtx.request, 'GET', '/api/v1/auth/me')
    expect(me?.code, JSON.stringify(me)).toBe(0)
    ownerUserId = me.data.user.id

    const created = await api(ownerCtx.request, 'POST', '/api/v1/bases', { name: `AD05沙盒-${SUF}` })
    expect(created?.code, JSON.stringify(created)).toBe(0)
    baseId = created.data.base.id
    const tables = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/tables`)
    homeTableId = tables.data.table_list[0].id
    saveState()
  }

  // 共享守卫步骤（恢复路径同样执行：缺失即补建，存在则跳过）。
  await ensureStockTable()
  await ensureStockRecords()
  await ensurePersonRecord()
})

test.afterAll(async () => {
  await ownerCtx?.close()
})

// ---------------- N7（票 02）：空企业名注册 → 默认建企 → 完整首页 → 能建表建字段 ----------------

test('TC-AD-01 | 空企业名注册（N7）：默认建企 → 完整首页 → 能建表建字段', async ({ browser }) => {
  // 服务端：会话主体携带企业 ID，注册者为企业所有者（修复前 enterprise_id/my_role 为空，
  // 首页按外部协作者视图降级——只剩「与我共享」区、没有新建入口）。
  const me = await api(ownerCtx.request, 'GET', '/api/v1/auth/me')
  expect(me?.code, JSON.stringify(me)).toBe(0)
  expect(me.data.user.enterprise_id ?? '', 'auth/me 应有企业 ID').not.toBe('')
  expect(me.data.enterprise?.id ?? '', 'auth/me 应有企业聚合对象').toBe(me.data.user.enterprise_id)
  expect(me.data.enterprise?.owner_id ?? '', '默认企业所有者应为注册用户').toBe(ownerUserId)
  expect(String(me.data.my_role ?? ''), '注册者角色应为所有者').toBe('owner')

  // UI：登录 → 首页含新建入口与四聚合区。
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(`${BASE}/login`)
  await page.getByLabel('用户名').fill(ownerName)
  await page.getByLabel('密码').fill(PWD)
  // 登录限速 20/min：整轮仅 2 次登录（API + UI），命中「操作过于频繁」时等待滑窗重试兜底。
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.waitForTimeout(1500)
    if (!page.url().includes('/login')) break
    const alert = page.locator('main').getByRole('alert').last()
    const text = await alert.innerText().catch(() => '')
    if (!text.includes('操作过于频繁')) break
    await page.waitForTimeout(12000)
  }
  await expect(page).toHaveURL(/\/home$/, { timeout: 20000 })
  await expect(page.getByRole('heading', { name: '协作多维表格' })).toBeVisible()
  await expect(page.getByRole('button', { name: '新建多维表格' })).toBeVisible({ timeout: 10000 })
  for (const section of ['星标', '最近访问', '我创建的', '与我共享']) {
    await expect(page.getByRole('heading', { name: section })).toBeVisible()
  }

  // 能建表：新建入口 → 进入网格 → 新建数据表落库可见。
  await page.getByRole('button', { name: '新建多维表格' }).click()
  await expect(page).toHaveURL(/\/base\//, { timeout: 20000 })
  await waitGrid(page)
  await page.getByRole('button', { name: '新建数据表' }).click()
  const tableDialog = page.getByRole('dialog', { name: '新建数据表' })
  await expect(tableDialog).toBeVisible({ timeout: 10000 })
  await tableDialog.getByLabel('数据表名称').fill(`验收表-${SUF}`)
  await tableDialog.getByRole('button', { name: '创建' }).click()
  await expect(page.locator('[role="tab"]').filter({ hasText: `验收表-${SUF}` })).toBeVisible({ timeout: 15000 })
  await expect(page.getByRole('columnheader').first()).toBeVisible({ timeout: 15000 })

  // 能建字段：新增字段对话框创建多行文本字段。
  await page.getByRole('button', { name: '新增字段' }).click()
  const fieldDialog = page.getByRole('dialog', { name: '新增字段' })
  await expect(fieldDialog).toBeVisible({ timeout: 10000 })
  await fieldDialog.getByLabel('字段类型').selectOption('text')
  await fieldDialog.getByLabel('字段名称').fill(`验收字段-${SUF}`)
  await fieldDialog.getByRole('button', { name: '创建' }).click()
  await expect(page.getByRole('columnheader', { name: `字段：验收字段-${SUF}` })).toBeVisible({ timeout: 15000 })
  await ctx.close()
})

// ---------------- N5（票 03）：含存量记录的表建公式字段 → 存量行公式列出现计算值 ----------------

test('TC-AD-02 | 存量记录公式回填（N5）：后加公式字段 → 存量行公式列出现计算值', async () => {
  // 前置（beforeAll 已就绪并落盘）：存量表含 3 条记录，创建先于公式字段——N5 回填语义的触发前提。
  expect(stockRecords.length, '存量记录应已就绪').toBeGreaterThanOrEqual(3)
  const page = await ownerCtx.newPage()
  await gotoTable(page, stockTableId)

  // 通过新增字段对话框（真实用户路径）在含存量记录的表上创建公式字段。
  const formulaFieldId = await ensureFieldViaUi(page, stockTableId, 'formula', T_FORMULA, `CONCATENATE('${FORMULA_PREFIX}', {标题})`)

  // 轮询单记录读取：等待 calc 消费者完成回填（N5 分批语义——存量行逐条物化，全部出现才算过）。
  const deadline = Date.now() + 60000
  let pending: string[] = []
  while (Date.now() < deadline) {
    pending = []
    for (const item of stockRecords) {
      const value = await readCellDecoded(ownerCtx.request, item.id, formulaFieldId, 1)
      if (value !== `${FORMULA_PREFIX}${item.title}`) pending.push(item.title)
    }
    if (pending.length === 0) break
    await page.waitForTimeout(2000)
  }
  expect(pending, `存量行公式列应在 60s 内全部回填（未回填：${pending.join(',')}）`).toEqual([])

  // 视图侧：刷新后网格单元格展示物化值（非「计算中…」/空白）。
  await gotoTable(page, stockTableId)
  for (const item of stockRecords) {
    await expect(cell(page, item.id, formulaFieldId)).toContainText(`${FORMULA_PREFIX}${item.title}`, { timeout: 20000 })
  }
  await page.close()
})

// ---------------- N2（票 04）：owner-only 企业人员候选含所有者 → 选择后写入并持久化 ----------------

test('TC-AD-03 | owner-only 企业人员候选含所有者（N2）：选择后值写入并持久化', async () => {
  // 目录端点直接断言：owner-only 企业（无成员、无外部协作者）→ 并集恰为所有者本身。
  const directory = await api(ownerCtx.request, 'GET', `/api/v1/bases/${baseId}/shares/person-directory`)
  expect(directory?.code, JSON.stringify(directory)).toBe(0)
  const entries = directory.data.collaborator_list ?? []
  const ownerEntry = entries.find((entry: any) => entry.user_id === ownerUserId)
  expect(ownerEntry, `人员目录应含所有者 ${ownerUserId}：${JSON.stringify(entries)}`).toBeTruthy()
  expect(ownerEntry.username, '所有者条目应携带用户名').toBe(ownerName)
  expect(ownerEntry.internal, '所有者应标记为企业内部成员').toBe(true)

  const page = await ownerCtx.newPage()
  await gotoTable(page, homeTableId)
  const personFieldId = await ensureFieldViaUi(page, homeTableId, 'person', T_PERSON)

  // 人员编辑器候选列表：owner-only 企业候选恰为所有者（修复前目录唯一数据源是外部协作者，
  // owner-only 企业候选恒为空，人员字段永远无法选值）。
  await openCellEditor(page, personRecordId, personFieldId)
  const panel = page.getByRole('listbox', { name: '人员候选' })
  await expect(panel).toBeVisible({ timeout: 10000 })
  const options = panel.getByRole('option')
  await expect(options, 'owner-only 企业候选应恰为所有者一人').toHaveCount(1)
  await expect(options.first()).toContainText(ownerName)
  await options.first().click()
  await expect(page.getByTestId(`editor-${personRecordId}-${personFieldId}`)).toBeHidden({ timeout: 10000 })

  // 单元格展示目录映射名（显示名缺省回落用户名），值已写入服务端（user_id 形态）。
  await expect(cell(page, personRecordId, personFieldId)).toContainText(ownerName, { timeout: 15000 })
  const stored = await readCellDecoded(ownerCtx.request, personRecordId, personFieldId)
  expect(stored, '人员值应以所有者 user_id 落库').toBe(ownerUserId)

  // 持久化：刷新后单元格仍显示所有者名称，服务端值不变。
  await gotoTable(page, homeTableId)
  await expect(cell(page, personRecordId, personFieldId)).toContainText(ownerName, { timeout: 20000 })
  const persisted = await readCellDecoded(ownerCtx.request, personRecordId, personFieldId, 1)
  expect(persisted, '刷新后服务端值应保持').toBe(ownerUserId)
  await page.close()
})
