// W 系列消息类型（票 05）：W-01~W-16 全部消息名与负载类型的唯一前端落点。
// 事实源：《接口文档-v0.6.md》8.2「WebSocket 消息结构（W-01 ~ W-16）」，逐一抄录、不新造；
// 线上判别字段为 t（8.6.8 示例）。seq / last_seq 为断线对账字段（技术方案 5.2）。

export type MessageDirection = 'c2s' | 's2c'

/** 消息编号（W-01 ~ W-16）与名称的描述项，顺序即接口文档 8.2 表顺序。 */
export interface WMessageDescriptor {
  readonly code: string
  readonly name: string
  readonly direction: MessageDirection
}

/** 全部 16 条 W 消息：可枚举断言的事实源（测试逐一对照接口文档 8.2）。 */
export const W_MESSAGES = [
  { code: 'W-01', name: 'join', direction: 'c2s' },
  { code: 'W-02', name: 'leave', direction: 'c2s' },
  { code: 'W-03', name: 'heartbeat', direction: 'c2s' },
  { code: 'W-04', name: 'cursor', direction: 'c2s' },
  { code: 'W-05', name: 'selection', direction: 'c2s' },
  { code: 'W-06', name: 'editing', direction: 'c2s' },
  { code: 'W-07', name: 'replay_request', direction: 'c2s' },
  { code: 'W-08', name: 'snapshot', direction: 's2c' },
  { code: 'W-09', name: 'cell_changed', direction: 's2c' },
  { code: 'W-10', name: 'record_deleted', direction: 's2c' },
  { code: 'W-11', name: 'structure_changed', direction: 's2c' },
  { code: 'W-12', name: 'presence', direction: 's2c' },
  { code: 'W-13', name: 'cursor_echo', direction: 's2c' },
  { code: 'W-14', name: 'recalc_status', direction: 's2c' },
  { code: 'W-15', name: 'session_revoked', direction: 's2c' },
  { code: 'W-16', name: 'ack', direction: 's2c' },
] as const satisfies readonly WMessageDescriptor[]

export type MessageCode = (typeof W_MESSAGES)[number]['code']
export type MessageName = (typeof W_MESSAGES)[number]['name']

/** C→S 消息名（W-01 ~ W-07）。 */
export const CLIENT_MESSAGE_NAMES = ['join', 'leave', 'heartbeat', 'cursor', 'selection', 'editing', 'replay_request'] as const

/** S→C 消息名（W-08 ~ W-16），同时是事件分发表的键集。 */
export const SERVER_MESSAGE_NAMES = [
  'snapshot',
  'cell_changed',
  'record_deleted',
  'structure_changed',
  'presence',
  'cursor_echo',
  'recalc_status',
  'session_revoked',
  'ack',
] as const

// ---------- C→S 负载（8.2 抄录） ----------

export interface JoinMessage {
  t: 'join'
  base_id: string
  table_id: string
  /** 本地 seq 高水位；首次接入无历史时缺省（对账字段）。 */
  last_seq?: number
}

export interface LeaveMessage {
  t: 'leave'
}

export interface HeartbeatMessage {
  t: 'heartbeat'
}

export interface CursorMessage {
  t: 'cursor'
  record_id?: string
  field_id?: string
  range?: CellRange
}

export interface SelectionMessage {
  t: 'selection'
  range: CellRange
}

export interface EditingMessage {
  t: 'editing'
  record_id: string
  field_id: string
  on: boolean
}

export interface ReplayRequestMessage {
  /** 断线重连后请求补发的对账字段：本地 seq 高水位。 */
  t: 'replay_request'
  last_seq: number
}

// ---------- S→C 负载（8.2 抄录） ----------

/** W-08/W-12 的成员项：presence 专属色由 tokens 提供 10 色（技术方案 11 章）。 */
export interface MemberInfo {
  user_id: string
  color: string
  online: boolean
  internal: boolean
}

export interface SnapshotMessage {
  t: 'snapshot'
  /** 房间当前 seq（对账字段，本地高水位的初始化来源）。 */
  seq: number
  member_list: MemberInfo[]
}

