import { authStore, workspaceStore } from '@/stores'

import {
  normalizeBaseRole,
  resolvePermission,
  type BaseRole,
  type PermissionAction,
  type PermissionState,
} from './matrix'

/** 权限门只依赖 Base 的 my_role；结构类型便于各域 Base 形状直接传入。 */
export interface PermissionHolder {
  my_role?: string | null
  /** 归一后的 Base 摘要使用 camelCase 字段（features/home/api.ts）。 */
  myRole?: string | null
}

export interface PermissionApi {
  role: BaseRole
  internal: boolean
  resolve: (action: PermissionAction) => PermissionState
  can: (action: PermissionAction) => boolean
}

/** 权限门 hook：Base 角色 + 会话内外部身份 → 动作 UI 态；无 Base 时角色按 viewer。
 * 票 09：强制只读（3001/3002）或离线占位时按 viewer 降级，保留内外部身份。 */
export function usePermission(base?: PermissionHolder | null): PermissionApi {
  const internal = authStore((state) => state.session?.internal ?? false)
  const forcedReadOnly = workspaceStore((state) => state.forcedReadOnly)
  const offline = workspaceStore((state) => state.offline)
  const actualRole = normalizeBaseRole(base?.my_role ?? base?.myRole)
  const role: BaseRole = forcedReadOnly || offline ? 'viewer' : actualRole

  const resolve = (action: PermissionAction): PermissionState =>
    resolvePermission(action, { role, internal })

  return {
    role,
    internal,
    resolve,
    can: (action) => resolve(action) === 'allow',
  }
}
