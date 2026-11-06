import { useEffect, useRef, useState } from 'react'

import { useQuery } from '@tanstack/react-query'

import { fetchUnreadCount } from '@/features/notifications/api'
import { notificationKeys } from '@/query-keys'
import { authStore } from '@/stores'
import { Button } from '@/ui-kit'

import styles from './NotificationBell.module.css'

/** 未读计数刷新周期：页面可见时每 30 秒一次（票 07）。 */
export const UNREAD_REFRESH_INTERVAL_MS = 30_000

/** 通知铃铛（H-74）：只展示未读计数，通知中心列表属 M4。 */
export function NotificationBell() {
  const signedIn = authStore((state) => state.session !== null)
  const [open, setOpen] = useState(false)
  const query = useQuery({
    queryKey: notificationKeys.unreadCount(),
    queryFn: fetchUnreadCount,
    enabled: signedIn,
    // 旁路入口不重试：失败即降级 0，不拖慢首页其它请求。
    retry: false,
    refetchOnWindowFocus: false,
  })

  // 定时器只依赖登录态，refetch 经 ref 转发避免每次渲染重置 30 秒窗口。
  const refetchRef = useRef(query.refetch)
  useEffect(() => {
    refetchRef.current = query.refetch
  }, [query.refetch])

  useEffect(() => {
    if (!signedIn) return
    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void refetchRef.current()
    }, UNREAD_REFRESH_INTERVAL_MS)
    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') void refetchRef.current()
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [signedIn])

  // 未登录/接口失败：铃铛降级为 0 未读，不影响首页其它功能。
  const unreadCount = signedIn && !query.isError ? (query.data ?? 0) : 0

  return (
    <div className={styles.wrap}>
      <button
        type="button"
        className={styles.bell}
        aria-label={`通知，${unreadCount} 条未读`}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <svg
          aria-hidden="true"
          focusable="false"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
          <path d="M13.7 21a2 2 0 0 1-3.4 0" />
        </svg>
        {unreadCount > 0 ? (
          <span className={styles.badge} aria-hidden="true">
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        ) : null}
      </button>

      {open ? (
        <div className={styles.panel} role="dialog" aria-label="通知">
          <p className={styles.count}>未读通知：{unreadCount} 条</p>
          <p>通知中心将在后续版本提供</p>
          <Button variant="secondary" onClick={() => setOpen(false)}>
            关闭
          </Button>
        </div>
      ) : null}
    </div>
  )
}
