import type { ReactNode } from 'react'

import styles from './Banner.module.css'

export type BannerTone = 'info' | 'warning'

export interface BannerProps {
  /** 语义色调：info=信息（role=status）/ warning=警告（role=alert），缺省 info */
  tone?: BannerTone
  /** 图标位：缺省渲染内置内联 SVG 图标（无外部资源） */
  icon?: ReactNode
  /** 文案位 */
  children: ReactNode
  /** 透传以覆盖样式（技术方案第 11 章） */
  className?: string
}

// 变体类名映射：BEM modifier
const TONE_CLASS: Record<BannerTone, string> = {
  info: styles['banner--info'],
  warning: styles['banner--warning'],
}

/** 基础横幅：信息/警告两种语义，含图标位与文案位。 */
export function Banner({ tone = 'info', icon, children, className }: BannerProps) {
  const classes = [styles.banner, TONE_CLASS[tone], className].filter(Boolean).join(' ')

  return (
    <div role={tone === 'warning' ? 'alert' : 'status'} className={classes}>
      <span className={styles.banner__icon} aria-hidden="true">
        {icon ?? <DefaultIcon tone={tone} />}
      </span>
      <div className={styles.banner__content}>{children}</div>
    </div>
  )
}

/* 内置默认图标：内联 SVG，颜色走 currentColor（由 tone 类名提供 token 色）。 */
function DefaultIcon({ tone }: { tone: BannerTone }) {
  if (tone === 'warning') {
    return (
      <svg viewBox="0 0 16 16" width="1em" height="1em" fill="none" aria-hidden="true" focusable="false">
        <path
          d="M8 1.75 14.75 13.75H1.25L8 1.75Z"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
        <rect x="7.25" y="6" width="1.5" height="4" rx="0.75" fill="currentColor" />
        <circle cx="8" cy="12.25" r="0.875" fill="currentColor" />
      </svg>
    )
  }

  return (
    <svg viewBox="0 0 16 16" width="1em" height="1em" fill="none" aria-hidden="true" focusable="false">
      <circle cx="8" cy="8" r="6.75" stroke="currentColor" strokeWidth="1.5" />
      <rect x="7.25" y="7" width="1.5" height="4.5" rx="0.75" fill="currentColor" />
      <circle cx="8" cy="4.75" r="0.875" fill="currentColor" />
    </svg>
  )
}
