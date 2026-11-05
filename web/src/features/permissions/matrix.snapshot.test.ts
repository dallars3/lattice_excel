// 权限矩阵快照（票 09）：四角色 × 内部/外部 × 全部动作的稳定输出。
// 快照只用于锁定矩阵形状；关键差异另以显式断言表达（需求 §5.1）。
import { describe, expect, it } from 'vitest'

import {
  resolvePermission,
  type BaseRole,
  type PermissionAction,
  type PermissionState,
} from './matrix'

const ROLES: readonly BaseRole[] = ['manager', 'editor', 'filler', 'viewer']

const ACTIONS: readonly PermissionAction[] = [
  'create_base',
  'manage_base',
  'star',
  'manage_structure',
  'edit_view',
  'edit_cell',
  'create_record',
  'delete_record',
  'export',
  'import',
  'manage_share',
  'comment',
]

function buildSnapshot(): Record<string, Record<string, PermissionState>> {
  const matrix: Record<string, Record<string, PermissionState>> = {}
  for (const internal of [true, false]) {
    const scope = internal ? 'internal' : 'external'
    for (const role of ROLES) {
      matrix[`${scope}/${role}`] = Object.fromEntries(
        ACTIONS.map((action) => [action, resolvePermission(action, { role, internal })]),
      )
    }
  }
  return matrix
}

describe('权限矩阵快照（需求 §5.1）', () => {
  it('四角色 × 内部/外部 × 全部动作输出稳定快照', () => {
    expect(buildSnapshot()).toMatchSnapshot()
  })

  it('可管理：内部全项允许', () => {
    for (const action of ACTIONS) {
      expect(resolvePermission(action, { role: 'manager', internal: true })).toBe('allow')
    }
  })

  it('可编辑：结构与 Base 管理隐藏，单元格/视图可编辑，星标保留', () => {
    expect(resolvePermission('manage_structure', { role: 'editor', internal: true })).toBe('hide')
    expect(resolvePermission('manage_base', { role: 'editor', internal: true })).toBe('hide')
    expect(resolvePermission('edit_cell', { role: 'editor', internal: true })).toBe('allow')
    expect(resolvePermission('edit_view', { role: 'editor', internal: true })).toBe('allow')
    expect(resolvePermission('star', { role: 'editor', internal: true })).toBe('allow')
    expect(resolvePermission('manage_share', { role: 'editor', internal: true })).toBe('readonly')
  })

  it('可填写：仅自己记录可编辑、仅新增、无删除/批量/结构/视图配置/导出/分享', () => {
    expect(resolvePermission('edit_cell', { role: 'filler', internal: true })).toBe('filter-self')
    expect(resolvePermission('create_record', { role: 'filler', internal: true })).toBe('allow')
    expect(resolvePermission('delete_record', { role: 'filler', internal: true })).toBe('hide')
    expect(resolvePermission('manage_structure', { role: 'filler', internal: true })).toBe('hide')
    expect(resolvePermission('edit_view', { role: 'filler', internal: true })).toBe('hide')
    expect(resolvePermission('export', { role: 'filler', internal: true })).toBe('hide')
    expect(resolvePermission('manage_share', { role: 'filler', internal: true })).toBe('close')
  })

  it('可查看：全部只读，无新增/删除/结构/配置/导出/分享', () => {
    expect(resolvePermission('edit_cell', { role: 'viewer', internal: true })).toBe('readonly')
    expect(resolvePermission('create_record', { role: 'viewer', internal: true })).toBe('hide')
    expect(resolvePermission('delete_record', { role: 'viewer', internal: true })).toBe('hide')
    expect(resolvePermission('manage_structure', { role: 'viewer', internal: true })).toBe('hide')
    expect(resolvePermission('edit_view', { role: 'viewer', internal: true })).toBe('hide')
    expect(resolvePermission('export', { role: 'viewer', internal: true })).toBe('hide')
    expect(resolvePermission('manage_share', { role: 'viewer', internal: true })).toBe('close')
  })

  it('外部账号：无论角色不渲染导出/新建 Base/Base 管理/分享', () => {
    for (const role of ROLES) {
      for (const action of ['export', 'create_base', 'manage_base', 'manage_share'] as const) {
        expect(resolvePermission(action, { role, internal: false })).toBe('hide')
      }
    }
    expect(resolvePermission('star', { role: 'manager', internal: false })).toBe('hide')
    expect(resolvePermission('manage_structure', { role: 'editor', internal: false })).toBe('hide')
  })

  it('外部账号保留其显式角色的数据能力（上限可编辑）', () => {
    expect(resolvePermission('edit_cell', { role: 'editor', internal: false })).toBe('allow')
    expect(resolvePermission('edit_cell', { role: 'filler', internal: false })).toBe('filter-self')
    expect(resolvePermission('edit_cell', { role: 'viewer', internal: false })).toBe('readonly')
  })
})
