import { useState } from 'react'

import type { FieldRecord } from '../../api/field'
import { allowedConvertTargets, isOptionFieldType } from '../../types/field-config'

import styles from './FieldPanels.module.css'

export type FieldMenuAction = 'rename' | 'duplicate' | 'config' | 'convert' | 'options' | 'delete'

export interface FieldHeaderRowProps {
  fields: FieldRecord[]
  descriptions: Record<string, string>
  canManageStructure: boolean
  canEditFields: boolean
  atCapacity: boolean
  onCreate: () => void
  onAction: (action: FieldMenuAction, field: FieldRecord) => void
}

/** 表头排序：主字段固定第一列，其余按 position 升序。 */
export function sortFieldsForHeader(fields: readonly FieldRecord[]): FieldRecord[] {
  return [...fields].sort((left, right) => {
    if (left.isPrimary !== right.isPrimary) return left.isPrimary ? -1 : 1
    return left.position - right.position
  })
}

/** 表头：字段名（悬停描述）、按角色裁剪的字段菜单与末列「＋」入口。 */
export function FieldHeaderRow({
  fields,
  descriptions,
  canManageStructure,
  canEditFields,
  atCapacity,
  onCreate,
  onAction,
}: FieldHeaderRowProps) {
  const [openMenuId, setOpenMenuId] = useState<string | null>(null)
  const sorted = sortFieldsForHeader(fields)
  const canOpenMenu = canManageStructure || canEditFields

  function run(action: FieldMenuAction, field: FieldRecord) {
    setOpenMenuId(null)
    onAction(action, field)
  }

  function menuItems(field: FieldRecord) {
    if (field.isPrimary) {
      return (
        <>
          <button type="button" role="menuitem" onClick={() => run('rename', field)}>
            重命名字段
          </button>
          <button type="button" role="menuitem" onClick={() => run('config', field)}>
            字段配置
          </button>
        </>
      )
    }

    return (
      <>
        <button type="button" role="menuitem" onClick={() => run('rename', field)}>
          重命名字段
        </button>
        <button type="button" role="menuitem" onClick={() => run('duplicate', field)}>
          复制字段
        </button>
        <button type="button" role="menuitem" onClick={() => run('config', field)}>
          字段配置
        </button>
        {canManageStructure && allowedConvertTargets(field.type).length > 0 ? (
          <button type="button" role="menuitem" onClick={() => run('convert', field)}>
            更改类型
          </button>
        ) : null}
        {isOptionFieldType(field.type) ? (
          <button type="button" role="menuitem" onClick={() => run('options', field)}>
            维护选项
          </button>
        ) : null}
        {canManageStructure ? (
          <button type="button" role="menuitem" onClick={() => run('delete', field)}>
            删除字段
          </button>
        ) : null}
      </>
    )
  }

  return (
    <div role="row" aria-label="字段表头" className={styles.header}>
      {sorted.map((field) => {
        const description = descriptions[field.id] ?? field.description
        return (
          <div key={field.id} role="columnheader" aria-label={`字段：${field.name}`} className={styles.fieldCell}>
            <span
              className={styles.fieldName}
              title={description === '' ? undefined : description}
            >
              {field.name}
              {field.isPrimary ? <span className={styles.primaryBadge}>主字段</span> : null}
            </span>
            {canOpenMenu ? (
              <>
                <button
                  type="button"
                  className={styles.menuButton}
                  aria-label={`字段操作：${field.name}`}
                  aria-haspopup="menu"
                  aria-expanded={openMenuId === field.id}
                  onClick={() =>
                    setOpenMenuId((current) => (current === field.id ? null : field.id))
                  }
                >
                  ⋯
                </button>
                {openMenuId === field.id ? (
                  <div role="menu" aria-label={`${field.name} 字段菜单`} className={styles.menu}>
                    {menuItems(field)}
                  </div>
                ) : null}
              </>
            ) : null}
          </div>
        )
      })}
      {canManageStructure ? (
        <button
          type="button"
          className={styles.plusButton}
          aria-label="新增字段"
          title={atCapacity ? '字段数已达上限（100）' : undefined}
          disabled={atCapacity}
          onClick={onCreate}
        >
          ＋
        </button>
      ) : null}
    </div>
  )
}
