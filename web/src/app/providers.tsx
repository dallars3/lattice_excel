import { useEffect, type ReactNode } from 'react'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

import { setApiErrorObserver } from '@/core/http'
import { authStore } from '@/stores'
import { ToastProvider } from '@/ui-kit'

// 服务端状态根 Provider（票 02）：QueryClient 挂根，全树组件可经 useQuery/useQueryClient 消费。
// 默认配置（retry 等）随票 04 httpClient 接入时再按错误码目录收敛。
const queryClient = new QueryClient()

/** 供测试与装配层直接访问的服务端状态缓存实例。 */
export const appQueryClient = queryClient

// Zustand store（src/stores/）为模块级单例，无需 Provider 包裹——任意组件直接 import 消费；
// 挂根语义由此达成（plan §3.4：05/06 填充实现时不回改装配层）。
export function AppProviders({ children }: { children: ReactNode }) {
  // 全局错误桥（票 01）：2001 统一打开重登弹窗；其他错误码保持原有局部消费语义（票 09 扩展）。
  useEffect(() => {
    setApiErrorObserver((error) => {
      if (error.code === 2001) {
        authStore.getState().openRelogin()
      }
    })
    return () => setApiErrorObserver(null)
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>{children}</ToastProvider>
    </QueryClientProvider>
  )
}
