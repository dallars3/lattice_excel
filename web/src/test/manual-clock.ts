// 手控时钟（票 05 测试接缝）：实现 Scheduler，供定时器注入。
// 测试绝不真实等待——用 advance 推进虚拟时间，到期回调按时间序、平局按注册序执行。
import type { Scheduler, TimerHandle } from '@/core/realtime/scheduler'

interface ScheduledTask {
  readonly handle: TimerHandle
  atMs: number
  readonly fn: () => void
}

export class ManualClock implements Scheduler {
  private currentMs = 0
  private nextHandle = 0
  private readonly tasks = new Map<TimerHandle, ScheduledTask>()

  now(): number {
    return this.currentMs
  }

  setTimeout(fn: () => void, delayMs: number): TimerHandle {
    this.nextHandle += 1
    const handle = this.nextHandle
    this.tasks.set(handle, { handle, atMs: this.currentMs + Math.max(0, delayMs), fn })
    return handle
  }

  clearTimeout(handle: TimerHandle): void {
    this.tasks.delete(handle)
  }

  /** 推进虚拟时间：依次执行所有到期回调（回调内新注册的任务同样参与本轮判定）。 */
  advance(deltaMs: number): void {
    const targetMs = this.currentMs + Math.max(0, deltaMs)
    for (;;) {
      const due = this.earliestDueTask(targetMs)
      if (due === undefined) {
        break
      }
      this.tasks.delete(due.handle)
      this.currentMs = Math.max(this.currentMs, due.atMs)
      due.fn()
    }
    this.currentMs = targetMs
  }

  /** 当前排队中的定时器数量（断言定时器已取消/未注册时用）。 */
  pendingCount(): number {
    return this.tasks.size
  }

  private earliestDueTask(targetMs: number): ScheduledTask | undefined {
    let earliest: ScheduledTask | undefined
    for (const task of this.tasks.values()) {
      if (task.atMs > targetMs) {
        continue
      }
      if (
        earliest === undefined ||
        task.atMs < earliest.atMs ||
        (task.atMs === earliest.atMs && task.handle < earliest.handle)
      ) {
        earliest = task
      }
    }
    return earliest
  }
}
