import { useEffect, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { useQuery } from '@tanstack/react-query'

import { authKeys } from '@/query-keys'
import { authStore } from '@/stores'

import { fetchMe } from './api'

/** /home 与 /base/:id 的会话守卫：无会话时探测 H-04 me；失败则带 next 回登录页。 */
export function RequireSession({ children }: { children: ReactNode }) {
  const session = authStore((state) => state.session)
  const setSession = authStore((state) => state.setSession)
  const location = useLocation()
  const navigate = useNavigate()
  const target = `${location.pathname}${location.search}`
  // 一旦本页确认过会话，就不再探测：登出清空会话后不得再发 me 请求（避免登出后又建会话）。
  const [wasAuthenticated, setWasAuthenticated] = useState(session !== null)
  useEffect(() => {
    if (session !== null) setWasAuthenticated(true)
  }, [session])

  const meQuery = useQuery({
    queryKey: authKeys.me(),
    queryFn: fetchMe,
    enabled: session === null && !wasAuthenticated,
    retry: false,
  })

  useEffect(() => {
    if (session !== null || meQuery.data === undefined) return
    setSession({
      user: meQuery.data.user,
      enterprise: meQuery.data.enterprise,
      myRole: meQuery.data.my_role,
    })
  }, [session, meQuery.data, setSession])

  useEffect(() => {
    if (session !== null || !meQuery.isError) return
    // 首屏 me 返回 2001 时全局桥会打开重登弹窗；页面已跳登录页，关闭弹窗避免叠加。
    authStore.getState().closeRelogin()
    navigate(`/login?next=${encodeURIComponent(target)}`, { replace: true })
  }, [session, meQuery.isError, navigate, target])

  if (session !== null) return <>{children}</>
  if (meQuery.isError) return null
  return <p role="status">正在加载会话…</p>
}
