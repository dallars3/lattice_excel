import { describe, expect, it, vi } from 'vitest'

import {
  IdempotencyCoordinator,
  createRequestId,
  defaultIdempotency,
  resetIdempotency,
} from './idempotency'

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

describe('core/http idempotency', () => {
  describe('createRequestId', () => {
    it('生成 uuid v4 格式且连续调用不重复', () => {
      const a = createRequestId()
      const b = createRequestId()
      expect(a).toMatch(UUID_V4)
      expect(b).toMatch(UUID_V4)
      expect(a).not.toBe(b)
    })
  })

  describe('IdempotencyCoordinator（注入生成器，表驱动）', () => {
    it('首次运行：把新生成的 id 传给 send 并返回其结果', async () => {
      const coordinator = new IdempotencyCoordinator(() => 'req-fixed-1')
      const seen: string[] = []
      const value = await coordinator.run('key-a', async (id) => {
        seen.push(id)
        return 'ok'
      })
      expect(seen).toEqual(['req-fixed-1'])
      expect(value).toBe('ok')
    })

    it('在途去重：同 key 并发第二次调用复用同一 promise，send 只执行一次（页内防重复提交）', async () => {
      const coordinator = new IdempotencyCoordinator(() => 'req-1')
      let calls = 0
      const send = async (id: string) => {
        calls += 1
        return `result:${id}`
      }
      const [p1, p2] = [coordinator.run('k', send), coordinator.run('k', send)]
      const [r1, r2] = await Promise.all([p1, p2])
      expect(calls).toBe(1)
      expect(r1).toBe('result:req-1')
      expect(r2).toBe('result:req-1')
    })

    it('失败重试复用同一 id：第一次 reject 后，同 key 再跑拿到相同 id', async () => {
      const coordinator = new IdempotencyCoordinator(() => 'req-same-on-retry')
      const seen: string[] = []
      const attempt = (n: number) => async (id: string) => {
        seen.push(id)
        if (n === 1) throw new Error('网络失败')
        return 'ok'
      }
      await expect(coordinator.run('k', attempt(1))).rejects.toThrow('网络失败')
      const value = await coordinator.run('k', attempt(2))
      expect(value).toBe('ok')
      expect(seen).toEqual(['req-same-on-retry', 'req-same-on-retry'])
    })

    it('成功后清空映射：同 key 再次发起得到新 id（新一轮业务写）', async () => {
      let counter = 0
      const coordinator = new IdempotencyCoordinator(() => `req-${(counter += 1)}`)
      const seen: string[] = []
      const send = async (id: string) => {
        seen.push(id)
        return id
      }
      await coordinator.run('k', send)
      await coordinator.run('k', send)
      expect(seen).toEqual(['req-1', 'req-2'])
    })

    it('不同 key 各自独立 id，互不干扰', async () => {
      let counter = 0
      const coordinator = new IdempotencyCoordinator(() => `req-${(counter += 1)}`)
      const seen: string[] = []
      const send = async (id: string) => {
        seen.push(id)
        return id
      }
      await Promise.all([coordinator.run('a', send), coordinator.run('b', send)])
      expect(seen).toEqual(['req-1', 'req-2'])
    })

    it('失败后在途 promise 清理完毕，同 key 并发重试不会拿到旧 rejection', async () => {
      const coordinator = new IdempotencyCoordinator(() => 'req-x')
      const first = coordinator.run('k', async () => {
        throw new Error('fail-1')
      })
      await expect(first).rejects.toThrow('fail-1')
      // 失败落定后再跑：应重新发起 send（带同一 id），而不是复用旧 promise
      let calls = 0
      const value = await coordinator.run('k', async (id) => {
        calls += 1
        return `ok:${id}`
      })
      expect(calls).toBe(1)
      expect(value).toBe('ok:req-x')
    })
  })

  describe('模块级默认实例', () => {
    it('defaultIdempotency 跨调用复用/清空，resetIdempotency 供测试隔离', async () => {
      resetIdempotency()
      const send = vi.fn(async (id: string) => id)
      const first = await defaultIdempotency.run('default-key', send)
      resetIdempotency()
      const second = await defaultIdempotency.run('default-key', send)
      expect(first).toMatch(UUID_V4)
      expect(second).toMatch(UUID_V4)
      expect(first).not.toBe(second)
      expect(send).toHaveBeenCalledTimes(2)
    })
  })
})
