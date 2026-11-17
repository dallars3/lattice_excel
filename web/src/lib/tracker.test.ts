// 埋点采集器单测（H-94 / 缺陷 #30）：时间行为全部经 ManualClock 注入（票 05 接缝），
// 上报边界经 Fake sender 注入——测试绝不触网，也绝不真实等待。
// 单例默认 sender 用 MSW 验证（组件/模块测试唯一 HTTP 边界）。
import { afterEach, describe, expect, it, vi } from 'vitest'

import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { ManualClock } from '@/test/manual-clock'

import {
  TRACK_EVENTS,
  createTracker,
  elapsedSinceBootMs,
  resetTracker,
  track,
  type TrackEvent,
} from './tracker'

describe('createTracker 批量上报', () => {
  it('达到 batchSize 阈值即按事件顺序整批 flush，载荷符合 H-94 契约', async () => {
    const clock = new ManualClock()
    const sender = vi.fn().mockResolvedValue(undefined)
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 3 })

    tracker.track('fe_base_create')
    tracker.track('fe_cell_edit_submit', { field_type: 'text' })
    expect(sender).not.toHaveBeenCalled()

    tracker.track('fe_auth_login_success', { user_type: 'internal' })
    await tracker.settle()

    expect(sender).toHaveBeenCalledTimes(1)
    const batch = sender.mock.calls[0][0] as TrackEvent[]
    expect(batch).toHaveLength(3)
    expect(batch.map((event) => event.event_name)).toEqual([
      'fe_base_create',
      'fe_cell_edit_submit',
      'fe_auth_login_success',
    ])
    for (const event of batch) {
      expect(event.event_id.length).toBeGreaterThanOrEqual(8)
      expect(event.client_at_ms).toBe(0)
      expect(event.detail).toBeTypeOf('object')
    }
    expect(batch[1].detail).toEqual({ field_type: 'text' })
    expect(batch[2].detail).toEqual({ user_type: 'internal' })
    expect(tracker.pendingCount()).toBe(0)
  })

  it('未达阈值时按 flushIntervalMs 缓冲超时上报，事件 client_at_ms 取入队时刻', async () => {
    const clock = new ManualClock()
    const sender = vi.fn().mockResolvedValue(undefined)
    const tracker = createTracker({
      sender,
      scheduler: clock,
      batchSize: 10,
      flushIntervalMs: 30_000,
    })

    clock.advance(5_000)
    tracker.track('fe_base_create')
    clock.advance(29_999)
    expect(sender).not.toHaveBeenCalled()

    clock.advance(1)
    await tracker.settle()

    expect(sender).toHaveBeenCalledTimes(1)
    const batch = sender.mock.calls[0][0] as TrackEvent[]
    expect(batch).toHaveLength(1)
    expect(batch[0].client_at_ms).toBe(5_000)
  })

  it('sender 失败静默丢弃本批：不重试、不抛错，后续事件照常入队上报', async () => {
    const clock = new ManualClock()
    const sender = vi
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(undefined)
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 2 })

    tracker.track('fe_base_create')
    tracker.track('fe_base_create')
    await tracker.settle()
    expect(sender).toHaveBeenCalledTimes(1)

    // 失败批不回队：缓冲为空，不因重试放大请求。
    expect(tracker.pendingCount()).toBe(0)

    tracker.track('fe_cell_edit_submit', { field_type: 'number' })
    tracker.track('fe_cell_edit_submit', { field_type: 'number' })
    await tracker.settle()
    expect(sender).toHaveBeenCalledTimes(2)
    const second = sender.mock.calls[1][0] as TrackEvent[]
    expect(second).toHaveLength(2)
  })

  it('track 不因 sender 失败向调用方抛错（flush 后台吞错）', async () => {
    const clock = new ManualClock()
    const sender = vi.fn().mockRejectedValue(new Error('boom'))
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 1 })

    expect(() => tracker.track('fe_base_create')).not.toThrow()
    await expect(tracker.flush()).resolves.toBeUndefined()
    await tracker.settle()
    expect(sender).toHaveBeenCalledTimes(1)
  })

  it('flush 在途时不重入：在途期间入队的事件留在缓冲，结算后按阈值跟进', async () => {
    const clock = new ManualClock()
    let releaseFirst: () => void = () => undefined
    const sender = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            releaseFirst = resolve
          }),
      )
      .mockResolvedValue(undefined)
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 2 })

    tracker.track('fe_base_create')
    tracker.track('fe_base_create')
    // 第一批在途，未结算。
    expect(sender).toHaveBeenCalledTimes(1)

    tracker.track('fe_cell_edit_submit', { field_type: 'text' })
    tracker.track('fe_cell_edit_submit', { field_type: 'text' })
    expect(sender).toHaveBeenCalledTimes(1)
    expect(tracker.pendingCount()).toBe(2)

    releaseFirst?.()
    await tracker.settle()

    expect(sender).toHaveBeenCalledTimes(2)
    expect(tracker.pendingCount()).toBe(0)
  })

  it('空缓冲 flush 是无操作，不触发 sender', async () => {
    const clock = new ManualClock()
    const sender = vi.fn().mockResolvedValue(undefined)
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 10 })

    await tracker.flush()
    expect(sender).not.toHaveBeenCalled()
  })

  it('trackOnce 同名事件每次页面加载至多上报一次', async () => {
    const clock = new ManualClock()
    const sender = vi.fn().mockResolvedValue(undefined)
    const tracker = createTracker({ sender, scheduler: clock, batchSize: 10 })

    tracker.trackOnce('fe_perf_screen_ready', { rows: 12, t_ms: 800 })
    tracker.trackOnce('fe_perf_screen_ready', { rows: 99, t_ms: 9_999 })
    tracker.track('fe_base_create')
    await tracker.flush()

    expect(sender).toHaveBeenCalledTimes(1)
    const batch = sender.mock.calls[0][0] as TrackEvent[]
    expect(batch.map((event) => event.event_name)).toEqual([
      'fe_perf_screen_ready',
      'fe_base_create',
    ])
    expect(batch[0].detail).toEqual({ rows: 12, t_ms: 800 })
  })
})

