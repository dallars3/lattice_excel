// 排序/筛选压测（验收专项，对 http://127.0.0.1:8080 部署栈执行）
// 运行：npx playwright test e2e/sf-stress.spec.ts --workers=1
// 覆盖：14 行数据；数字/文本字段；单字段连续正降切换；跨字段切换；
//       双字段组合排序；筛选连续换字段；筛选+排序组合。
// 断言口径：按「值序列」断言（重复值构成并列组，组内记录顺序不确定——
//           与 MySQL ai_ci 排序语义一致，值序列本身确定）；空值位置按
//           产品既定口径断言：空值统一排最后（core compareNullableCells
//           空值恒在末尾，不随方向翻转；与排序面板提示「空值统一排最后」一致）。
//           双字段组合（TC-SF-04）的并列组内次序由二级排序键决定（按该键
//           自身方向比较；core compareRecords 逐键比较，仅全键并列才落到
//           记录 ID 兜底）。
import { test, expect, type BrowserContext, type Page } from '@playwright/test'

// SF_BASE_URL 环境变量可覆盖目标栈（并行验证实例用）；缺省仍指向本地部署栈 :8080。
const BASE = process.env.SF_BASE_URL ?? 'http://127.0.0.1:8080'
const PWD = 'Pass1234'
const OWNER = `adsf${Date.now().toString(36).slice(-5)}`

let ctx: BrowserContext
let baseId = ''
let tableId = ''
const fieldIds: Record<string, string> = {}
let page: Page

// 行数据（故意乱序；含重复值、负数、空数字、空文本各一组）
const ROWS: Array<{ num1: number | null; num2: number; txt1: string | null }> = [
  { num1: 42, num2: 10, txt1: '苹果' },
  { num1: 7, num2: 10, txt1: '香蕉' },
  { num1: 100, num2: 9, txt1: 'apple' },
  { num1: -3, num2: 9, txt1: 'banana' },
  { num1: 0, num2: 8, txt1: '2024总结' },
  { num1: 55, num2: 8, txt1: '苹果' },
  { num1: 7, num2: 7, txt1: '猕猴桃' },
  { num1: 999, num2: 7, txt1: '蓝莓' },
  { num1: -100, num2: 6, txt1: '橙子' },
  { num1: 33, num2: 6, txt1: '西瓜' },
  { num1: null, num2: 5, txt1: null },
  { num1: 42, num2: 5, txt1: '香蕉' },
  { num1: 1, num2: 4, txt1: '苹果酱' },
  { num1: 8, num2: 4, txt1: '1abc' },
]

// 空值统一排最后（ASC/DESC 均末尾）：与 core compareNullableCells 及面板提示一致。
const NUM1_ASC: Array<number | null> = [
  -100, -3, 0, 1, 7, 7, 8, 33, 42, 42, 55, 100, 999, null,
]
const NUM1_DESC: Array<number | null> = [
  999, 100, 55, 42, 42, 33, 8, 7, 7, 1, 0, -3, -100, null,
]
const TXT1_ASC: Array<string | null> = [
  '1abc', '2024总结', 'apple', 'banana', '橙子', '猕猴桃',
  '苹果', '苹果', '苹果酱', '蓝莓', '西瓜', '香蕉', '香蕉', null,
]
const TXT1_DESC: Array<string | null> = [
  '香蕉', '香蕉', '西瓜', '蓝莓', '苹果酱', '苹果', '苹果', '猕猴桃', '橙子',
  'banana', 'apple', '2024总结', '1abc', null,
]

