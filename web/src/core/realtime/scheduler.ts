// 定时器接缝（票 05）：状态机退避与心跳的全部时间行为都经由 Scheduler 注入。
// 测试用手控时钟（src/test/manual-clock.ts），绝不真实等待；生产用 realScheduler。
export type TimerHandle = number

export interface Scheduler {
  setTimeout(fn: () => void, delayMs: number): TimerHandle
  clearTimeout(handle: TimerHandle): void
  now(): number
}

/** 生产调度器：包装浏览器/Node 定时器；句柄在浏览器侧本就是 number。 */
export const realScheduler: Scheduler = {
  setTimeout: (fn, delayMs) => setTimeout(fn, delayMs) as unknown as TimerHandle,
  clearTimeout: (handle) => clearTimeout(handle),
  now: () => Date.now(),
}
