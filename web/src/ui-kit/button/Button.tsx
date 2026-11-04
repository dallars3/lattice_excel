import { forwardRef } from 'react'
import type { ButtonHTMLAttributes } from 'react'

import styles from './Button.module.css'

export type ButtonVariant = 'primary' | 'secondary' | 'danger'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** 视觉变体：主要（primary）/ 次要（secondary）/ 危险（danger），缺省 primary */
  variant?: ButtonVariant
}

// 变体类名映射：BEM modifier（CSS Modules 以 kebab 修饰符命名，经字符串键取用）
const VARIANT_CLASS: Record<ButtonVariant, string> = {
  primary: styles['button--primary'],
  secondary: styles['button--secondary'],
  danger: styles['button--danger'],
}

/** 基础按钮：主要/次要/危险三 variants + 原生 disabled 禁用态；className 与原生 button 属性全部透传。 */
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'primary', type = 'button', className, children, ...rest },
  ref,
) {
  const classes = [styles.button, VARIANT_CLASS[variant], className].filter(Boolean).join(' ')

  return (
    <button ref={ref} type={type} className={classes} {...rest}>
      {children}
    </button>
  )
})
