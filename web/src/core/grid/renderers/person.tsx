// person 渲染器（票 05）：需求 4.3 人员行「头像 + 显示名（多人并列）」。
// user_id → {name, disabled} 由网格元数据（H-63 协作者）注入；未知成员回退 user_id 并灰显，
// 停用成员灰显并提示。头像用首字色块（不引入外部资源）。
import { defineRenderer } from '../registry'

import styles from './m2.module.css'

export interface PersonInfo {
  readonly name: string
  readonly disabled?: boolean
}

export interface PersonCellConfig {
  readonly allow_multiple?: boolean
  /** user_id → 人员信息（GridArea 注入）。 */
  readonly users?: Readonly<Record<string, PersonInfo>>
}

const AVATAR_COLORS = ['#3370ff', '#2ea121', '#ff8800', '#7b61ff', '#f54a45', '#0fc0d9'] as const

function asList(value: unknown): string[] {
  const list = Array.isArray(value) ? value : [value]
  return list
    .filter((item) => item !== null && item !== undefined && item !== '')
    .map((item) => String(item))
}

function avatarColor(userId: string): string {
  let hash = 0
  for (let i = 0; i < userId.length; i += 1) {
    hash = (hash * 31 + userId.charCodeAt(i)) >>> 0
  }
  return AVATAR_COLORS[hash % AVATAR_COLORS.length]
}

function PersonCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  const ids = asList(value)
  if (ids.length === 0) {
    return <span className={styles.person} />
  }
  const users = ((config ?? {}) as PersonCellConfig).users ?? {}
  return (
    <span className={styles.person}>
      {ids.map((userId, index) => {
        const info = users[userId]
        const name = info?.name ?? userId
        const disabled = info?.disabled === true
        const unknown = info === undefined
        const initial = Array.from(name)[0] ?? '?'
        return (
          <span
            key={`${userId}-${index}`}
            className={`${styles.person__item} ${disabled || unknown ? styles['person__item--muted'] : ''}`}
            data-disabled={disabled ? 'true' : undefined}
            data-unknown={unknown ? 'true' : undefined}
            title={disabled ? '已停用成员' : unknown ? '未知成员' : name}
          >
            <span
              className={`${styles.avatar} ${styles['avatar--sm']}`}
              style={{
                backgroundColor:
                  disabled || unknown ? 'var(--color-text-disabled)' : avatarColor(userId),
              }}
              aria-hidden="true"
            >
              {initial}
            </span>
            <span className={styles.person__name}>{name}</span>
          </span>
        )
      })}
    </span>
  )
}

/** 人员字段渲染器：头像 + 显示名，多人并列。 */
export const PERSON_RENDERER = defineRenderer<string, PersonCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: PersonCell,
})
