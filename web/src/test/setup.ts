// Vitest 全局 setup（票 04）：接入 MSW 生命周期。MSW 是组件/模块测试唯一 HTTP 边界
// （spec Testing Decisions）；未注册 handler 的请求直接报错，防止测试触达真实网络。
import { afterAll, afterEach, beforeAll } from 'vitest'

import { resetTracker } from '@/lib/tracker'
import { mswServer } from '@/mocks/server'

beforeAll(() => mswServer.listen({ onUnhandledRequest: 'error' }))
afterEach(() => mswServer.resetHandlers())
afterAll(() => mswServer.close())

// 埋点（工单 17）：测试默认换入无副作用 sender——业务测试的埋点事件只入缓冲不触网，
// 避免 MSW onUnhandledRequest 误伤既有用例；需要断言埋点的用例自行 resetTracker 注入 spy。
resetTracker({ sender: async () => {} })
