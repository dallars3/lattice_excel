// 错误码目录（单一事实源）：13 个错误码 → 「文案 / 动作 / 恢复策略」三元组 + 全局/局部消费层标注。
// 事实源：《接口文档-v0.6.md》8.0.1（错误码与 HTTP 状态）+《前端-技术方案-v0.1.md》5.1（层级清单）。
// 消费方式：ApiError 携带 catalog 条目，组件从目录取文案与动作，不各自硬编码。

/** 接口文档 8.0.1 定义的 13 个业务错误码。 */
export type ErrorCode =
  | 1001
  | 1002
  | 2001
  | 2002
  | 3001
  | 3002
  | 4001
  | 4002
  | 5001
  | 5002
  | 5003
  | 9001
  | 9002

/**
 * 消费层（技术方案 5.1）：
 * - global：由全局层（App 级横幅/拦截器）统一处理，业务组件不重复兜底；
 * - local：由发生处的组件/页面就地消费（表单提示、冲突弹窗、列表空态等）。
 */
export type ErrorLayer = 'global' | 'local'

/**
 * 恢复策略及处理语义：
 * - relogin：会话失效，清会话后跳转登录页，登录成功返回原页（2001）。
 * - reinput：用户修正输入/凭据/文件后重发同一请求（1001/1002/2002/5002）。
 * - readonly-fallback：触发全局只读降级 + 提示（需求 3.8.4；3001/3002）。
 * - resolve-conflict：打开 4001 三动作裁决（用对方的值/仍用我的覆盖/复制我的内容）。
 * - refresh：失效相关缓存重拉最新数据，内容保留可复制（4002 记录/字段已被删除）。
 * - retry：退避后重试同一请求（5003/9001/9002）。
 * - none：阻断性错误，仅展示文案与动作，无可自动恢复路径（5001）。
 */
export type RecoveryStrategy =
  | 'relogin'
  | 'reinput'
  | 'readonly-fallback'
  | 'resolve-conflict'
  | 'refresh'
  | 'retry'
  | 'none'

/** 单个错误码的目录条目：文案/动作/恢复策略三元组 + 消费层 + 对应 HTTP 状态（便于拦截器按状态兜底）。 */
export interface CatalogEntry {
  /** number 而非 ErrorCode：兜底条目的码来自服务端未知码，不受 13 码联合约束。 */
  readonly code: number
  readonly httpStatus: number
  readonly layer: ErrorLayer
  /** 文案：发生了什么（面向用户）。 */
  readonly title: string
  /** 动作：用户下一步该做什么。 */
  readonly action: string
  readonly recovery: RecoveryStrategy
}

export const ERROR_CATALOG: Readonly<Record<ErrorCode, CatalogEntry>> = {
  1001: {
    code: 1001,
    httpStatus: 400,
    layer: 'local',
    title: '请求参数缺失或不合法',
    action: '请检查填写内容后重试',
    recovery: 'reinput',
  },
  1002: {
    code: 1002,
    httpStatus: 400,
    layer: 'local',
    title: '名称已被使用（用户名或对象重名）',
    action: '请换一个名称再试',
    recovery: 'reinput',
  },
  2001: {
    code: 2001,
    httpStatus: 401,
    layer: 'global',
    title: '登录状态已失效',
    action: '请重新登录后继续操作',
    recovery: 'relogin',
  },
  2002: {
    code: 2002,
    httpStatus: 401,
    layer: 'local',
    title: '用户名或密码错误，或账号已锁定',
    action: '请核对凭据；连续失败请稍后再试',
    recovery: 'reinput',
  },
  3001: {
    code: 3001,
    httpStatus: 403,
    layer: 'global',
    title: '当前身份无此操作权限',
    action: '页面已切换为只读，如需操作请联系多维表格管理员',
    recovery: 'readonly-fallback',
  },
  3002: {
    code: 3002,
    httpStatus: 403,
    layer: 'local',
    title: '外部协作者不可导出数据',
    action: '请联系企业管理员导出',
    recovery: 'readonly-fallback',
  },
  4001: {
    code: 4001,
    httpStatus: 409,
    layer: 'local',
    title: '内容已被他人修改',
    action: '请选择：采用对方的新值、保留我的修改，或复制我的内容',
    recovery: 'resolve-conflict',
  },
  4002: {
    code: 4002,
    httpStatus: 409,
    layer: 'local',
    title: '该记录或字段已被删除',
    action: '编辑内容已保留可复制，刷新后查看最新数据',
    recovery: 'refresh',
  },
  5001: {
    code: 5001,
    httpStatus: 413,
    layer: 'local',
    title: '已达容量上限',
    action: '请清理不需要的数据后重试，或联系管理员',
    recovery: 'none',
  },
  5002: {
    code: 5002,
    httpStatus: 413,
    layer: 'local',
    title: '文件不符合要求（类型受限或超出大小上限）',
    action: '请更换符合要求的文件后重试',
    recovery: 'reinput',
  },
  5003: {
    code: 5003,
    httpStatus: 429,
    layer: 'global',
    title: '操作过于频繁',
    action: '请稍候片刻再试',
    recovery: 'retry',
  },
  9001: {
    code: 9001,
    httpStatus: 500,
    layer: 'global',
    title: '服务暂时不可用',
    action: '请稍后重试；若持续出现请联系管理员',
    recovery: 'retry',
  },
  9002: {
    code: 9002,
    httpStatus: 500,
    layer: 'global',
    title: '保存冲突，请重试',
    action: '系统正在重试保存，请确认最新内容后继续',
    recovery: 'retry',
  },
}

/** 13 码清单（接口文档 8.0.1 顺序），供测试与遍历消费。 */
export const ALL_ERROR_CODES: readonly ErrorCode[] = [
  1001, 1002, 2001, 2002, 3001, 3002, 4001, 4002, 5001, 5002, 5003, 9001, 9002,
]

/** 未知码兜底：全局层提示，仅展示不可自动恢复（不猜测语义、不自动重试）。 */
const UNKNOWN_FALLBACK: CatalogEntry = {
  code: 0,
  httpStatus: 500,
  layer: 'global',
  title: '发生未知错误',
  action: '请稍后重试或联系管理员',
  recovery: 'none',
}

/** 按码查目录：已知码返回条目，未知码返回兜底（服务端新增码不致前端崩溃）。 */
export function lookupErrorCatalog(code: number): CatalogEntry {
  const entry = (ERROR_CATALOG as Readonly<Record<number, CatalogEntry | undefined>>)[code]
  return entry ?? UNKNOWN_FALLBACK
}
