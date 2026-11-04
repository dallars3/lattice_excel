// Vitest 全局 setup（票 04）：接入 MSW 生命周期。MSW 是组件/模块测试唯一 HTTP 边界
// （spec Testing Decisions）；未注册 handler 的请求直接报错，防止测试触达真实网络。
import { afterAll, afterEach, beforeAll } from 'vitest'

import { mswServer } from '@/mocks/server'

beforeAll(() => mswServer.listen({ onUnhandledRequest: 'error' }))
afterEach(() => mswServer.resetHandlers())
afterAll(() => mswServer.close())
