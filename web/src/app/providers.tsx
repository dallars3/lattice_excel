import { type ReactNode } from 'react'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { isApiError } from '@/core/http'
import { ToastProvider } from '@/ui-kit'

import { GlobalErrorBridge } from './GlobalErrorBridge'

/** 读请求自动重试：仅服务暂不可用（9001/9002，含网络失败包装的 9001）重试 3 次。 */
export function shouldRetryRead(failureCount: number, error: unknown): boolean {
  if (failureCount >= 3) return false
  return isApiError(error) && (error.code === 9001 || error.code === 9002)
}

/** 重试间隔：测试环境 0ms，生产指数退避（1s 起，上限 30s）。 */
export function readRetryDelay(failureCount: number): number {
  if (import.meta.env.MODE === 'test') return 0
  return Math.min(30_000, 1_000 * 2 ** failureCount)
}

// 服务端状态根 Provider（票 02）：QueryClient 挂根，全树组件可经 useQuery/useQueryClient 消费。
// 票 09：读请求默认按错误码目录自动重试 3 次；写请求保持失败保留输入 + 手动重试（票 06）。
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: shouldRetryRead, retryDelay: readRetryDelay },
    mutations: { retry: false },
  },
})

/** 供测试与装配层直接访问的服务端状态缓存实例。 */
export const appQueryClient = queryClient

// Zustand store（src/stores/）为模块级单例，无需 Provider 包裹——任意组件直接 import 消费；
// 挂根语义由此达成（plan §3.4：05/06 填充实现时不回改装配层）。
export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <GlobalErrorBridge>{children}</GlobalErrorBridge>
      </ToastProvider>
    </QueryClientProvider>
  )
}
