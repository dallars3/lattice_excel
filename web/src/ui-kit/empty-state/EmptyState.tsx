import type { ReactNode } from 'react'

import { Button } from '../button/Button'

import styles from './EmptyState.module.css'

export interface EmptyStateAction {
  /** 操作按钮文案 */
  label: string
  onClick: () => void
}

export interface EmptyStateProps {
  /** 主文案 */
  title: string
  /** 补充描述，缺省不渲染 */
  description?: string
  /** 插图位：缺省使用内置内联 SVG（禁止外部图片/CDN 资源） */
  illustration?: ReactNode
  /** 可选操作按钮，缺省不渲染 */
  action?: EmptyStateAction
  /** 透传以覆盖样式（技术方案第 11 章） */
  className?: string
}

/** 空态基础件：插图（内联 SVG）+ 文案 + 可选操作按钮。 */
export function EmptyState({ title, description, illustration, action, className }: EmptyStateProps) {
  const classes = [styles.emptyState, className].filter(Boolean).join(' ')

  return (
    <div className={classes}>
      <div className={styles.emptyState__illustration} aria-hidden="true">
        {illustration ?? <DefaultIllustration />}
      </div>
      <p className={styles.emptyState__title}>{title}</p>
      {description === undefined ? null : <p className={styles.emptyState__description}>{description}</p>}
      {action === undefined ? null : (
        <div className={styles.emptyState__action}>
          <Button variant="secondary" onClick={action.onClick}>
            {action.label}
          </Button>
        </div>
      )}
    </div>
  )
}

/* 内置默认插图：内联 SVG（空表格 + 检索镜），颜色走 currentColor（由 CSS 提供 token 色）。 */
function DefaultIllustration() {
  return (
    <svg viewBox="0 0 120 90" width="7.5em" height="5.5em" fill="none" aria-hidden="true" focusable="false">
      <rect x="4" y="4" width="112" height="82" rx="10" stroke="currentColor" strokeWidth="2" opacity="0.35" />
      <line x1="4" y1="24" x2="116" y2="24" stroke="currentColor" strokeWidth="2" opacity="0.35" />
      <line x1="44" y1="24" x2="44" y2="86" stroke="currentColor" strokeWidth="2" opacity="0.35" />
      <line x1="84" y1="24" x2="84" y2="86" stroke="currentColor" strokeWidth="2" opacity="0.35" />
      <circle cx="52" cy="50" r="12" stroke="currentColor" strokeWidth="3" />
      <path d="m61 59 12 12" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}
