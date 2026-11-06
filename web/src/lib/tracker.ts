// 埋点采集器（H-94 / 缺陷 #30 / 技术方案 §13）：附录 C 事件经 track(name, fields) 进入
// 本地缓冲，达到批量阈值或缓冲超时后整批上报 POST /api/v1/tracking/batch。
// 语义红线（工单 17 验收）：
// - 批量上报：默认 10 条或 30s 触发 flush；在途期间新事件继续入队，不打断主流程；
// - 失败静默丢弃：sender 失败只丢弃本批，绝不重试放大，绝不向调用方抛错；
// - 永不阻塞：track/flush 全程同步返回，网络在后台进行。
// 上报端点契约（接口文档 v0.7 §2.16 / gateway/tracking_routes.go）：单批 ≤100 条、
// event_id 8~64 字符、event_name ^fe_[a-z0-9_]+$、detail 为 JSON 对象；
// 网关 DisallowUnknownFields —— 批量请求不得携带 client_request_id。
import { http } from '@/core/http'

import { createRequestId } from '@/core/http/idempotency'
import { realScheduler, type Scheduler } from '@/core/realtime/scheduler'

/** 单条埋点事件（H-94 请求体 events[] 元素）。 */
export interface TrackEvent {
  readonly event_id: string
  readonly event_name: string
  readonly detail: Record<string, unknown>
  readonly client_at_ms: number
}

/** 上报边界：整批发送，成功 resolve、失败 reject（由采集器静默消化）。 */
export type TrackSender = (events: readonly TrackEvent[]) => Promise<void>

export interface TrackerOptions {
  sender: TrackSender
  scheduler: Scheduler
  /** 缓冲条数阈值，达到即整批 flush（技术方案 §13：10 条触发）。 */
  batchSize?: number
  /** 缓冲超时（毫秒），首批入队后计时，到期 flush（技术方案 §13：30s 触发）。 */
  flushIntervalMs?: number
}

/** 默认批量阈值与超时：技术方案 §13（10 条或 30s）。 */
export const TRACK_BATCH_SIZE = 10
export const TRACK_FLUSH_INTERVAL_MS = 30_000

/** 工单 17 接线事件名（附录 C 清单的子集；事实源 前端需求文档-v0.1.md 附录 C）。 */
export const TRACK_EVENTS = {
  /** 登录成功（附录 C：fe_auth_login_success，关键字段 user_type）。 */
  loginSuccess: 'fe_auth_login_success',
  /** 新建 Base（附录 C：fe_base_create）。 */
  baseCreate: 'fe_base_create',
  /** 单元格提交保存成功（附录 C：fe_cell_edit_submit，关键字段 field_type）。 */
  cellEditSubmit: 'fe_cell_edit_submit',
  /** 首屏可交互（附录 C：fe_perf_screen_ready，关键字段 rows、t_ms）。 */
  perfScreenReady: 'fe_perf_screen_ready',
} as const

interface Tracker {
  track: (eventName: string, fields?: Record<string, unknown>) => void
  trackOnce: (eventName: string, fields?: Record<string, unknown>) => void
  flush: () => Promise<void>
  /** 测试接缝：等待在途（含链式跟进）flush 全部落幕。 */
  settle: () => Promise<void>
  pendingCount: () => number
}

