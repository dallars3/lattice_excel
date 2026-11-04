// MSW 测试服务端（票 04 建，票 09 复用）：组件/模块测试唯一 HTTP 边界。
// 生命周期在 src/test/setup.ts 统一接线；用例内经 mswServer.use(...) 注册 handler。
import { setupServer } from 'msw/node'

/** 无默认 handler：每个用例显式声明自己需要的后端路由（契约显式化）。 */
export const mswServer = setupServer()
