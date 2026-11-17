// 弹层 portal（票 01 / BUG-06）：选项面板/日历/行内错误统一渲染到 document.body，
// 脱离网格单元格 overflow:hidden 裁剪上下文；以编辑器宿主（触发单元格）为锚定位，
// 视口边缘自动翻转方向（下方放不下翻到上方），水平越界收拢回视口内。
// 层级沿用 --z-index-dropdown（弹层类名自带），最小宽度由调用方类名+锚宽共同约束。
import { useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { createPortal } from 'react-dom'

/** 弹层与锚的间隙：与既有 top: calc(100% + var(--spacing-2xs)) 的 2px 同口径。 */
export const EDITOR_POPUP_GAP_PX = 2

/** 弹层与视口边缘的最小留白。 */
export const EDITOR_POPUP_VIEWPORT_MARGIN_PX = 8

/**
 * 弹层归属标记（portal 化后弹层不在编辑器宿主 DOM 子树内）：
 * 失焦提交缝（GridArea 编辑宿主 onBlur）据此把「焦点移入本编辑器弹层」
 * 视为仍在编辑器内，不触发提交——保持「焦点真正离开编辑器宿主才提交」语义不变。
 */
export const EDITOR_POPUP_ATTR = 'data-grid-editor-portal'

/** 弹层尺寸（px）。 */
export interface PopupSize {
  readonly width: number
  readonly height: number
}

/** 弹层最终定位（视口坐标，配 position:fixed）。 */
export interface PopupPlacement {
  readonly top: number
  readonly left: number
  /** true = 已翻转到锚上方。 */
  readonly flipped: boolean
}

/** 锚参与定位的几何量（getBoundingClientRect 的子集）。 */
export interface PopupAnchorRect {
  readonly top: number
  readonly bottom: number
  readonly left: number
}

/**
 * 纯定位函数：默认锚下方 + 左对齐；下方放不下且上方空间更大时翻转锚上方；
 * 上下都放不下时保持下方（best effort）；水平右溢收拢、不越出左右边距。
 */
export function resolvePopupPlacement(
  anchor: PopupAnchorRect,
  popup: PopupSize,
  viewport: PopupSize,
): PopupPlacement {
  const margin = EDITOR_POPUP_VIEWPORT_MARGIN_PX
  const gap = EDITOR_POPUP_GAP_PX
  const spaceBelow = viewport.height - anchor.bottom - gap
  const spaceAbove = anchor.top - gap
  const flipped = spaceBelow < popup.height && spaceAbove > spaceBelow
  const unclampedTop = flipped ? anchor.top - gap - popup.height : anchor.bottom + gap
  const top = Math.max(margin, unclampedTop)
  let left = anchor.left
  if (left + popup.width > viewport.width - margin) {
    left = viewport.width - margin - popup.width
  }
  if (left < margin) left = margin
  return { top, left, flipped }
}

interface EditorPopupProps {
  /** 锚元素（编辑器宿主）；null 时经回调 ref 置入后的重渲染再挂载弹层。 */
  readonly anchor: HTMLElement | null
  /** 弹层类名（承载既有视觉样式与 --z-index-dropdown 层级）。 */
  readonly className?: string
  readonly role?: string
  readonly ariaLabel?: string
  readonly id?: string
  /** 最小宽度下限（px）；实际 minWidth = max(下限, 锚宽)，替代 portal 前的 100% 锚宽语义。 */
  readonly minWidth?: number
  readonly children: ReactNode
}

/**
 * 编辑器弹层容器：portal 到 document.body，position:fixed 锚定锚元素。
 * 首帧以 visibility:hidden 预挂测量尺寸，layout effect 内定位后可见；
 * 期间滚动（含网格滚动容器）与视口尺寸变化会重新定位。
 */
export function EditorPopup({
  anchor,
  className,
  role,
  ariaLabel,
  id,
  minWidth,
  children,
}: EditorPopupProps) {
  const [placement, setPlacement] = useState<PopupPlacement | null>(null)
  const popupRef = useRef<HTMLDivElement | null>(null)

  useLayoutEffect(() => {
    if (anchor === null) return undefined
    const measure = () => {
      const popup = popupRef.current
      if (popup === null) return
      setPlacement(
        resolvePopupPlacement(
          anchor.getBoundingClientRect(),
          popup.getBoundingClientRect(),
          { width: window.innerWidth, height: window.innerHeight },
        ),
      )
    }
    measure()
    // capture 捕获任意祖先滚动容器（scroll 事件不冒泡但走捕获阶段）。
    window.addEventListener('scroll', measure, true)
    window.addEventListener('resize', measure)
    return () => {
      window.removeEventListener('scroll', measure, true)
      window.removeEventListener('resize', measure)
    }
  }, [anchor])

  if (anchor === null) return null

  const anchorRect = anchor.getBoundingClientRect()
  const style: CSSProperties = {
    position: 'fixed',
    visibility: placement === null ? 'hidden' : 'visible',
    top: placement === null ? anchorRect.bottom + EDITOR_POPUP_GAP_PX : placement.top,
    left: placement === null ? anchorRect.left : placement.left,
    ...(minWidth === undefined ? undefined : { minWidth: Math.max(minWidth, anchorRect.width) }),
  }

  return createPortal(
    <div
      ref={popupRef}
      id={id}
      role={role}
      aria-label={ariaLabel}
      className={className}
      style={style}
      {...{ [EDITOR_POPUP_ATTR]: '' }}
    >
      {children}
    </div>,
    document.body,
  )
}
