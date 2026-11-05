import { useState } from 'react'

import { PermissionGate, usePermission } from '@/features/permissions'

import type { BaseSummary } from '../api'
import { formatModifiedAt } from '../format'

import styles from './BaseCard.module.css'

export interface BaseCardProps {
  base: BaseSummary
  onOpen: (base: BaseSummary) => void
  onRename: (base: BaseSummary) => void
  onDuplicate: (base: BaseSummary) => void
  onDelete: (base: BaseSummary) => void
  onToggleStar: (base: BaseSummary) => void
}

/** 首页 Base 卡片：名称/表数/最近修改时间与修改人/星标状态；菜单按权限显隐。 */
export function BaseCard({
  base,
  onOpen,
  onRename,
  onDuplicate,
  onDelete,
  onToggleStar,
}: BaseCardProps) {
  const [menuOpen, setMenuOpen] = useState(false)
  const permission = usePermission(base)
  const canManage = permission.resolve('manage_base') === 'allow'
  const canStar = permission.resolve('star') === 'allow'
  const showMenu = permission.internal && (canManage || canStar)

  function run(action: () => void) {
    setMenuOpen(false)
    action()
  }

  return (
    <article className={styles.card} aria-label={`多维表格：${base.name}`}>
      <div className={styles.header}>
        <button
          type="button"
          className={styles.name}
          aria-label={`打开多维表格：${base.name}`}
          onClick={() => onOpen(base)}
        >
          {base.name}
        </button>
        {base.starred ? (
          <span className={styles.starBadge} aria-label="已星标">
            ★
          </span>
        ) : null}
      </div>
      <div className={styles.meta}>
        <span>{base.tableCount} 个数据表</span>
        <span>最近修改：{formatModifiedAt(base.lastModifiedAt)}</span>
        <span className={styles.modifier}>{base.lastModifier === '' ? '—' : base.lastModifier}</span>
      </div>
      {showMenu ? (
        <div className={styles.menuWrap}>
          <button
            type="button"
            className={styles.moreButton}
            aria-label={`更多操作：${base.name}`}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((open) => !open)}
          >
            ⋯
          </button>
          {menuOpen ? (
            <div className={styles.menu} role="menu" aria-label={`${base.name} 操作菜单`}>
              <PermissionGate state={permission.resolve('manage_base')}>
                <button type="button" role="menuitem" onClick={() => run(() => onRename(base))}>
                  重命名
                </button>
                <button type="button" role="menuitem" onClick={() => run(() => onDuplicate(base))}>
                  复制
                </button>
                <button type="button" role="menuitem" onClick={() => run(() => onDelete(base))}>
                  删除
                </button>
              </PermissionGate>
              <PermissionGate state={permission.resolve('star')}>
                <button type="button" role="menuitem" onClick={() => run(() => onToggleStar(base))}>
                  {base.starred ? '取消星标' : '星标'}
                </button>
              </PermissionGate>
            </div>
          ) : null}
        </div>
      ) : null}
    </article>
  )
}
