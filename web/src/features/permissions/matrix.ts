// 权限门基座（需求 §5.1 矩阵直译）：Base.my_role × 内部/外部 → 四类 UI 态。
// 前端隐藏仅作体验优化，不作为安全边界（§5.3）；写操作最终以服务端 3001/3002 为准。

/** 归一后的 Base 角色：owner/manager → manager（可管理）；未知角色按 viewer 兜底。 */
export type BaseRole = 'manager' | 'editor' | 'filler' | 'viewer'

/** 四类 UI 态 + 一个放行态：close=入口不可打开/功能关闭，hide=入口不渲染。 */
export type PermissionState = 'allow' | 'hide' | 'readonly' | 'close' | 'filter-self'

/** 首页与后续票共用的动作字典。 */
export type PermissionAction =
  | 'create_base'
  | 'manage_base'
  | 'star'
  | 'manage_structure'
  | 'edit_view'
  | 'edit_cell'
  | 'create_record'
  | 'delete_record'
  | 'export'
  | 'import'
  | 'manage_share'
  | 'comment'

export interface PermissionContext {
  role: BaseRole
  internal: boolean
}

/** owner/manager 归一为可管理；其余已知角色原样，未知/空按 viewer。 */
export function normalizeBaseRole(raw: string | null | undefined): BaseRole {
  const value = (raw ?? '').trim().toLowerCase()
  switch (value) {
    case 'owner':
    case 'manager':
      return 'manager'
    case 'editor':
      return 'editor'
    case 'filler':
      return 'filler'
    case 'viewer':
      return 'viewer'
    default:
      return 'viewer'
  }
}

type RoleActions = Record<BaseRole, Record<PermissionAction, PermissionState>>

/** 内部账号矩阵（需求 §5.1）。 */
const INTERNAL_ACTIONS: RoleActions = {
  manager: {
    create_base: 'allow',
    manage_base: 'allow',
    star: 'allow',
    manage_structure: 'allow',
    edit_view: 'allow',
    edit_cell: 'allow',
    create_record: 'allow',
    delete_record: 'allow',
    export: 'allow',
    import: 'allow',
    manage_share: 'allow',
    comment: 'allow',
  },
  editor: {
    create_base: 'allow',
    manage_base: 'hide',
    star: 'allow',
    manage_structure: 'hide',
    edit_view: 'allow',
    edit_cell: 'allow',
    create_record: 'allow',
    delete_record: 'allow',
    export: 'allow',
    import: 'hide',
    manage_share: 'readonly',
    comment: 'allow',
  },
  filler: {
    create_base: 'allow',
    manage_base: 'hide',
    star: 'allow',
    manage_structure: 'hide',
    edit_view: 'hide',
    edit_cell: 'filter-self',
    create_record: 'allow',
    delete_record: 'hide',
    export: 'hide',
    import: 'hide',
    manage_share: 'close',
    comment: 'filter-self',
  },
  viewer: {
    create_base: 'allow',
    manage_base: 'hide',
    star: 'allow',
    manage_structure: 'hide',
    edit_view: 'hide',
    edit_cell: 'readonly',
    create_record: 'hide',
    delete_record: 'hide',
    export: 'hide',
    import: 'hide',
    manage_share: 'close',
    comment: 'allow',
  },
}

/** 外部账号恒关闭的动作（D13：外部协作者不可导出；外部无建 Base / Base 级管理）。 */
const EXTERNAL_FORCED_HIDE: readonly PermissionAction[] = [
  'create_base',
  'manage_base',
  'star',
  'manage_structure',
  'export',
  'import',
]

/** 解析动作的 UI 态：外部账号先覆盖恒隐藏项，再取角色矩阵。 */
export function resolvePermission(
  action: PermissionAction,
  context: PermissionContext,
): PermissionState {
  if (!context.internal && EXTERNAL_FORCED_HIDE.includes(action)) return 'hide'
  return INTERNAL_ACTIONS[context.role][action]
}
