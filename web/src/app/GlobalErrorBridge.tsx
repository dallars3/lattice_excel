// 全局异常桥（票 09）：挂在 ToastProvider 内，统一处理跨页面错误码与离线占位。
// 2001 重登；3001/3002 强制只读；5003 限流提示；9001/9002 服务重试提示。
// 离线：online/offline 事件与初始 navigator.onLine 驱动 workspaceStore.offline，渲染黄色横幅。
import { useEffect, useRef, type ReactNode } from 'react'

import { setApiErrorObserver, type ApiError } from '@/core/http'
import { authStore, workspaceStore } from '@/stores'
import { Banner, useToast } from '@/ui-kit'

import styles from './GlobalErrorBridge.module.css'

export const PERMISSION_CHANGED_MESSAGE = '你的权限已变更，页面已转为只读'
export const EXTERNAL_EXPORT_MESSAGE = '外部协作者不可导出数据'
export const RATE_LIMIT_MESSAGE = '操作过于频繁，请稍候片刻再试'
export const SERVICE_RETRY_MESSAGE = '服务暂时不可用，正在重试（最多 3 次）'
export const OFFLINE_MESSAGE = '网络已断开，恢复后将自动同步'

/** 同一提示 1s 内去重，避免读请求自动重试时堆叠多条相同 toast。 */
const NOTICE_DEDUPE_MS = 1000

export function GlobalErrorBridge({ children }: { children: ReactNode }) {
  const toast = useToast()
  const lastNoticeAt = useRef<Record<string, number>>({})

  useEffect(() => {
    const notify = (key: string, message: string) => {
      const now = Date.now()
      const previous = lastNoticeAt.current[key]
      if (previous !== undefined && now - previous < NOTICE_DEDUPE_MS) return
      lastNoticeAt.current[key] = now
      toast.push(message)
    }

    setApiErrorObserver((error: ApiError) => {
      switch (error.code) {
        case 2001:
          authStore.getState().openRelogin()
          return
        case 3001:
          workspaceStore.getState().setForcedReadOnly(true)
          notify('3001', PERMISSION_CHANGED_MESSAGE)
          return
        case 3002:
          workspaceStore.getState().setForcedReadOnly(true)
          notify('3002', PERMISSION_CHANGED_MESSAGE)
          notify('3002-export', EXTERNAL_EXPORT_MESSAGE)
          return
        case 5003:
          notify('5003', RATE_LIMIT_MESSAGE)
          return
        case 9001:
        case 9002:
          notify(String(error.code), SERVICE_RETRY_MESSAGE)
          return
        default:
          return
      }
    })

    return () => setApiErrorObserver(null)
  }, [toast])

  const offline = workspaceStore((state) => state.offline)

  useEffect(() => {
    const sync = () => {
      workspaceStore.getState().setOffline(navigator.onLine === false)
    }
    sync()
    window.addEventListener('online', sync)
    window.addEventListener('offline', sync)
    return () => {
      window.removeEventListener('online', sync)
      window.removeEventListener('offline', sync)
    }
  }, [])

  return (
    <>
      {offline ? (
        <Banner tone="warning" className={styles.offline}>
          {OFFLINE_MESSAGE}
        </Banner>
      ) : null}
      {children}
    </>
  )
}
