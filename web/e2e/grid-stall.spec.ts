// 网格停滞专项（票 06）：对独立 live stack 运行真实浏览器/API 压力序列。
// 运行：$env:SF_BASE_URL='http://127.0.0.1:8091'; node node_modules/playwright/cli.js test
//      e2e/grid-stall.spec.ts --workers=1
// 失败时 Playwright 保留 trace；附加的 grid-stall-diagnostics.json 会给出请求阶段分类。
import {
  expect,
  test,
  type APIRequestContext,
  type BrowserContext,
  type Page,
  type StorageState,
} from '@playwright/test'

import {
  GridStallProbe,
  groupLabelsFromOrderedValues,
  singleSelectCells,
  stringCells,
  stringListCells,
  type ExpectedGridState,
  type ViewConfigExpectation,
} from './support/grid-stall-diagnostics'

const BASE = process.env.SF_BASE_URL ?? 'http://127.0.0.1:8091'
const PWD = 'Pass1234'
const RUN = Date.now().toString(36).slice(-7)
const OWNER = `gst${RUN}`
const WAIT_MS = Number(process.env.GRID_STALL_WAIT_MS ?? '20000')
const FAULT = process.env.GRID_STALL_FAULT ?? ''

const EMPTY_RAW_CONFIG = {
  hidden_field_list: [],
  frozen_field_list: [],
  filter_list: [],
  sort_list: [],
  group_by_list: [],
  row_height: 'ROW_HEIGHT_MEDIUM',
  field_width_map: {},
}

const ROWS: Array<{
  num1: number | null
  num2: number
  txt1: string | null
  status: 'todo' | 'done' | null
  tags: Array<'red' | 'blue'>
}> = [
  { num1: 42, num2: 10, txt1: '苹果', status: 'todo', tags: ['red'] },
  { num1: 7, num2: 10, txt1: '香蕉', status: 'done', tags: ['blue'] },
  { num1: 100, num2: 9, txt1: 'apple', status: 'todo', tags: ['red', 'blue'] },
  { num1: -3, num2: 9, txt1: 'banana', status: 'done', tags: [] },
  { num1: 0, num2: 8, txt1: '2024总结', status: 'todo', tags: ['blue'] },
  { num1: 55, num2: 8, txt1: '苹果', status: 'done', tags: ['red'] },
  { num1: 7, num2: 7, txt1: '猕猴桃', status: 'todo', tags: [] },
  { num1: 999, num2: 7, txt1: '蓝莓', status: 'done', tags: ['red', 'blue'] },
  { num1: -100, num2: 6, txt1: '橙子', status: 'todo', tags: ['blue'] },
  { num1: 33, num2: 6, txt1: '西瓜', status: 'done', tags: ['red'] },
  { num1: null, num2: 5, txt1: null, status: null, tags: [] },
  { num1: 42, num2: 5, txt1: '香蕉', status: 'done', tags: ['blue'] },
  { num1: 1, num2: 4, txt1: '苹果酱', status: null, tags: ['red'] },
  { num1: 8, num2: 4, txt1: '1abc', status: 'done', tags: ['red', 'blue'] },
]

const TITLES = ROWS.map((_, index) => `压测行${String(index + 1).padStart(2, '0')}`)

let baseId = ''
let tableId = ''
let viewId = ''
let storageState: StorageState
const fieldIds: Record<string, string> = {}
let statusLabels: Map<string, string>
let tagLabels: Map<string, string>
let statusOptionIds: Record<'todo' | 'done', string>

let context: BrowserContext | undefined
let page: Page | undefined
let probe: GridStallProbe | undefined
let writeOutcome: unknown

// 场景局部失败保留 trace；默认 :8091 防止误打共享 :8080/:8090。
test.use({ trace: 'retain-on-failure' })

