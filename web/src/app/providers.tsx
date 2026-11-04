import type { ReactNode } from 'react'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// 服务端状态根 Provider（票 02）：QueryClient 挂根，全树组件可经 useQuery/useQueryClient 消费。
// 默认配置（retry 等）随票 04 httpClient 接入时再按错误码目录收敛。
const queryClient = new QueryClient()

// Zustand store（src/stores/ 6 个骨架）为模块级单例，无需 Provider 包裹——任意组件直接 import 消费；
// 挂根语义由此达成（plan §3.4：05/06 填充实现时不回改装配层）。
export function AppProviders({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}
