// 浏览器侧 MSW worker（票 09）：仅 dev 生效——main.tsx 经 import.meta.env.DEV 动态 import 本模块，
// 生产构建该分支被静态消除，msw 与本文件不进产物（验证：npm run build 输出无 msw chunk、
// dist 内无 mockServiceWorker.js——SW 脚本由 vite.config.ts 的 apply:'serve' 中间件按需服务，不入 public/）。
// handler 与组件测试共用同一数据面（features/grid-view/demo-table + handlers.mockCursorRecords），
// 保证 dev 演示与 RTL 用例走完全一致的 cursor/limit/has_more 语义。
import { setupWorker } from 'msw/browser'

import { buildDemoRows, DEMO_RECORDS_PATH, DEMO_ROW_TOTAL } from '@/features/grid-view/demo-table'

import { mockCursorRecords } from './handlers'

export const demoWorker = setupWorker(
  mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }),
)

/** dev 启动入口：注册 service worker 拦截演示页请求。onUnhandledRequest bypass（演示页外不拦截）。 */
export async function enableDemoMocks(): Promise<void> {
  await demoWorker.start({ onUnhandledRequest: 'bypass' })
}