test.describe.serial('网格停滞 trace 与重复稳定性', () => {
  test.setTimeout(90_000)

  test.beforeAll(async ({ browser }) => {
    const setup = await browser.newContext({ viewport: { width: 1280, height: 1500 } })
    const registered = await envelope(
      setup.request,
      'POST',
      '/api/v1/auth/register',
      {
        username: OWNER,
        password: PWD,
        enterprise_name: `网格停滞企业${RUN.slice(-4)}`,
      },
    )
    expect(registered.code, 'register').toBe(0)
    expect(
      (await envelope(setup.request, 'POST', '/api/v1/auth/login', {
        username: OWNER,
        password: PWD,
      })).code,
      'login',
    ).toBe(0)

    const created = await envelope(setup.request, 'POST', '/api/v1/bases', {
      name: `网格停滞专项-${RUN}`,
    })
    expect(created.code, 'create base').toBe(0)
    baseId = created.data.base.id
    const tables = await envelope(setup.request, 'GET', `/api/v1/bases/${baseId}/tables`)
    tableId = tables.data.table_list[0].id
    const fields = await envelope(setup.request, 'GET', `/api/v1/tables/${tableId}/fields`)
    const primary = fields.data.field_list.find((field: any) => field.is_primary)
    fieldIds['标题'] = primary.id

    const createField = async (name: string, type: string, configMap: Record<string, unknown> = {}) =>
      envelope(setup.request, 'POST', `/api/v1/tables/${tableId}/fields`, {
        name,
        type,
        config_map: configMap,
      })
    const number1 = await createField('数字1', 'number')
    fieldIds['数字1'] = number1.data.field.id
    const number2 = await createField('数字2', 'number')
    fieldIds['数字2'] = number2.data.field.id
    const text1 = await createField('文本1', 'text')
    fieldIds['文本1'] = text1.data.field.id
    const status = await createField('状态', 'select', {
      option_list: [
        { name: '待办', color: 'blue' },
        { name: '完成', color: 'green' },
      ],
      allow_create: false,
    })
    fieldIds['状态'] = status.data.field.id
    const tags = await createField('标签', 'multiselect', {
      option_list: [
        { name: '红', color: 'red' },
        { name: '蓝', color: 'blue' },
      ],
      allow_create: false,
    })
    fieldIds['标签'] = tags.data.field.id

    const statusOptions = status.data.field.config_map.option_list as Array<{
      id: string
      name: string
    }>
    const tagOptions = tags.data.field.config_map.option_list as Array<{
      id: string
      name: string
    }>
    const optionId = (list: typeof statusOptions, name: string): string =>
      list.find((option) => option.name === name)?.id ?? ''
    statusLabels = new Map(
      statusOptions.map((option) => [option.id, option.name] as const),
    )
    tagLabels = new Map(tagOptions.map((option) => [option.id, option.name] as const))
    statusOptionIds = {
      todo: optionId(statusOptions, '待办'),
      done: optionId(statusOptions, '完成'),
    }

    for (let index = 0; index < ROWS.length; index += 1) {
      const row = ROWS[index]
      const cellsMap: Record<string, unknown> = {
        [fieldIds['标题']]: TITLES[index],
        [fieldIds['数字2']]: row.num2,
      }
      if (row.num1 !== null) cellsMap[fieldIds['数字1']] = row.num1
      if (row.txt1 !== null) cellsMap[fieldIds['文本1']] = row.txt1
      if (row.status !== null) {
        cellsMap[fieldIds['状态']] = optionId(
          statusOptions,
          row.status === 'todo' ? '待办' : '完成',
        )
      }
      if (row.tags.length > 0) {
        cellsMap[fieldIds['标签']] = row.tags.map((tag) =>
          optionId(tagOptions, tag === 'red' ? '红' : '蓝'),
        )
      }
      const result = await envelope(setup.request, 'POST', `/api/v1/bases/${baseId}/records`, {
        table_id: tableId,
        cells_map: cellsMap,
      })
      expect(result.code, `create record ${index + 1}`).toBe(0)
    }

    const views = await envelope(setup.request, 'GET', `/api/v1/tables/${tableId}/views`)
    viewId = views.data.view_list[0].id
    storageState = await setup.storageState()
    await setup.close()
  })

  test.beforeEach(async ({ browser }) => {
    writeOutcome = undefined
    context = await browser.newContext({
      storageState,
      viewport: { width: 1280, height: 1500 },
    })
    page = await context.newPage()
    await resetViewConfig(context.request)
    probe = new GridStallProbe(page, {
      baseId,
      tableId,
      viewId,
      primaryFieldId: fieldIds['标题'],
    })
    await page.goto(`${BASE}/base/${baseId}`)
    const grid = page.getByRole('grid', { name: '记录网格' })
    await expect(grid).toBeVisible({ timeout: WAIT_MS })
    await expect(grid).toHaveAttribute('data-record-window-state', 'ready', {
      timeout: WAIT_MS,
    })
    await installStallFault(page)
  })

  test.afterEach(async () => {
    await context?.close()
    context = undefined
    page = undefined
    probe = undefined
  })

  trackedScenario('TC-GST-01 | 连续排序切换最终匹配当前确认 ViewConfig', async () => {
    const currentPage = page
    assertPage(currentPage)
    await openPanel(currentPage, '排序')
    await currentPage.getByRole('button', { name: '添加排序' }).click()
    await expect(currentPage.getByLabel('排序字段 1')).toBeVisible({ timeout: WAIT_MS })

    const expected: ViewConfigExpectation = {
      filters: [],
      sorts: [{ fieldId: fieldIds['数字1'], dir: 'desc' }],
      group: null,
    }
    probe!.beginPhase('continuous-sort', expected)
    await currentPage.getByLabel('排序字段 1').selectOption(fieldIds['数字1'])
    await currentPage.getByLabel('排序方向 1').selectOption('asc')
    await currentPage.getByLabel('排序字段 1').selectOption(fieldIds['文本1'])
    await currentPage.getByLabel('排序方向 1').selectOption('desc')
    await currentPage.getByLabel('排序字段 1').selectOption(fieldIds['数字1'])
    await currentPage.getByLabel('排序方向 1').selectOption('desc')

    await assertConfirmedGrid(currentPage, probe!, expected)
  })

  trackedScenario('TC-GST-02 | 连续筛选切换最终匹配当前确认 ViewConfig', async () => {
    const currentPage = page
    assertPage(currentPage)
    await openPanel(currentPage, '筛选')
    await currentPage.getByRole('button', { name: '添加筛选条件' }).click()
    const fieldSelect = currentPage.getByLabel('筛选字段 1')
    const opSelect = currentPage.getByLabel('筛选操作 1')
    const valueInput = currentPage.getByLabel('筛选值 1')
    await expect(fieldSelect).toBeVisible({ timeout: WAIT_MS })

    const expected: ViewConfigExpectation = {
      filters: [{ fieldId: fieldIds['文本1'], op: 'contains', value: '苹果' }],
      sorts: [],
      group: null,
    }
    probe!.beginPhase('continuous-filter', expected)
    await fieldSelect.selectOption(fieldIds['数字1'])
    await opSelect.selectOption('gt')
    await valueInput.fill('0')
    await fieldSelect.selectOption(fieldIds['文本1'])
    await valueInput.fill('apple')
    await valueInput.fill('苹果')

    await assertConfirmedGrid(currentPage, probe!, expected)
  })

  trackedScenario('TC-GST-03 | 连续分组切换最终匹配当前确认 ViewConfig', async () => {
    const currentPage = page
    assertPage(currentPage)
    await openPanel(currentPage, '分组')
    const groupSelect = currentPage.getByLabel('分组字段')
    await expect(groupSelect).toBeVisible({ timeout: WAIT_MS })

    const expected: ViewConfigExpectation = {
      filters: [],
      sorts: [],
      group: fieldIds['状态'],
    }
    probe!.beginPhase('continuous-group', expected)
    await groupSelect.selectOption(fieldIds['状态'])
    await groupSelect.selectOption(fieldIds['标签'])
    await groupSelect.selectOption(fieldIds['状态'])

    await assertConfirmedGrid(currentPage, probe!, expected)
  })

  trackedScenario('TC-GST-04 | 写后连续浏览最终匹配当前确认 ViewConfig', async () => {
    const currentPage = page
    assertPage(currentPage)
    const title = `压测写后${RUN.slice(-4)}`
    await currentPage.getByRole('button', { name: '+ 新增记录' }).click()
    const editor = currentPage.locator('[data-testid^="editor-"] input').first()
    await expect(editor).toBeVisible({ timeout: WAIT_MS })
    await editor.fill(title)
    await editor.press('Enter')

    await expect.poll(async () => {
      const result = await envelope(
        context!.request,
        'GET',
        `/api/v1/bases/${baseId}/records?table_id=${encodeURIComponent(tableId)}&view_id=${encodeURIComponent(viewId)}&limit=100`,
      )
      return (result.data.record_list ?? []).length
    }, {
      message: '写入后的记录未落库',
      timeout: WAIT_MS,
      intervals: [100, 250, 500],
    }).toBe(ROWS.length + 1)

    await openPanel(currentPage, '排序')
    await currentPage.getByRole('button', { name: '添加排序' }).click()
    await expect(currentPage.getByLabel('排序字段 1')).toBeVisible({ timeout: WAIT_MS })

    const expected: ViewConfigExpectation = {
      filters: [{ fieldId: fieldIds['标题'], op: 'contains', value: '压测' }],
      sorts: [{ fieldId: fieldIds['数字1'], dir: 'asc' }],
      group: fieldIds['状态'],
    }
    probe!.beginPhase('write-then-browse', expected)
    await currentPage.getByLabel('排序字段 1').selectOption(fieldIds['数字1'])
    await currentPage.getByLabel('排序方向 1').selectOption('asc')
    await openPanel(currentPage, '筛选')
    await currentPage.getByRole('button', { name: '添加筛选条件' }).click()
    await currentPage.getByLabel('筛选字段 1').selectOption(fieldIds['标题'])
    await currentPage.getByLabel('筛选操作 1').selectOption('contains')
    await currentPage.getByLabel('筛选值 1').fill('压测')
    await openPanel(currentPage, '分组')
    await currentPage.getByLabel('分组字段').selectOption(fieldIds['状态'])

    writeOutcome = {
      title,
      expectedRecordCount: ROWS.length + 1,
      postObservedByProbe: true,
    }
    await assertConfirmedGrid(currentPage, probe!, expected, title)
    await expect(
      currentPage
        .locator(`[data-testid$="-${fieldIds['标题']}"]`)
        .filter({ hasText: title }),
    ).toHaveCount(1)
  })

  trackedScenario('TC-GST-05 | 当前确认 ViewConfig 下无 120 秒级停滞', async () => {
    const currentPage = page
    assertPage(currentPage)
    await openPanel(currentPage, '排序')
    await currentPage.getByRole('button', { name: '添加排序' }).click()
    await expect(currentPage.getByLabel('排序字段 1')).toBeVisible({ timeout: WAIT_MS })
    const expected: ViewConfigExpectation = {
      filters: [],
      sorts: [{ fieldId: fieldIds['数字1'], dir: 'asc' }],
      group: null,
    }
    probe!.beginPhase('stall-budget', expected)
    await currentPage.getByLabel('排序字段 1').selectOption(fieldIds['数字1'])
    await currentPage.getByLabel('排序方向 1').selectOption('asc')
    await assertConfirmedGrid(currentPage, probe!, expected)
  })
})

