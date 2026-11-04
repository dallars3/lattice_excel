// 幂等键（client_request_id）：所有写请求自动生成 uuid v4 并携带；页内内存级 map 防重复提交；
// 失败重试沿用同一 id，成功后清空（接口文档 8.0.2 幂等约定 + 技术方案 5.1）。
// 生成器独立导出：离线队列（票 06）复用同一生成器，保证 id 口径一致。

/** uuid v4：优先 crypto.randomUUID；不可用（旧 jsdom/非安全上下文）时走 RFC 4122 v4 兜底。 */
export function createRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (char) => {
    const rand = (Math.random() * 16) | 0
    const value = char === 'x' ? rand : (rand & 0x3) | 0x8
    return value.toString(16)
  })
}

interface IdempotencyEntry {
  /** 本逻辑写操作的幂等 id：失败后保留供重试复用，成功后随条目一起清除。 */
  requestId: string
  /** 在途请求 promise：存在则同 key 并发调用复用它（防重复提交）。 */
  promise?: Promise<unknown>
}

/**
 * 页内幂等协调器。逻辑写以 key 标识（httpclient 写请求默认按 method+url+body 派生；
 * 显式幂等场景可传自定义 key）：
 * - 在途去重：同 key 并发第二次 run 返回同一 promise，send 只执行一次；
 * - 失败复用：send reject 后保留 requestId，同 key 重试复用同一 id；
 * - 成功清空：send resolve 后删除条目，下一次同 key 是新的一轮写。
 */
export class IdempotencyCoordinator {
  private readonly entries = new Map<string, IdempotencyEntry>()

  constructor(private readonly generate: () => string = createRequestId) {}

  run<T>(key: string, send: (requestId: string) => Promise<T>): Promise<T> {
    const existing = this.entries.get(key)
    if (existing?.promise) {
      return existing.promise as Promise<T>
    }
    const requestId = existing?.requestId ?? this.generate()
    const promise: Promise<T> = send(requestId).then(
      (value) => {
        this.entries.delete(key)
        return value
      },
      (error: unknown) => {
        // 失败：保留 requestId 供重试，仅清掉在途 promise。
        const current = this.entries.get(key)
        if (current && current.promise === promise) {
          current.promise = undefined
        }
        throw error
      },
    )
    this.entries.set(key, { requestId, promise })
    return promise
  }

  /** 清空全部状态（页面卸载/测试隔离用）。 */
  reset(): void {
    this.entries.clear()
  }
}

/** httpClient 默认使用的页内协调器实例。 */
export const defaultIdempotency = new IdempotencyCoordinator()

/** 重置默认协调器（仅测试/会话整体重置时使用）。 */
export function resetIdempotency(): void {
  defaultIdempotency.reset()
}
