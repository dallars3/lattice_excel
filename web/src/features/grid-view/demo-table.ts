// 演示数据面（票 09）：演示列配置（字段类型/列宽/类型配置）+ 确定性假记录工厂。
// 本模块零 MSW 依赖：src/mocks/browser.ts（dev worker）与组件测试共用同一数据面，
// 保证 dev 演示与 RTL 用例走完全一致的 cursor/limit/has_more 语义（mockCursorRecords 切片）。
// 术语沿用 GLOSSARY.md：列 = 字段（Field），行 = 记录（Record），单元格 = Cell。

/** 演示记录查询路由（mockCursorRecords 按 H-48 语义切片：data.record_list/has_more/next_cursor）。 */
export const DEMO_RECORDS_PATH = '/api/v1/bases/b_demo/records'
/** 演示视图 id：query-keys recordsKeys.list 的域参数。 */
export const DEMO_VIEW_ID = 'vw_demo'
/** 游标页大小：接口文档 8.0.2 默认 limit=20。 */
export const DEMO_PAGE_SIZE = 20
/** 演示记录总数（3 页游标，足够演示滚动追页 + LRU 计数）。 */
export const DEMO_ROW_TOTAL = 60

/** 演示记录行（MockCursorRow 的业务子集：id + cells_map；索引签名对齐 mock 工厂入参形状）。 */
export interface DemoRecordRow {
  readonly id: string
  readonly cells_map?: Record<string, unknown>
  [key: string]: unknown
}

/** select 演示选项：色值经 CSS 变量注入（渲染器只消费配置色，不写死颜色）。 */
export interface DemoSelectOption {
  readonly id: string
  readonly label: string
  readonly color?: string
}

/** 演示列（字段）：key 取值键 / fieldType 注册表类型键 / width 布局列宽 / config 字段类型配置。 */
export interface DemoColumn {
  readonly key: string
  readonly name: string
  readonly fieldType: 'text' | 'number' | 'select'
  readonly width: number
  readonly config: unknown
}

export const DEMO_STATUS_OPTIONS: readonly DemoSelectOption[] = [
  { id: 'st_todo', label: '待处理', color: 'var(--presence-color-5)' },
  { id: 'st_doing', label: '进行中', color: 'var(--presence-color-1)' },
  { id: 'st_done', label: '已完成', color: 'var(--presence-color-3)' },
]

export const DEMO_PRIORITY_OPTIONS: readonly DemoSelectOption[] = [
  { id: 'pr_high', label: '高', color: 'var(--presence-color-7)' },
  { id: 'pr_medium', label: '中' },
  { id: 'pr_low', label: '低' },
]

/** 首列为主字段（GLOSSARY：不可删除/隐藏/换位；演示列序同构）。 */
export const DEMO_COLUMNS: readonly DemoColumn[] = [
  { key: 'name', name: '名称', fieldType: 'text', width: 200, config: {} },
  { key: 'qty', name: '数量', fieldType: 'number', width: 140, config: { precision: 0, thousands: true } },
  { key: 'amount', name: '金额', fieldType: 'number', width: 140, config: { precision: 2, thousands: true } },
  { key: 'status', name: '状态', fieldType: 'select', width: 140, config: { options: DEMO_STATUS_OPTIONS } },
  { key: 'note', name: '备注', fieldType: 'text', width: 140, config: {} },
  { key: 'extra', name: '补充说明', fieldType: 'text', width: 140, config: {} },
  { key: 'score', name: '评分', fieldType: 'number', width: 140, config: { precision: 1 } },
  { key: 'priority', name: '优先级', fieldType: 'select', width: 140, config: { options: DEMO_PRIORITY_OPTIONS } },
  { key: 'tag', name: '标签', fieldType: 'text', width: 140, config: {} },
  { key: 'sort', name: '排序', fieldType: 'number', width: 140, config: { precision: 0, thousands: true } },
]

/** 确定性假记录工厂：dev worker 与组件测试共用（同值可断言）。 */
export function buildDemoRows(count: number): DemoRecordRow[] {
  return Array.from({ length: count }, (_, zeroBased) => {
    const sequence = zeroBased + 1
    return {
      id: `rec_${String(sequence).padStart(4, '0')}`,
      cells_map: {
        name: `记录 ${sequence}`,
        qty: sequence * 125,
        amount: sequence * 1000 + 0.25,
        status: DEMO_STATUS_OPTIONS[sequence % 3]?.id,
        note: `备注内容 ${sequence}`,
        extra: `补充说明 ${sequence}`,
        score: Number(((sequence % 10) * 0.55).toFixed(2)),
        priority: DEMO_PRIORITY_OPTIONS[(sequence + 1) % 3]?.id,
        tag: `标签 ${sequence}`,
        sort: sequence * 100,
      },
    }
  })
}