function assertPage(value: Page | undefined): asserts value is Page {
  expect(value, 'page').toBeDefined()
}

function trackedScenario(title: string, run: () => Promise<void>): void {
  test(title, async ({}, testInfo) => {
    try {
      await run()
    } catch (failure) {
      await probe?.attachOnFailure(testInfo, writeOutcome, true)
      throw failure
    }
  })
}

async function envelope(
  request: APIRequestContext,
  method: 'GET' | 'POST' | 'PUT',
  path: string,
  data?: unknown,
): Promise<any> {
  const response = await request.fetch(`${BASE}${path}`, {
    method,
    data,
  })
  const body = await response.json()
  expect(response.ok(), `${method} ${path}: HTTP ${response.status()} ${JSON.stringify(body)}`).toBe(true)
  return body
}

async function resetViewConfig(request: APIRequestContext): Promise<void> {
  const result = await envelope(request, 'PUT', `/api/v1/views/${viewId}/config`, {
    view_config: EMPTY_RAW_CONFIG,
  })
  expect(result.code, 'reset ViewConfig').toBe(0)
}

async function openPanel(page: Page, name: '排序' | '筛选' | '分组'): Promise<void> {
  const button = page.getByRole('button', { name, exact: true })
  if ((await button.getAttribute('aria-expanded')) !== 'true') {
    await button.click()
  }
  await expect(page.getByRole('region', { name: `${name}面板` })).toBeVisible({
    timeout: WAIT_MS,
  })
}