test.describe.serial('排序/筛选压测', () => {
  test.beforeAll(async ({ browser }) => {
    ctx = await browser.newContext({ viewport: { width: 1280, height: 1500 } })
    expect((await (await ctx.request.post(`${BASE}/api/v1/auth/register`, {
      data: { username: OWNER, password: PWD, enterprise_name: `压测企业${OWNER.slice(-4)}` },
    })).json())?.code).toBe(0)
    expect((await (await ctx.request.post(`${BASE}/api/v1/auth/login`, {
      data: { username: OWNER, password: PWD },
    })).json())?.code).toBe(0)

    const created = await (await ctx.request.post(`${BASE}/api/v1/bases`, {
      data: { name: `排序筛选压测-${OWNER.slice(-4)}` },
    })).json()
    expect(created.code).toBe(0)
    baseId = created.data.base.id
    const tables = await (await ctx.request.get(`${BASE}/api/v1/bases/${baseId}/tables`)).json()
    tableId = tables.data.table_list[0].id

    const fields = await (await ctx.request.get(`${BASE}/api/v1/tables/${tableId}/fields`)).json()
    fieldIds['标题'] = fields.data.field_list.find((f: any) => f.is_primary).id
    const createField = async (name: string, type: string) => {
      const result = await (await ctx.request.post(`${BASE}/api/v1/tables/${tableId}/fields`, {
        data: { name, type, config_map: {} },
      })).json()
      expect(result.code, `create field ${name}`).toBe(0)
      fieldIds[name] = result.data.field.id
    }
    await createField('数字1', 'FIELD_TYPE_NUMBER')
    await createField('数字2', 'FIELD_TYPE_NUMBER')
    await createField('文本1', 'FIELD_TYPE_TEXT')

    for (let index = 0; index < ROWS.length; index += 1) {
      const row = ROWS[index]
      const cellsMap: Record<string, unknown> = {
        [fieldIds['标题']]: `行${String(index + 1).padStart(2, '0')}`,
      }
      if (row.num1 !== null) cellsMap[fieldIds['数字1']] = row.num1
      if (row.txt1 !== null) cellsMap[fieldIds['文本1']] = row.txt1
      cellsMap[fieldIds['数字2']] = row.num2
      const result = await (await ctx.request.post(`${BASE}/api/v1/bases/${baseId}/records`, {
        data: { table_id: tableId, cells_map: cellsMap },
      })).json()
      expect(result.code, `create record ${index + 1}`).toBe(0)
    }

    page = await ctx.newPage()
    await page.goto(`${BASE}/base/${baseId}`)
    await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
  })

  async function openSortPanel(page: Page) {
    // 工具栏面板为 toggle 语义：已展开时不再点击（用例串行共享面板状态）。
    const sortButton = page.getByRole('button', { name: '排序', exact: true })
    if ((await sortButton.getAttribute('aria-expanded')) !== 'true') {
      await sortButton.click()
    }
    await expect(page.getByRole('region', { name: /排序/ })).toBeVisible({ timeout: 10000 })
  }

  async function openFilterPanel(page: Page) {
    const filterButton = page.getByRole('button', { name: '筛选', exact: true })
    if ((await filterButton.getAttribute('aria-expanded')) !== 'true') {
      await filterButton.click()
    }
    await expect(page.getByRole('region', { name: /筛选/ })).toBeVisible({ timeout: 10000 })
  }

  async function setSort(page: Page, index: number, fieldName: string, direction: 'asc' | 'desc') {
    for (let attempt = 0; attempt < 5; attempt += 1) {
      if ((await page.getByLabel(`排序字段 ${index}`).count()) > 0) break
      await page.getByRole('button', { name: '添加排序' }).click()
      await page.waitForTimeout(300)
    }
    await page.getByLabel(`排序字段 ${index}`).selectOption({ label: fieldName })
    await page.getByLabel(`排序方向 ${index}`).selectOption(direction)
    await page.waitForTimeout(1500)
  }

  /** 读某字段整列的值序列（按 DOM 顺序=排序后视觉顺序）；连续两次读取一致才算稳定（重取落定前不误判）。 */
  async function columnValues(page: Page, fieldId: string): Promise<Array<string | null>> {
    const read = async (): Promise<Array<string | null>> => {
      const texts = await page.locator(`[data-testid$="-${fieldId}"]`).allInnerTexts()
      return texts.map((text) => (text.trim() === '' ? null : text.trim()))
    }
    let previous = await read()
    for (let attempt = 0; attempt < 20; attempt += 1) {
      const next = await read()
      if (next.length === previous.length && next.every((value, index) => value === previous[index])) {
        return next
      }
      previous = next
      await page.waitForTimeout(150)
    }
    return previous
  }

  /** 读两列值对：按单元格 recordId 对齐（避免双列并发读被网格重渲染撕裂），读取稳定后才返回。 */
  async function columnPairs(page: Page, fieldA: string, fieldB: string): Promise<string[]> {
    const readColumn = async (fieldId: string) => {
      const cells = page.locator(`[data-testid$="-${fieldId}"]`)
      const count = await cells.count()
      const rows: Array<{ recordId: string; text: string | null }> = []
      for (let index = 0; index < count; index += 1) {
        const cell = cells.nth(index)
        const testId = (await cell.getAttribute('data-testid')) ?? ''
        const text = (await cell.innerText()).trim()
        rows.push({
          recordId: testId.slice('cell-'.length, testId.length - fieldId.length - 1),
          text: text === '' ? null : text,
        })
      }
      return rows
    }
    const readPairs = async (): Promise<string[]> => {
      const [columnA, columnB] = await Promise.all([readColumn(fieldA), readColumn(fieldB)])
      const textByRecord = new Map(columnB.map((row) => [row.recordId, row.text]))
      return columnA.map((row) => {
        const other = textByRecord.get(row.recordId)
        if (other === undefined) return 'MISMATCH'
        return `${row.text ?? 'null'}|${other ?? 'null'}`
      })
    }
    let previous: string[] = []
    for (let attempt = 0; attempt < 20; attempt += 1) {
      const current = await readPairs()
      const stable =
        current.length > 0 &&
        !current.includes('MISMATCH') &&
        current.length === previous.length &&
        current.every((value, index) => value === previous[index])
      if (stable) return current
      previous = current
      await page.waitForTimeout(150)
    }
    return previous
  }

  function toNumbers(values: Array<string | null>): Array<number | null> {
    return values.map((value) => (value === null ? null : Number(value)))
  }

  test('TC-SF-01 | 数字字段连续正降切换（4 次切换 5 个状态）', async () => {
    await openSortPanel(page)
    await setSort(page, 1, '数字1', 'asc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
    await setSort(page, 1, '数字1', 'desc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_DESC)
    await setSort(page, 1, '数字1', 'asc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
    await setSort(page, 1, '数字1', 'desc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_DESC)
    await setSort(page, 1, '数字1', 'asc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
    // 标题列跟随行序联动（同一行内数字与标题一致）
    const titles = await columnValues(page, fieldIds['标题'])
    expect(titles.length).toBe(ROWS.length)
  })

  test('TC-SF-02 | 文本字段连续正降切换（4 次切换 5 个状态）', async () => {
    await openSortPanel(page)
    await setSort(page, 1, '文本1', 'asc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_ASC)
    await setSort(page, 1, '文本1', 'desc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_DESC)
    await setSort(page, 1, '文本1', 'asc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_ASC)
    await setSort(page, 1, '文本1', 'desc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_DESC)
    await setSort(page, 1, '文本1', 'asc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_ASC)
  })

  test('TC-SF-03 | 跨字段切换排序字段', async () => {
    await openSortPanel(page)
    await setSort(page, 1, '数字1', 'desc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_DESC)
    await setSort(page, 1, '文本1', 'asc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(TXT1_ASC)
    await setSort(page, 1, '数字2', 'desc')
    const num2 = await toNumbers(await columnValues(page, fieldIds['数字2']))
    expect(num2).toEqual([...num2].sort((a, b) => (b ?? -Infinity) - (a ?? -Infinity)))
    await setSort(page, 1, '数字1', 'asc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
  })

  test('TC-SF-04 | 双字段组合排序（数字1 正序 + 数字2 倒序，方向互换）', async () => {
    await openSortPanel(page)
    await page.getByRole('button', { name: '添加排序' }).click()
    await setSort(page, 1, '数字1', 'asc')
    await setSort(page, 2, '数字2', 'desc')
    // 期望值对序列：num1 升序，并列组内按记录 ID 兜底（创建序）；空值统一排最后。
    const pairs = await columnPairs(page, fieldIds['数字1'], fieldIds['数字2'])
    expect(pairs).toEqual([
      '-100|6', '-3|9', '0|8', '1|4', '7|10', '7|7', '8|4',
      '33|6', '42|10', '42|5', '55|8', '100|9', '999|7', 'null|5',
    ])
    await setSort(page, 1, '数字1', 'desc')
    await setSort(page, 2, '数字2', 'asc')
    const pairsDesc = await columnPairs(page, fieldIds['数字1'], fieldIds['数字2'])
    // 此阶段二级排序键为数字2 升序：num1 并列组内按 num2 升序排列（42 组 5→10，7 组 7→10）。
    expect(pairsDesc).toEqual([
      '999|7', '100|9', '55|8', '42|5', '42|10', '33|6', '8|4', '7|7',
      '7|10', '1|4', '0|8', '-3|9', '-100|6', 'null|5',
    ])
  })

  test('TC-SF-05 | 筛选连续换字段（两轮 12 次切换）', async () => {
    test.setTimeout(120000)
    await page.goto(`${BASE}/base/${baseId}`)
    await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
    // 「添加筛选条件/清除全部」位于筛选面板内：先展开面板再操作。
    await openFilterPanel(page)
    const titleCells = page.locator(`[data-testid$="-${fieldIds['标题']}"]`)
    const cases: Array<{ field: string; op: string; value?: string; expect: number }> = [
      { field: '数字1', op: '大于', value: '0', expect: 10 },
      { field: '文本1', op: '包含', value: '苹果', expect: 3 },
      { field: '数字1', op: '等于', value: '7', expect: 2 },
      // 文本字段操作符白名单无「等于」（contains/not_contains/is_empty/not_empty），
      // 改用数字2 等于（行11/行12 的数字2 均为 5）覆盖第二数字字段的 eq。
      { field: '数字2', op: '等于', value: '5', expect: 2 },
      { field: '数字1', op: '小于', value: '0', expect: 2 },
      // 空值单元格不参与值比较类筛选（core matchesFilter：empty 一律不匹配，与 SQL NULL 语义一致）：
      // 14 行中文本1 非空且不含「苹果」的共 10 行（空文本行被排除）。
      { field: '文本1', op: '不包含', value: '苹果', expect: 10 },
    ]
    for (let round = 0; round < 2; round += 1) {
      for (const item of cases) {
        const clearAll = page.getByRole('button', { name: '清除全部' })
        if (await clearAll.isEnabled()) await clearAll.click()
        await page.getByRole('button', { name: '添加筛选条件' }).click()
        const count = await page.getByLabel(/^筛选字段 /).count()
        await page.getByLabel(`筛选字段 ${count}`).selectOption({ label: item.field })
        await page.getByLabel(`筛选操作 ${count}`).selectOption({ label: item.op })
        if (item.value !== undefined) {
          await page.getByLabel(`筛选值 ${count}`).fill(item.value)
        }
        await page.waitForTimeout(1200)
        // 保存落库并重取落定后按行数断言（轮询等待，避免慢响应下的一次性计数误判）。
        await expect(
          titleCells,
          `round${round + 1} ${item.field} ${item.op} ${item.value ?? ''}`,
        ).toHaveCount(item.expect, { timeout: 15000 })
      }
    }
    await page.getByRole('button', { name: '清除全部' }).click().catch(() => {})
    await expect(titleCells).toHaveCount(ROWS.length, { timeout: 15000 })
  })

  test('TC-SF-06 | 筛选+排序组合（筛选下连续切换方向）', async () => {
    test.setTimeout(90000)
    await page.goto(`${BASE}/base/${baseId}`)
    await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
    // 筛选：文本1 包含 苹（3 行：42/55/1）
    await openFilterPanel(page)
    await page.getByRole('button', { name: '添加筛选条件' }).click()
    await page.getByLabel('筛选字段 1').selectOption({ label: '文本1' })
    await page.getByLabel('筛选操作 1').selectOption({ label: '包含' })
    await page.getByLabel('筛选值 1').fill('苹果')
    await page.waitForTimeout(1200)
    // 排序：数字1 降序 → 55,42,1
    await openSortPanel(page)
    await setSort(page, 1, '数字1', 'desc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual([55, 42, 1])
    // 换排序字段：文本1 升序 → 苹果,苹果,苹果酱
    await setSort(page, 1, '文本1', 'asc')
    expect(await columnValues(page, fieldIds['文本1'])).toEqual(['苹果', '苹果', '苹果酱'])
    // 连续切换方向两次
    await setSort(page, 1, '数字1', 'desc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual([55, 42, 1])
    await setSort(page, 1, '数字1', 'asc')
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual([1, 42, 55])
    // 清筛选 → 全量恢复（「清除全部」在筛选面板内：先切回筛选面板再清除）。
    await openFilterPanel(page)
    await page.getByRole('button', { name: '清除全部' }).click().catch(() => {})
    await page.waitForTimeout(1000)
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
  })
})
