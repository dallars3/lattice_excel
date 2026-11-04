// realtime 传输层门面（票 05）：状态机、心跳、W-01~W-16 类型化消息、可注入传输层。
// 事实源：《前端-技术方案-v0.1.md》5.2 与《接口文档-v0.6.md》8.2（W-01~W-16）。
// 业务侧统一从此入口消费；M3 接真 WS 时仅替换 transportFactory 的实现，状态机与消费方零改动。
export {
  CLIENT_MESSAGE_NAMES,
  SERVER_MESSAGE_NAMES,
  W_MESSAGES,
  encodeClientMessage,
  parseClientMessage,
  parseServerMessage,
  type AckMessage,
  type CellChangedMessage,
  type CellRange,
  type CellValue,
  type ClientMessage,
  type ClientMessageMap,
  type ClientMessageName,
  type CursorEchoMessage,
  type CursorMessage,
  type EditingMessage,
  type HeartbeatMessage,
  type JoinMessage,
  type LeaveMessage,
  type MemberInfo,
  type MessageCode,
  type MessageDirection,
  type MessageName,
  type PresenceMessage,
  type RecordDeletedMessage,
  type RecalcStatusMessage,
  type ReplayRequestMessage,
  type SelectionMessage,
  type ServerMessage,
  type ServerMessageMap,
  type ServerMessageName,
  type SessionRevokedMessage,
  type SnapshotMessage,
  type StructureChangedMessage,
} from './messages'
export { HEARTBEAT_INTERVAL_MS, HeartbeatMonitor, type HeartbeatMonitorOptions } from './heartbeat'
export { realScheduler, type Scheduler, type TimerHandle } from './scheduler'
export {
  WsTransport,
  type Transport,
  type TransportCloseInfo,
  type TransportFactory,
  type TransportHandlers,
  type WsTransportOptions,
} from './transport'
export { FakeTransport, createFakeTransportFactory, type FakeTransportHarness } from './fake-transport'
export {
  BACKOFF_INITIAL_MS,
  BACKOFF_MAX_MS,
  REALTIME_STATES,
  RealtimeClient,
  type RealtimeClientOptions,
  type RealtimeState,
  type RoomSelector,
  type ServerMessageListener,
  type StateListener,
  type StateTransition,
  type Unsubscribe,
} from './state-machine'