async function assertConfirmedGrid(
  page: Page,
  probe: GridStallProbe,
  expectedConfig: ViewConfigExpectation,
  extraTitle?: string,
): Promise<void> {
  // 在网络阶段失败前也保留一份独立的预期数据，确保 H-47/H-48 未完成附件仍包含
  // expected/current visible data，而不是只有空壳请求时序。
  probe.setExpectedGrid(offlineExpectedGrid(expectedConfig, extraTitle))
  await expect.poll(() => probe.latestSaveMatches(expectedConfig), {
    message: 'H-47 未确认当前 ViewConfig',
    timeout: WAIT_MS,
    intervals: [100, 250, 500],
  }).toBe(true)

  const currentRaw = await envelope(
    context!.request,
    'GET',
    `/api/v1/views/${viewId}/config`,
  )
  const currentSignature = signatureFromRaw(currentRaw.data.view_config)
  expect(currentSignature, 'H-46 回读应与当前确认 ViewConfig 一致').toBe(
    expectedSignature(expectedConfig),
  )

  const records = await fetchCurrentRecords()
  const titles = stringCells(records, fieldIds['标题']).map((value) => value ?? '')
  const expectedState: ExpectedGridState = {
    recordTitles: titles,
    groupLabels:
      expectedConfig.group === null || expectedConfig.group === undefined
        ? []
        : expectedConfig.group === fieldIds['状态']
          ? groupLabelsFromOrderedValues(
              singleSelectCells(records, fieldIds['状态'], statusLabels),
            )
          : groupLabelsFromOrderedValues(
              stringListCells(records, fieldIds['标签'], tagLabels),
            ),
  }
  probe.setExpectedGrid(expectedState)

  await expect.poll(async () => {
    const grid = page.getByRole('grid', { name: '记录网格' })
    const state = await grid.getAttribute('data-record-window-state')
    const applying = await page.getByTestId('view-conditions-applying').count()
    const titleValues = (
      await page
        .locator(`[data-testid^="cell-"][data-testid$="-${fieldIds['标题']}"]`)
        .allInnerTexts()
    ).map((value) => value.trim())
    const groupLabels =
      expectedState.groupLabels.length === 0
        ? []
        : await page.locator('[data-group-key] button').evaluateAll((buttons) =>
            buttons.map((button) =>
              (button.getAttribute('aria-label') ?? '').replace(/^分组：/, ''),
            ),
          )
    return { state, applying, titleValues, groupLabels }
  }, {
    message: '最终网格未匹配当前确认 ViewConfig',
    timeout: WAIT_MS,
    intervals: [100, 250, 500],
  }).toEqual({
    state: 'ready',
    applying: 0,
    titleValues: expectedState.recordTitles,
    // 虚拟滚动允许只渲染完整组序列中的连续片段；不允许出现中间草稿分组、
    // 重复组头或逆序组头。记录标题仍保持整窗精确相等。
    groupLabels: expect.any(Array),
  })
  const currentGroupLabels = await page.locator('[data-group-key] button').evaluateAll((buttons) =>
    buttons.map((button) =>
      (button.getAttribute('aria-label') ?? '').replace(/^分组：/, ''),
    ),
  )
  expect(
    isContiguousGroupSlice(currentGroupLabels, expectedState.groupLabels),
    `组头窗口 ${JSON.stringify(currentGroupLabels)} 必须是 ${JSON.stringify(expectedState.groupLabels)} 的连续片段`,
  ).toBe(true)
}

