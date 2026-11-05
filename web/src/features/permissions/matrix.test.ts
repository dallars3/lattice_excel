import { describe, expect, it } from 'vitest'

import {
  normalizeBaseRole,
  resolvePermission,
  type BaseRole,
  type PermissionAction,
  type PermissionState,
} from './matrix'

const INTERNAL_MATRIX: Record<BaseRole, Record<PermissionAction, PermissionState>> = {
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

const ROLES: BaseRole[] = ['manager', 'editor', 'filler', 'viewer']

describe('normalizeBaseRole（Base.my_role 归一）', () => {
  it.each([
    ['owner', 'manager'],
    ['manager', 'manager'],
    ['editor', 'editor'],
    ['filler', 'filler'],
    ['viewer', 'viewer'],
    ['OWNER', 'manager'],
    ['  Viewer ', 'viewer'],
  ])('%s → %s', (raw, expected) => {
    expect(normalizeBaseRole(raw)).toBe(expected)
  })

  it('未知/空角色按最小权限 viewer 兜底', () => {
    expect(normalizeBaseRole(undefined)).toBe('viewer')
    expect(normalizeBaseRole(null)).toBe('viewer')
    expect(normalizeBaseRole('')).toBe('viewer')
    expect(normalizeBaseRole('admin')).toBe('viewer')
  })
})

describe('resolvePermission（内部账号：需求 §5.1 矩阵）', () => {
  it.each(ROLES)('%s 的全部动作与矩阵一致', (role) => {
    for (const [action, expected] of Object.entries(INTERNAL_MATRIX[role])) {
      expect(resolvePermission(action as PermissionAction, { role, internal: true })).toBe(expected)
    }
  })

  it('viewer 恒 hide 写入口（新增记录/删除记录/结构管理/导入）', () => {
    for (const action of ['create_record', 'delete_record', 'manage_structure', 'import'] as const) {
      expect(resolvePermission(action, { role: 'viewer', internal: true })).toBe('hide')
    }
    expect(resolvePermission('edit_cell', { role: 'viewer', internal: true })).toBe('readonly')
  })
})

describe('resolvePermission（外部账号：恒无新建与导出）', () => {
  it.each(ROLES)('%s 外部账号恒 hide create_base / export / 管理入口', (role) => {
    for (const action of ['create_base', 'export', 'manage_base', 'star', 'import'] as const) {
      expect(resolvePermission(action, { role, internal: false })).toBe('hide')
    }
    expect(resolvePermission('manage_structure', { role, internal: false })).toBe('hide')
  })

  it('外部 editor 仍可编辑单元格/新增记录，filler 按自己记录过滤，viewer 只读', () => {
    expect(resolvePermission('edit_cell', { role: 'editor', internal: false })).toBe('allow')
    expect(resolvePermission('create_record', { role: 'editor', internal: false })).toBe('allow')
    expect(resolvePermission('edit_cell', { role: 'filler', internal: false })).toBe('filter-self')
    expect(resolvePermission('edit_cell', { role: 'viewer', internal: false })).toBe('readonly')
    expect(resolvePermission('create_record', { role: 'viewer', internal: false })).toBe('hide')
  })
})