describe('模块单例与默认 sender（MSW 边界）', () => {
  afterEach(() => {
    resetTracker()
  })

  it('事件名常量与附录 C 清单一致', () => {
    // 事实源：docs/doc/前端需求文档-v0.1.md 附录 C（埋点事件清单）。
    expect(TRACK_EVENTS.loginSuccess).toBe('fe_auth_login_success')
    expect(TRACK_EVENTS.baseCreate).toBe('fe_base_create')
    expect(TRACK_EVENTS.cellEditSubmit).toBe('fe_cell_edit_submit')
    expect(TRACK_EVENTS.perfScreenReady).toBe('fe_perf_screen_ready')
  })

  it('默认 sender 上报 H-94：POST /api/v1/tracking/batch，不带 client_request_id', async () => {
    const bodies: unknown[] = []
    mswServer.use(
      mockEnvelope({
        path: '/api/v1/tracking/batch',
        method: 'post',
        onRequest: async (request) => {
          bodies.push((await request.json()) as unknown)
        },
        data: { accepted: 1, duplicated: 0, rejected: 0 },
      }),
    )
    for (let index = 0; index < 10; index += 1) {
      track('fe_base_create')
    }
    await resetTracker()

    expect(bodies).toHaveLength(1)
    const body = bodies[0] as { events: TrackEvent[] }
    expect(body.events).toHaveLength(10)
    expect(body.events[0].event_name).toBe('fe_base_create')
    expect(body.events[0].event_id).toBeTypeOf('string')
    expect(body.events[0].detail).toEqual({})
  })

  it('elapsedSinceBootMs 随真实时间单调非负', () => {
    const first = elapsedSinceBootMs()
    expect(first).toBeGreaterThanOrEqual(0)
  })
})
