import { useRef, useState } from 'react'

import type { FieldRecord } from '../../api/field'
import { allowedConvertTargets, isOptionFieldType } from '../../types/field-config'
import { clampFieldWidth } from '../../types/view-config'

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
  /** 列宽表（与行单元格同序同宽；缺省 160px，票 05）。 */
  columnWidths?: readonly number[]
  /** 冻结列数（默认冻结主字段所在首列）；冻结单元格 sticky left。 */
  frozenCount?: number
  /** 列宽拖拽松手（票 07）：返回 clamp 后的宽度，由上层写入视图 field_width_map。 */
  onColumnResize?: (fieldId: string, width: number) => void
  /** 表头置于纵向滚动容器内时 sticky top。 */
  sticky?: boolean
}

const DEFAULT_COLUMN_WIDTH = 160
const PLUS_COLUMN_WIDTH = 48

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
  columnWidths,
  frozenCount = 0,
  onColumnResize,
  sticky = false,
}: FieldHeaderRowProps) {
  const [openMenuId, setOpenMenuId] = useState<string | null>(null)
  const [resizing, setResizing] = useState<{ fieldId: string; width: number } | null>(null)
  const resizeRef = useRef<{
    fieldId: string
    startX: number
    startWidth: number
    currentWidth: number
  } | null>(null)
  const sorted = sortFieldsForHeader(fields)
  const canOpenMenu = canManageStructure || canEditFields
  const widths = sorted.map((field, index) =>
    resizing !== null && resizing.fieldId === field.id
      ? resizing.width
      : columnWidths?.[index] ?? DEFAULT_COLUMN_WIDTH,
  )
  const frozenOffsets: number[] = []
  let frozenOffset = 0
  for (let index = 0; index < sorted.length; index += 1) {
    frozenOffsets[index] = frozenOffset
    if (index < frozenCount) frozenOffset += widths[index] ?? DEFAULT_COLUMN_WIDTH
  }
  const totalWidth =
    widths.reduce((sum, width) => sum + width, 0) +
    (canManageStructure ? PLUS_COLUMN_WIDTH : 0)

  function run(action: FieldMenuAction, field: FieldRecord) {
    setOpenMenuId(null)
    onAction(action, field)
  }

  function startResize(
    field: FieldRecord,
    startWidth: number,
    event: { preventDefault: () => void; clientX?: number },
  ) {
    if (onColumnResize === undefined || resizeRef.current !== null) return
    event.preventDefault()
    const startX = event.clientX ?? 0
    resizeRef.current = {
      fieldId: field.id,
      startX,
      startWidth,
      currentWidth: startWidth,
    }
    setResizing({ fieldId: field.id, width: startWidth })

    const moveTo = (clientX: number) => {
      const drag = resizeRef.current
      if (drag === null) return
      const width = clampFieldWidth(drag.startWidth + clientX - drag.startX)
      drag.currentWidth = width
      setResizing({ fieldId: drag.fieldId, width })
    }
    const onMouseMove = (moveEvent: MouseEvent) => moveTo(moveEvent.clientX)
    const onPointerMove = (moveEvent: PointerEvent) => moveTo(moveEvent.clientX)

    const finish = () => {
      const drag = resizeRef.current
      resizeRef.current = null
      setResizing(null)
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', finish)
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', finish)
      if (drag !== null && drag.currentWidth !== drag.startWidth) {
        onColumnResize(drag.fieldId, drag.currentWidth)
      }
    }

    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', finish)
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', finish)
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
    <div
      role="row"
      aria-label="字段表头"
      className={sticky ? `${styles.header} ${styles.headerSticky}` : styles.header}
      style={columnWidths === undefined ? undefined : { width: totalWidth, minWidth: totalWidth }}
    >
      {sorted.map((field, index) => {
        const description = descriptions[field.id] ?? field.description
        const frozen = index < frozenCount
        return (
          <div
            key={field.id}
            role="columnheader"
            aria-label={`字段：${field.name}`}
            className={frozen ? `${styles.fieldCell} ${styles.fieldCellFrozen}` : styles.fieldCell}
            style={{
              width: widths[index],
              flex: `0 0 ${widths[index]}px`,
              left: frozen ? frozenOffsets[index] : undefined,
            }}
          >
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
            {onColumnResize === undefined ? null : (
              <span
                role="separator"
                aria-orientation="vertical"
                aria-label={`拖拽调整列宽：${field.name}`}
                className={styles.resizeHandle}
                onMouseDown={(event) => startResize(field, widths[index] ?? DEFAULT_COLUMN_WIDTH, event)}
                onPointerDown={(event) =>
                  startResize(field, widths[index] ?? DEFAULT_COLUMN_WIDTH, event)
                }
              />
            )}
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
          style={columnWidths === undefined ? undefined : { width: PLUS_COLUMN_WIDTH, flex: `0 0 ${PLUS_COLUMN_WIDTH}px` }}
          onClick={onCreate}
        >
          ＋
        </button>
      ) : null}
    </div>
  )
}
