import type { ReactNode } from 'react'

import type { PermissionState } from './matrix'

/** 统一权限门：仅 allow 渲染 children，其余态（hide/readonly/close/filter-self）一律不渲染。 */
export function PermissionGate({
  state,
  children,
}: {
  state: PermissionState
  children: ReactNode
}) {
  return state === 'allow' ? <>{children}</> : null
}