export function createTracker(options: TrackerOptions): Tracker {
  const batchSize = options.batchSize ?? TRACK_BATCH_SIZE
  const flushIntervalMs = options.flushIntervalMs ?? TRACK_FLUSH_INTERVAL_MS
  const { sender, scheduler } = options

  let buffer: TrackEvent[] = []
  let flushTimer: number | null = null
  let inFlightPromise: Promise<void> | null = null
  // trackOnce 去重集合：每页面加载至多一次的事件（如 fe_perf_screen_ready）。
  const reportedOnce = new Set<string>()

  function clearTimer(): void {
    if (flushTimer !== null) {
      scheduler.clearTimeout(flushTimer)
      flushTimer = null
    }
  }

  function armTimer(): void {
    if (flushTimer === null) {
      flushTimer = scheduler.setTimeout(() => {
        flushTimer = null
        void flush()
      }, flushIntervalMs)
    }
  }

  function flush(): Promise<void> {
    if (inFlightPromise !== null || buffer.length === 0) return Promise.resolve()
    const batch = buffer
    buffer = []
    clearTimer()
    inFlightPromise = (async () => {
      try {
        await sender(batch)
      } catch {
        // 失败静默丢弃本批：不重试放大、不影响主流程（工单 17 验收口径）。
      } finally {
        inFlightPromise = null
        if (buffer.length >= batchSize) {
          await flush()
        } else if (buffer.length > 0) {
          armTimer()
        }
      }
    })()
    return inFlightPromise
  }

  async function settle(): Promise<void> {
    while (inFlightPromise !== null) {
      await inFlightPromise
    }
  }

  function track(eventName: string, fields?: Record<string, unknown>): void {
    buffer.push({
      event_id: createRequestId(),
      event_name: eventName,
      detail: fields ?? {},
      client_at_ms: scheduler.now(),
    })
    if (buffer.length >= batchSize) {
      void flush()
      return
    }
    armTimer()
  }

  /** 每页面加载至多一次的事件：重复调用按首次字段上报，之后忽略。 */
  function trackOnce(eventName: string, fields?: Record<string, unknown>): void {
    if (reportedOnce.has(eventName)) return
    reportedOnce.add(eventName)
    track(eventName, fields)
  }

  return {
    track,
    trackOnce,
    flush,
    settle,
    pendingCount: () => buffer.length,
  }
}

// ---------- 模块单例（默认绑定 H-94 端点） ----------

/** 默认上报边界：H-94 批量端点。网关 DisallowUnknownFields，禁带 client_request_id。 */
async function defaultSender(events: readonly TrackEvent[]): Promise<void> {
  await http.post(
    '/api/v1/tracking/batch',
    { events },
    { attachClientRequestId: false },
  )
}

function createDefaultTracker(): Tracker {
  return createTracker({ sender: defaultSender, scheduler: realScheduler })
}

let activeTracker: Tracker = createDefaultTracker()

/** 应用启动时刻（模块初始化）：fe_perf_screen_ready 的 t_ms 计时原点。 */
const bootTimeMs = Date.now()

/** 自应用启动起经过的毫秒数（首屏可交互耗时口径）。 */
export function elapsedSinceBootMs(): number {
  return Math.max(0, Date.now() - bootTimeMs)
}

/**
 * 记录一条埋点（批量缓冲，达阈值/超时后整批上报；失败静默丢弃，绝不抛错）。
 * 事件名取附录 C 清单（`TRACK_EVENTS` 提供工单 17 接线子集的常量）。
 */
export function track(eventName: string, fields?: Record<string, unknown>): void {
  activeTracker.track(eventName, fields)
}

/** 记录每页面加载至多一次的事件（如 fe_perf_screen_ready）。 */
export function trackOnce(eventName: string, fields?: Record<string, unknown>): void {
  activeTracker.trackOnce(eventName, fields)
}

/**
 * 重置单例：先等待旧实例在途上报落幕再替换。生产不调用；测试隔离用——
 * options 可注入 sender/scheduler/阈值（注入 sender 后请求不再走 MSW 之外的边界）。
 */
export function resetTracker(
  options?: Partial<Pick<TrackerOptions, 'sender' | 'scheduler' | 'batchSize' | 'flushIntervalMs'>>,
): Promise<void> {
  const previous = activeTracker
  activeTracker =
    options === undefined
      ? createDefaultTracker()
      : createTracker({
          sender: options.sender ?? defaultSender,
          scheduler: options.scheduler ?? realScheduler,
          batchSize: options.batchSize,
          flushIntervalMs: options.flushIntervalMs,
        })
  return previous.settle()
}
