import { describe, expect, it, vi } from 'vitest'

import { ManualClock } from '@/test/manual-clock'

import { HEARTBEAT_INTERVAL_MS, HeartbeatMonitor } from './heartbeat'

describe('core/realtime heartbeat 15s（注入时钟，无真实等待）', () => {
  const setup = () => {
    const clock = new ManualClock()
    const sent: number[] = []
    const onTimeout = vi.fn()
    const monitor = new HeartbeatMonitor({
      scheduler: clock,
      intervalMs: HEARTBEAT_INTERVAL_MS,
      sendHeartbeat: () => sent.push(clock.now()),
      onTimeout,
    })
    return { clock, sent, onTimeout, monitor }
  }

  it('默认间隔常量为 15000ms（技术方案 5.2：心跳 15s）', () => {
    expect(HEARTBEAT_INTERVAL_MS).toBe(15_000)
  })

  it.each([
    { desc: '到期 15s 发出心跳', steps: [{ advance: 15_000 }], expectSentAt: [15_000] },
    { desc: '应答重置：ack 后下个周期照常发出', steps: [{ advance: 15_000 }, { ack: true, advance: 15_000 }], expectSentAt: [15_000, 30_000] },
    { desc: '持续推进按固定周期发出（不因应答漂移）', steps: [{ advance: 15_000 }, { ack: true, advance: 15_000 }, { ack: true, advance: 15_000 }], expectSentAt: [15_000, 30_000, 45_000] },
  ])('$desc', ({ steps, expectSentAt }) => {
    const { clock, sent, monitor } = setup()
    monitor.start()
    for (const step of steps) {
      if (step.ack) {
        monitor.onAck()
      }
      clock.advance(step.advance)
    }
    expect(sent).toEqual(expectSentAt)
  })

  it('超时：一个完整周期内无应答，下一 tick 判死并停止心跳（onTimeout 恰好一次）', () => {
    const { clock, sent, onTimeout, monitor } = setup()
    monitor.start()
    clock.advance(15_000) // 发出第 1 次心跳
    expect(sent).toEqual([15_000])
    clock.advance(15_000) // 无应答到期：判死，不再发心跳
    expect(onTimeout).toHaveBeenCalledTimes(1)
    expect(sent).toEqual([15_000])
    clock.advance(60_000) // 判死后不再有任何动作
    expect(onTimeout).toHaveBeenCalledTimes(1)
    expect(sent).toEqual([15_000])
  })

  it('stop 取消定时器：不再发出心跳、不触发超时', () => {
    const { clock, sent, onTimeout, monitor } = setup()
    monitor.start()
    monitor.stop()
    clock.advance(120_000)
    expect(sent).toEqual([])
    expect(onTimeout).not.toHaveBeenCalled()
    expect(clock.pendingCount()).toBe(0)
  })

  it('stop 后可重新 start（joined 离开再进入的场景）', () => {
    const { clock, sent, monitor } = setup()
    monitor.start()
    clock.advance(15_000)
    monitor.stop()
    monitor.start()
    clock.advance(15_000)
    expect(sent).toEqual([15_000, 30_000])
  })
})