export interface CellChangedMessage {
  t: 'cell_changed'
  seq: number
  record_id: string
  field_id: string
  /** 单元格值：类型随字段类型变化，M1 抄契约透传，不收窄。 */
  value: CellValue
  version: number
  operator: string
}

export interface RecordDeletedMessage {
  t: 'record_deleted'
  seq: number
  record_id: string
}

export interface StructureChangedMessage {
  t: 'structure_changed'
  seq: number
  /** 变更类别（字段/表/视图变化）；接口文档未枚举取值，透传字符串。 */
  change_type: string
  table_id: string
  field_id?: string
}

export interface PresenceMessage {
  t: 'presence'
  user_id: string
  color: string
  online: boolean
  internal: boolean
}

export interface CursorEchoMessage {
  t: 'cursor_echo'
  user_id: string
  record_id?: string
  field_id?: string
  range?: CellRange
}

export interface RecalcStatusMessage {
  t: 'recalc_status'
  record_id: string
  field_id: string
  status: 'calc' | 'error'
}

export interface SessionRevokedMessage {
  t: 'session_revoked'
  reason: 'link_closed' | 'perm_changed' | 'logout'
}

export interface AckMessage {
  t: 'ack'
  msg_id: string
}

// ---------- 透传与联合类型 ----------

/**
 * W-04/W-05/W-13 的 range 载荷：接口文档 8.2 仅注明「单元格/区域」，
 * 内部结构未细化；M1 抄契约不新造字段，落为透明透传形状。
 */
export type CellRange = Record<string, unknown>

/** 单元格值（W-09 value）：随字段类型变化的任意 JSON 值，M1 透传。 */
export type CellValue = unknown

/** C→S 消息联合（按名取形）。 */
export interface ClientMessageMap {
  join: JoinMessage
  leave: LeaveMessage
  heartbeat: HeartbeatMessage
  cursor: CursorMessage
  selection: SelectionMessage
  editing: EditingMessage
  replay_request: ReplayRequestMessage
}

export type ClientMessageName = keyof ClientMessageMap
export type ClientMessage = ClientMessageMap[ClientMessageName]

/** S→C 消息联合（按名取形）：事件分发表的值类型来源。 */
export interface ServerMessageMap {
  snapshot: SnapshotMessage
  cell_changed: CellChangedMessage
  record_deleted: RecordDeletedMessage
  structure_changed: StructureChangedMessage
  presence: PresenceMessage
  cursor_echo: CursorEchoMessage
  recalc_status: RecalcStatusMessage
  session_revoked: SessionRevokedMessage
  ack: AckMessage
}

export type ServerMessageName = keyof ServerMessageMap
export type ServerMessage = ServerMessageMap[ServerMessageName]

const CLIENT_NAME_SET: ReadonlySet<string> = new Set(CLIENT_MESSAGE_NAMES)
const SERVER_NAME_SET: ReadonlySet<string> = new Set(SERVER_MESSAGE_NAMES)

/** 客户端消息序列化为线上 JSON（判别字段 t 随消息对象携带）。 */
export function encodeClientMessage(message: ClientMessage): string {
  return JSON.stringify(message)
}

/** 解析服务端原始帧为类型化消息；非法帧（非 JSON / 未知或方向不符的 t）返回 null，不抛异常。 */
export function parseServerMessage(raw: string): ServerMessage | null {
  const parsed = tryParseJson(raw)
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null
  }
  const name = (parsed as { t?: unknown }).t
  if (typeof name !== 'string' || !SERVER_NAME_SET.has(name)) {
    return null
  }
  return parsed as ServerMessage
}

/** 解析客户端原始帧为类型化消息（测试对账断言用）；非法帧返回 null，不抛异常。 */
export function parseClientMessage(raw: string): ClientMessage | null {
  const parsed = tryParseJson(raw)
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return null
  }
  const name = (parsed as { t?: unknown }).t
  if (typeof name !== 'string' || !CLIENT_NAME_SET.has(name)) {
    return null
  }
  return parsed as ClientMessage
}

function tryParseJson(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return null
  }
}