async function fetchCurrentRecords(): Promise<Array<Record<string, unknown>>> {
  const result = await envelope(
    context!.request,
    'GET',
    `/api/v1/bases/${baseId}/records?table_id=${encodeURIComponent(tableId)}&view_id=${encodeURIComponent(viewId)}&limit=100`,
  )
  expect(result.code, 'H-48 baseline').toBe(0)
  return (result.data.record_list as Array<{ cells_map?: Record<string, unknown> }>).map(
    (record) =>
      Object.fromEntries(
        Object.entries(record.cells_map ?? {}).map(([fieldId, value]) => [
          fieldId,
          decodeValue(value),
        ]),
      ),
  )
}

function signatureFromRaw(raw: unknown): string {
  if (typeof raw !== 'object' || raw === null) return ''
  const config = raw as Record<string, unknown>
  const filters = Array.isArray(config.filter_list)
    ? config.filter_list.map((entry) => {
        const filter = entry as Record<string, unknown>
        return [
          String(filter.field_id ?? ''),
          String(filter.op ?? '').replace(/^VIEW_FILTER_OP_/, '').toLowerCase(),
          decodeValue(filter.value),
        ]
      })
    : []
  const sorts = Array.isArray(config.sort_list)
    ? config.sort_list.map((entry) => {
        const sort = entry as Record<string, unknown>
        return [
          String(sort.field_id ?? ''),
          String(sort.dir ?? '').replace(/^SORT_DIR_/, '').toLowerCase(),
        ]
      })
    : []
  const group =
    Array.isArray(config.group_by_list)
    && typeof config.group_by_list[0] === 'object'
    && config.group_by_list[0] !== null
      ? [String((config.group_by_list[0] as Record<string, unknown>).field_id ?? ''), null]
      : null
  if (filters.length === 0 && sorts.length === 0 && group === null) return ''
  return JSON.stringify({ filters, sorts, group })
}

