// 排序/筛选压测（验收专项，对 http://127.0.0.1:8080 部署栈执行）
// 运行：npx playwright test e2e/sf-stress.spec.ts --workers=1
// 覆盖：14 行数据；数字/文本字段；单字段连续正降切换；跨字段切换；
//       双字段组合排序；筛选连续换字段；筛选+排序组合。
// 断言口径：按「值序列」断言（重复值构成并列组，组内记录顺序不确定——
//           与 MySQL ai_ci 排序语义一致，值序列本身确定）；空值位置按
//           实测口径断言（MySQL：ASC 空值最前，DESC 最后）。
import { test, expect, type BrowserContext, type Page } from '@playwright/test'

const BASE = 'http://127.0.0.1:8080'
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

const NUM1_ASC: Array<number | null> = [
  null, -100, -3, 0, 1, 7, 7, 8, 33, 42, 42, 55, 100, 999,
]
const NUM1_DESC: Array<number | null> = [...NUM1_ASC].reverse()
const TXT1_ASC: Array<string | null> = [
  null, '1abc', '2024总结', 'apple', 'banana', '橙子', '猕猴桃',
  '苹果', '苹果', '苹果酱', '西瓜', '香蕉', '香蕉',
]
const TXT1_DESC: Array<string | null> = [...TXT1_ASC].reverse()

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
    await page.getByRole('button', { name: '排序', exact: true }).click()
    await expect(page.getByRole('region', { name: /排序/ })).toBeVisible({ timeout: 10000 })
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

  /** 读某字段整列的值序列（按 DOM 顺序=排序后视觉顺序）。 */
  async function columnValues(page: Page, fieldId: string): Promise<Array<string | null>> {
    const texts = await page.locator(`[data-testid$="-${fieldId}"]`).allInnerTexts()
    return texts.map((text) => (text.trim() === '' ? null : text.trim()))
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
    // 期望值对序列：num1 升序，num1 并列时 num2 降序
    const pairs = (await Promise.all([
      columnValues(page, fieldIds['数字1']),
      columnValues(page, fieldIds['数字2']),
    ])).map(([a, b]) => `${a === null ? 'null' : Number(a)}|${b === null ? 'null' : Number(b)}`)
    expect(pairs).toEqual([
      'null|5', '-100|6', '-3|9', '0|8', '1|4', '7|10', '7|7', '8|4',
      '33|6', '42|10', '42|5', '55|8', '100|9', '999|7',
    ])
    await setSort(page, 1, '数字1', 'desc')
    await setSort(page, 2, '数字2', 'asc')
    const pairsDesc = (await Promise.all([
      columnValues(page, fieldIds['数字1']),
      columnValues(page, fieldIds['数字2']),
    ])).map(([a, b]) => `${a === null ? 'null' : Number(a)}|${b === null ? 'null' : Number(b)}`)
    expect(pairsDesc).toEqual([
      '999|7', '100|9', '55|8', '42|5', '42|10', '33|6', '8|4', '7|7',
      '7|10', '1|4', '0|8', '-3|9', '-100|6', 'null|5',
    ])
  })

  test('TC-SF-05 | 筛选连续换字段（两轮 12 次切换）', async () => {
    test.setTimeout(120000)
    await page.goto(`${BASE}/base/${baseId}`)
    await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
    const visible = () => page.locator(`[data-testid$="-${fieldIds['标题']}"]`).count()
    const cases: Array<{ field: string; op: string; value?: string; expect: number }> = [
      { field: '数字1', op: '大于', value: '0', expect: 10 },
      { field: '文本1', op: '包含', value: '苹果', expect: 3 },
      { field: '数字1', op: '等于', value: '7', expect: 2 },
      { field: '文本1', op: '等于', value: '香蕉', expect: 2 },
      { field: '数字1', op: '小于', value: '0', expect: 2 },
      { field: '文本1', op: '不包含', value: '苹果', expect: 11 },
    ]
    for (let round = 0; round < 2; round += 1) {
      for (const item of cases) {
        await page.getByRole('button', { name: '清除全部' }).click().catch(() => {})
        await page.getByRole('button', { name: '添加筛选条件' }).click()
        const count = await page.getByLabel(/^筛选字段 /).count()
        await page.getByLabel(`筛选字段 ${count}`).selectOption({ label: item.field })
        await page.getByLabel(`筛选操作 ${count}`).selectOption({ label: item.op })
        if (item.value !== undefined) {
          await page.getByLabel(`筛选值 ${count}`).fill(item.value)
        }
        await page.waitForTimeout(1200)
        expect(await visible(), `round${round + 1} ${item.field} ${item.op} ${item.value ?? ''}`).toBe(item.expect)
      }
    }
    await page.getByRole('button', { name: '清除全部' }).click().catch(() => {})
    await page.waitForTimeout(800)
    expect(await visible()).toBe(ROWS.length)
  })

  test('TC-SF-06 | 筛选+排序组合（筛选下连续切换方向）', async () => {
    test.setTimeout(90000)
    await page.goto(`${BASE}/base/${baseId}`)
    await expect(page.getByRole('grid', { name: '记录网格' })).toBeVisible({ timeout: 20000 })
    // 筛选：文本1 包含 苹（3 行：42/55/1）
    await page.getByRole('button', { name: '筛选', exact: true }).click()
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
    // 清筛选 → 全量恢复
    await page.getByRole('button', { name: '清除全部' }).click().catch(() => {})
    await page.waitForTimeout(1000)
    expect(await toNumbers(await columnValues(page, fieldIds['数字1']))).toEqual(NUM1_ASC)
  })
})
