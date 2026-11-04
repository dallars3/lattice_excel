import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'

import styles from './Toast.module.css'

/** 默认自动消失时长：合理默认值（3s），可经 ToastOptions.duration 覆盖。 */
export const DEFAULT_TOAST_DURATION_MS = 3000

export interface ToastOptions {
  /** 自动消失时长（毫秒），缺省 DEFAULT_TOAST_DURATION_MS */
  duration?: number
}

interface ToastItem {
  id: number
  message: ReactNode
  duration: number
}

interface ToastContextValue {
  /** 入栈一条 toast，返回其 id */
  push: (message: ReactNode, options?: ToastOptions) => number
  /** 按 id 手动移除 */
  dismiss: (id: number) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

/** 取用 toast 入栈/移除能力；必须在 &lt;ToastProvider&gt; 内使用。 */
export function useToast(): ToastContextValue {
  const value = useContext(ToastContext)
  if (value === null) {
    throw new Error('useToast 必须在 <ToastProvider> 内使用')
  }

  return value
}

/**
 * Toast 提供者：入栈、自动消失（每条 toast 独立计时，新 toast 入栈不影响旧 toast 计时）、
 * 手动关闭按钮。渲染层经 portal 挂 document.body（DOM 方案自定，ui-kit 无业务语义）。
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const nextIdRef = useRef(0)

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }, [])

  const push = useCallback((message: ReactNode, options?: ToastOptions) => {
    nextIdRef.current += 1
    const id = nextIdRef.current
    setToasts((prev) => [
      ...prev,
      { id, message, duration: options?.duration ?? DEFAULT_TOAST_DURATION_MS },
    ])

    return id
  }, [])

  const value = useMemo(() => ({ push, dismiss }), [push, dismiss])

  return (
    <ToastContext.Provider value={value}>
      {children}
      {createPortal(
        <div className={styles.toastStack}>
          {toasts.map((toast) => (
            <ToastItemView key={toast.id} toast={toast} onDismiss={dismiss} />
          ))}
        </div>,
        document.body,
      )}
    </ToastContext.Provider>
  )
}

/**
 * 单条 toast：挂载即启动自己的自动消失计时器；
 * 依赖仅 toast 对象（入栈时创建、引用稳定），新 toast 入栈不会重启本条计时器。
 */
function ToastItemView({ toast, onDismiss }: { toast: ToastItem; onDismiss: (id: number) => void }) {
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(toast.id), toast.duration)

    return () => window.clearTimeout(timer)
  }, [toast, onDismiss])

  return (
    <div role="status" className={styles.toast}>
      <div className={styles.toast__message}>{toast.message}</div>
      <button type="button" className={styles.toast__close} aria-label="关闭提示" onClick={() => onDismiss(toast.id)}>
        <CloseIcon />
      </button>
    </div>
  )
}

/* 手动关闭图标：内联 SVG，颜色走 currentColor。 */
function CloseIcon() {
  return (
    <svg viewBox="0 0 12 12" width="1em" height="1em" fill="none" aria-hidden="true" focusable="false">
      <path d="m2.5 2.5 7 7m0-7-7 7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}