function expectedSignature(expected: ViewConfigExpectation): string {
  const filters = expected.filters ?? []
  const sorts = expected.sorts ?? []
  const group = expected.group ?? null
  if (filters.length === 0 && sorts.length === 0 && group === null) return ''
  return JSON.stringify({
    filters: filters.map((filter) => [filter.fieldId, filter.op, filter.value]),
    sorts: sorts.map((sort) => [sort.fieldId, sort.dir]),
    group: group === null ? null : [group, null],
  })
}

function decodeValue(raw: unknown): unknown {
  if (typeof raw !== 'object' || raw === null) return null
  const value = raw as Record<string, unknown>
  if ('string_value' in value) return value.string_value
  if ('int_value' in value) return Number(value.int_value)
  if ('double_value' in value) return value.double_value
  if ('bool_value' in value) return value.bool_value
  if (
    typeof value.list_value === 'object'
    && value.list_value !== null
    && Array.isArray((value.list_value as Record<string, unknown>).value_list)
  ) {
    return ((value.list_value as Record<string, unknown>).value_list as unknown[]).map(decodeValue)
  }
  return null
}

function isContiguousGroupSlice(actual: string[], expected: string[]): boolean {
  if (expected.length === 0) return actual.length === 0
  if (actual.length === 0) return true
  const start = expected.indexOf(actual[0])
  return start >= 0 && actual.every((label, index) => expected[start + index] === label)
}

function offlineExpectedGrid(
  config: ViewConfigExpectation,
  extraTitle?: string,
): ExpectedGridState {
  const rows = [
    ...ROWS.map((row, index) => ({ ...row, title: TITLES[index], index })),
    ...(extraTitle === undefined
      ? []
      : [{ num1: null, num2: null, txt1: null, status: null, tags: [], title: extraTitle, index: ROWS.length }]),
  ]
  const filtered = rows.filter((row) =>
    (config.filters ?? []).every((filter) => {
      const text = filter.fieldId === fieldIds['标题'] ? row.title : row.txt1 ?? ''
      if (filter.op === 'contains') return String(text).includes(String(filter.value))
      return true
    }),
  )
  const statusOrder = [statusOptionIds.todo, statusOptionIds.done].sort((left, right) =>
    left.localeCompare(right),
  )
  const result = [...filtered].sort((left, right) => {
    if (config.group === fieldIds['状态']) {
      const leftRank =
        left.status === null ? 2 : statusOrder.indexOf(statusOptionIds[left.status])
      const rightRank =
        right.status === null ? 2 : statusOrder.indexOf(statusOptionIds[right.status])
      if (leftRank !== rightRank) return leftRank - rightRank
    }
    for (const sort of config.sorts ?? []) {
      if (sort.fieldId !== fieldIds['数字1']) continue
      if (left.num1 === null && right.num1 === null) continue
      if (left.num1 === null) return 1
      if (right.num1 === null) return -1
      const comparison = left.num1 - right.num1
      if (comparison !== 0) return sort.dir === 'asc' ? comparison : -comparison
    }
    return left.index - right.index
  })
  const statusValues = result.map((row) =>
    row.status === null ? null : statusLabels.get(statusOptionIds[row.status]) ?? row.status,
  )
  return {
    recordTitles: result.map((row) => row.title),
    groupLabels:
      config.group === fieldIds['状态']
        ? groupLabelsFromOrderedValues(statusValues)
        : [],
  }
}

async function installStallFault(page: Page): Promise<void> {
  if (FAULT === '') return
  if (FAULT === 'H47_HOLD' || FAULT === 'H48_HOLD' || FAULT === 'H48_STALE_RESPONSE') {
    await page.route('**/api/v1/**', async (route) => {
      const request = route.request()
      const url = new URL(request.url())
      const kind =
        request.method() === 'PUT' && url.pathname.endsWith('/config')
          ? 'H47'
          : request.method() === 'GET' && url.pathname.endsWith('/records')
            ? 'H48'
            : ''
      if (kind === '' || (FAULT.startsWith('H47') && kind !== 'H47') || (FAULT.startsWith('H48') && kind !== 'H48')) {
        await route.continue()
        return
      }
      const response = await route.fetch()
      if (FAULT.endsWith('_HOLD')) {
        await new Promise<void>(() => undefined)
      }
      if (FAULT === 'H48_STALE_RESPONSE') {
        const body = await response.json()
        body.data = { record_list: [], has_more: false, next_cursor: null }
        await route.fulfill({ response, json: body })
        return
      }
      await route.fulfill({ response })
    })
  }
}
