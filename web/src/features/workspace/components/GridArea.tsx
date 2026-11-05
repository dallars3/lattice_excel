import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { UIEvent } from 'react'

import { useQueryClient } from '@tanstack/react-query'

import {
  LoadedRangeCache,
  RowHeightModel,
  computeWindow,
  createCellRegistry,
  registerM2Cells,
  type CellRendererDef,
} from '@/core/grid'
import { usePersonDirectory, useRelationMetadata, type RelationMetadata } from '@/features/records/metadata'
import { useRecords } from '@/features/records/useRecords'
import { fieldKeys, recordsKeys, viewKeys } from '@/query-keys'
import { trackWrite, workspaceStore } from '@/stores'
import { Button, EmptyState, useOptionalToast } from '@/ui-kit'

import {
  convertField,
  createField,
  deleteField,
  duplicateField,
  renameField,
  updateFieldConfig,
  updateFieldOptions,
  type CreateFieldInput,
  type FieldRecord,
} from '../api/field'
import type { TableSummary } from '../api/table'
import {
  fetchViewConfigRaw,
  removeFieldFromViews,
  updateViewConfigRaw,
} from '../api/view-config-raw'
import type { ViewSummary } from '../api/view'
import { apiErrorText, apiErrorTitle } from '../errorText'
import type { FieldOptionValue, FieldType } from '../types/field-config'
import { FieldConfigPanel } from './field/FieldConfigPanel'
import { FieldConvertDialog } from './field/FieldConvertDialog'
import { FieldCreateDialog } from './field/FieldCreateDialog'
import { FieldDeleteDialog } from './field/FieldDeleteDialog'
import { FieldHeaderRow, sortFieldsForHeader, type FieldMenuAction } from './field/FieldHeaderRow'
import { FieldRenameDialog } from './field/FieldRenameDialog'
import { OptionsEditor } from './field/OptionsEditor'
import {
  buildDeleteImpact,
  isFieldCapacityReached,
  type DeleteFieldImpactView,
} from './field/impact'
import styles from './field/FieldPanels.module.css'
import gridStyles from './GridArea.module.css'

export interface GridAreaProps {
  tableId: string | null
  /** 当前 Base；缺省时不请求记录（孤立表头测试/字段面板场景）。 */
  baseId?: string
  /** 当前视图；与 tableId 一起作为 H-48 查询标识。 */
  viewId?: string | null
  fields: FieldRecord[]
  fieldsLoading?: boolean
  fieldsError?: unknown
  onRetryFields?: () => void
  /** 可管理（manage_structure=allow）：新增/删除/类型转换入口。 */
  canManageStructure: boolean
  /** 可编辑及以上：重命名/配置/复制/选项入口（删除与类型转换仍仅可管理）。 */
  canEditFields: boolean
  tables?: TableSummary[]
  views?: ViewSummary[]
  /** 票 04 删除字段影响面兜底（无真实记录上下文时的已加载条数）。 */
  loadedRecordCount?: number
  hasMoreRecords?: boolean
  /** 票 07 接线预留：筛选无结果空态。 */
  hasActiveFilters?: boolean
  onClearFilters?: () => void
  /** 票 07 接线预留：全部字段隐藏空态。 */
  allFieldsHidden?: boolean
  onRestoreFields?: () => void
  /** 底栏计数（G-3：不虚构总数）。 */
  onRecordCountChange?: (count: number, hasMore: boolean) => void
  /** 关联/引用记录跳转（目标表 + 记录）。 */
  onOpenRelation?: (tableId: string, recordId: string) => void
}

/** 默认列宽（spec 字段宽度裁定：未出现于 field_width_map 的字段用 160px，票 07 接视图配置）。 */
const DEFAULT_COLUMN_WIDTH = 160
/** 表头高度（虚拟窗口纵向偏移补偿）。 */
const GRID_HEADER_HEIGHT = 36
/** jsdom 无布局环境下的视口兜底（与 GridDemoPage 一致）。 */
const FALLBACK_VIEWPORT = { width: 800, height: 600 }
/** 距已加载末尾剩余行数 ≤ 该值时预取下一页。 */
const FETCH_AHEAD_ROWS = 5

function cx(...parts: ReadonlyArray<string | false | undefined>): string {
  return parts.filter((part) => part !== undefined && part !== '').join(' ')
}

interface RecordCellProps {
  readonly recordId: string
  readonly field: FieldRecord
  readonly value: unknown
  readonly config: unknown
  readonly renderer: CellRendererDef
  readonly width: number
  readonly frozen: boolean
  readonly selected: boolean
  readonly onSelect: (recordId: string, fieldId: string) => void
  readonly onOpenRecord?: (recordId: string) => void
}

/** 单个记录单元格（memo 化：窗口滚动/选区变化只重渲染受影响组件）。 */
const RecordCell = memo(function RecordCell({
  recordId,
  field,
  value,
  config,
  renderer,
  width,
  frozen,
  selected,
  onSelect,
  onOpenRecord,
}: RecordCellProps) {
  const Cell = renderer.component
  const alignClass =
    renderer.align === 'right'
      ? gridStyles['gridCell--right']
      : renderer.align === 'center'
        ? gridStyles['gridCell--center']
        : undefined
  return (
    <div
      role="gridcell"
      className={cx(
        gridStyles.gridCell,
        alignClass,
        frozen ? gridStyles.gridCellFrozen : undefined,
        selected ? gridStyles.gridCellSelected : undefined,
        selected ? gridStyles.gridCellFocused : undefined,
      )}
      style={{ width, flex: `0 0 ${width}px` }}
      aria-selected={selected}
      aria-label={`${field.name}（${recordId}）`}
      tabIndex={selected ? 0 : -1}
      data-testid={`cell-${recordId}-${field.id}`}
      onClick={() => onSelect(recordId, field.id)}
    >
      <Cell value={value} config={config} onOpenRecord={onOpenRecord} />
    </div>
  )
})

type DialogState =
  | { kind: 'create' }
  | { kind: 'rename'; field: FieldRecord }
  | { kind: 'config'; field: FieldRecord }
  | { kind: 'convert'; field: FieldRecord }
  | { kind: 'options'; field: FieldRecord }
  | { kind: 'delete'; field: FieldRecord }

/** 网格区：字段表头（按 position 排序、主字段首列、按角色裁剪菜单）+ 记录骨架（票 05/06 扩展）。 */
export function GridArea({
  tableId,
  baseId,
  viewId = null,
  fields,
  fieldsLoading = false,
  fieldsError,
  onRetryFields,
  canManageStructure,
  canEditFields,
  tables = [],
  views = [],
  loadedRecordCount = 0,
  hasMoreRecords = false,
  hasActiveFilters = false,
  onClearFilters,
  allFieldsHidden = false,
  onRestoreFields,
  onRecordCountChange,
  onOpenRelation,
}: GridAreaProps) {
  const queryClient = useQueryClient()
  const toast = useOptionalToast()
  const descriptions = workspaceStore((state) => state.fieldDescriptions)
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const atCapacity = isFieldCapacityReached(fields.length)
  const fieldsFailed = fieldsError !== undefined && fieldsError !== null
  const sortedFields = useMemo(() => sortFieldsForHeader(fields), [fields])
  const fieldsById = useMemo(() => {
    const map = new Map<string, FieldRecord>()
    for (const field of fields) map.set(field.id, field)
    return map
  }, [fields])

  const recordsReady =
    baseId !== undefined &&
    baseId !== '' &&
    tableId !== null &&
    viewId !== null &&
    !fieldsLoading &&
    !fieldsFailed &&
    !allFieldsHidden &&
    fields.length > 0

  const records = useRecords({
    baseId: baseId ?? '',
    tableId,
    viewId,
    enabled: recordsReady,
  })

  const personFields = useMemo(
    () => sortedFields.filter((field) => field.type === 'person'),
    [sortedFields],
  )
  const relationFields = useMemo(
    () => sortedFields.filter((field) => field.type === 'relation'),
    [sortedFields],
  )
  const lookupFields = useMemo(
    () => sortedFields.filter((field) => field.type === 'lookup'),
    [sortedFields],
  )
  const targetTableIds = useMemo(() => {
    const ids: string[] = []
    for (const field of relationFields) {
      const target = field.configMap.target_table_id
      if (typeof target === 'string' && target !== '') ids.push(target)
    }
    for (const field of lookupFields) {
      const relationId = field.configMap.relation_field_id
      const relation = typeof relationId === 'string' ? fieldsById.get(relationId) : undefined
      const target = relation?.configMap.target_table_id
      if (typeof target === 'string' && target !== '') ids.push(target)
    }
    return ids
  }, [fieldsById, lookupFields, relationFields])

  const personQuery = usePersonDirectory(
    baseId ?? '',
    baseId !== undefined && personFields.length > 0 && !fieldsFailed,
  )
  const relationQuery = useRelationMetadata(
    baseId ?? '',
    targetTableIds,
    baseId !== undefined && targetTableIds.length > 0 && !fieldsFailed,
  )
  const personDirectory = personQuery.data ?? {}
  const relationMetadata: RelationMetadata = relationQuery.data ?? {}

  const registry = useMemo(() => {
    const instance = createCellRegistry()
    registerM2Cells(instance)
    return instance
  }, [])

  const renderers = useMemo(() => {
    const map: Record<string, CellRendererDef> = {}
    for (const field of sortedFields) {
      map[field.id] = registry.queryRenderer(field.type)
    }
    return map
  }, [registry, sortedFields])

  const columnWidths = useMemo(
    () => sortedFields.map(() => DEFAULT_COLUMN_WIDTH),
    [sortedFields],
  )

  const cellConfigs = useMemo(() => {
    const configs: Record<string, unknown> = {}

    const targetConfigFor = (targetField: FieldRecord): unknown => {
      if (targetField.type === 'person') {
        return { ...targetField.configMap, users: personDirectory }
      }
      if (targetField.type === 'relation') {
        const targetTableId = String(targetField.configMap.target_table_id ?? '')
        return {
          ...targetField.configMap,
          labels: relationMetadata[targetTableId]?.labels ?? {},
        }
      }
      if (targetField.type === 'attachment') {
        return { ...targetField.configMap, files: targetField.configMap.files ?? {} }
      }
      return targetField.configMap
    }

    for (const field of sortedFields) {
      switch (field.type) {
        case 'person':
          configs[field.id] = { ...field.configMap, users: personDirectory }
          break
        case 'relation': {
          const targetTableId = String(field.configMap.target_table_id ?? '')
          configs[field.id] = {
            ...field.configMap,
            labels: relationMetadata[targetTableId]?.labels ?? {},
          }
          break
        }
        case 'lookup': {
          const relationFieldId = field.configMap.relation_field_id
          const relationField =
            typeof relationFieldId === 'string' ? fieldsById.get(relationFieldId) : undefined
          const sourceTableId = String(relationField?.configMap.target_table_id ?? '')
          const targetFieldId = field.configMap.target_field_id
          const targetField = relationMetadata[sourceTableId]?.fields.find(
            (candidate) => candidate.id === targetFieldId,
          )
          configs[field.id] = {
            targetType: targetField?.type ?? 'text',
            targetConfig: targetField === undefined ? {} : targetConfigFor(targetField),
          }
          break
        }
        default:
          configs[field.id] = field.configMap
          break
      }
    }
    return configs
  }, [fieldsById, personDirectory, relationMetadata, sortedFields])

  const openCallbacks = useMemo(() => {
    const callbacks: Record<string, (recordId: string) => void> = {}
    if (onOpenRelation === undefined) return callbacks

    const lookupTargetTableId = (lookupField: FieldRecord): string => {
      const relationFieldId = lookupField.configMap.relation_field_id
      const relationField =
        typeof relationFieldId === 'string' ? fieldsById.get(relationFieldId) : undefined
      const sourceTableId = String(relationField?.configMap.target_table_id ?? '')
      const targetFieldId = lookupField.configMap.target_field_id
      const targetField = relationMetadata[sourceTableId]?.fields.find(
        (candidate) => candidate.id === targetFieldId,
      )
      if (targetField?.type === 'relation') {
        return String(targetField.configMap.target_table_id ?? '')
      }
      return sourceTableId
    }

    for (const field of sortedFields) {
      let targetTableId = ''
      if (field.type === 'relation') {
        targetTableId = String(field.configMap.target_table_id ?? '')
      } else if (field.type === 'lookup') {
        targetTableId = lookupTargetTableId(field)
      }
      if (targetTableId === '') continue
      callbacks[field.id] = (recordId: string) => onOpenRelation(targetTableId, recordId)
    }
    return callbacks
  }, [fieldsById, onOpenRelation, relationMetadata, sortedFields])

  const [activeCell, setActiveCell] = useState<{ recordId: string; fieldId: string } | null>(null)
  const handleSelectCell = useCallback((recordId: string, fieldId: string) => {
    setActiveCell({ recordId, fieldId })
  }, [])

  const rangeCache = useRef(new LoadedRangeCache())
  useEffect(() => {
    let startIndex = 0
    for (const page of records.pages) {
      rangeCache.current.put({
        cursor: String(page.nextCursor ?? `page-${startIndex}`),
        startIndex,
        endIndex: startIndex + page.items.length - 1,
        recordIds: page.items.map((record) => record.id),
      })
      startIndex += page.items.length
    }
  }, [records.pages])

  const viewportRef = useRef<HTMLDivElement | null>(null)
  const [measured, setMeasured] = useState({ width: 0, height: 0 })
  const [scrollTop, setScrollTop] = useState(0)

  useLayoutEffect(() => {
    const element = viewportRef.current
    if (element === null) return undefined
    const measure = () => setMeasured({ width: element.clientWidth, height: element.clientHeight })
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [])

  const viewport =
    measured.width > 0 && measured.height > 0 ? measured : FALLBACK_VIEWPORT
  const viewportHeight = viewport.height
  const rowModel = useMemo(() => new RowHeightModel('medium'), [])
  const rowSizes = useMemo(
    () => rowModel.sizes(records.rows.length),
    [rowModel, records.rows.length],
  )
  const rowWindow = useMemo(
    () =>
      computeWindow({
        sizes: rowSizes,
        scrollOffset: Math.max(0, scrollTop - GRID_HEADER_HEIGHT),
        viewportSize: Math.max(0, viewportHeight - GRID_HEADER_HEIGHT),
      }),
    [rowSizes, scrollTop, viewportHeight],
  )

  const loadRef = useRef({
    rowCount: 0,
    hasMore: false,
    fetchingNext: false,
    sizes: [] as number[],
    viewportH: 0,
  })
  loadRef.current = {
    rowCount: records.rows.length,
    hasMore: records.hasMore,
    fetchingNext: records.isFetchingNextPage,
    sizes: rowSizes,
    viewportH: viewportHeight,
  }

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget
    setScrollTop(element.scrollTop)
    const snapshot = loadRef.current
    const window = computeWindow({
      sizes: snapshot.sizes,
      scrollOffset: Math.max(0, element.scrollTop - GRID_HEADER_HEIGHT),
      viewportSize: Math.max(0, snapshot.viewportH - GRID_HEADER_HEIGHT),
    })
    if (
      window.contentEnd >= 0 &&
      window.contentEnd >= snapshot.rowCount - FETCH_AHEAD_ROWS &&
      snapshot.hasMore &&
      !snapshot.fetchingNext
    ) {
      void records.fetchNextPage()
    }
  }

  useEffect(() => {
    if (recordsReady) {
      onRecordCountChange?.(records.rows.length, records.hasMore)
    } else {
      onRecordCountChange?.(0, false)
    }
  }, [onRecordCountChange, records.hasMore, records.rows.length, recordsReady])

  function closeDialog() {
    setDialog(null)
  }

  async function invalidateFields() {
    if (tableId === null) return
    await queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId) })
  }

  async function invalidateStructure() {
    if (tableId === null) return
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId) }),
      queryClient.invalidateQueries({ queryKey: viewKeys.list(tableId) }),
      queryClient.invalidateQueries({ queryKey: recordsKeys.all }),
      queryClient.invalidateQueries({ queryKey: viewKeys.configScope() }),
    ])
  }

  async function handleCreate(input: CreateFieldInput, description: string) {
    if (tableId === null) return
    const created = await trackWrite(() => createField(tableId, input))
    if (description !== '') {
      workspaceStore.getState().setFieldDescription(created.id, description)
    }
    await invalidateFields()
  }

  async function handleRename(field: FieldRecord, name: string) {
    await trackWrite(() => renameField(field.id, name))
    await invalidateFields()
  }

  async function handleSaveConfig(field: FieldRecord, configMap: Record<string, unknown>) {
    await trackWrite(() => updateFieldConfig(field.id, configMap))
    await invalidateFields()
  }

  async function handleSaveOptions(
    field: FieldRecord,
    options: FieldOptionValue[],
    allowCreate: boolean,
  ) {
    await trackWrite(() => updateFieldOptions(field.id, options, allowCreate))
    await invalidateFields()
  }

  async function handleDuplicate(field: FieldRecord) {
    try {
      setActionError(null)
      await trackWrite(() => duplicateField(field.id))
      await invalidateStructure()
    } catch (failure) {
      setActionError(apiErrorText(failure))
    }
  }

  /** 删除后清理视图中对字段的引用；返回实际移除失效条件的视图数。 */
  async function cleanupViewReferences(fieldId: string): Promise<number> {
    if (views.length === 0) return 0
    const loaded = await Promise.allSettled(
      views.map(async (view) => ({
        viewId: view.id,
        config: await fetchViewConfigRaw(view.id),
      })),
    )
    const entries = loaded
      .filter(
        (result): result is PromiseFulfilledResult<{ viewId: string; config: Record<string, unknown> }> =>
          result.status === 'fulfilled',
      )
      .map((result) => result.value)
    if (entries.length === 0) return 0
    const { cleaned, affectedViewIds } = removeFieldFromViews(entries, fieldId)
    await Promise.allSettled(
      cleaned
        .filter((entry) => entry.changed)
        .map((entry) => updateViewConfigRaw(entry.viewId, entry.config)),
    )
    return affectedViewIds.length
  }

  async function handleDelete(field: FieldRecord): Promise<number> {
    await trackWrite(() => deleteField(field.id))
    const affected = await cleanupViewReferences(field.id)
    await invalidateStructure()
    if (affected > 0) {
      toast?.push(`已移除 ${affected} 个视图中的失效条件`)
    }
    return affected
  }

  async function handleConvert(field: FieldRecord, target: FieldType, confirm: boolean) {
    const result = await trackWrite(() => convertField(field.id, target, confirm))
    if (confirm) await invalidateStructure()
    return result
  }

  function openAction(action: FieldMenuAction, field: FieldRecord) {
    setActionError(null)
    switch (action) {
      case 'duplicate':
        void handleDuplicate(field)
        return
      case 'rename':
        setDialog({ kind: 'rename', field })
        return
      case 'config':
        setDialog({ kind: 'config', field })
        return
      case 'convert':
        setDialog({ kind: 'convert', field })
        return
      case 'options':
        setDialog({ kind: 'options', field })
        return
      case 'delete':
        setDialog({ kind: 'delete', field })
        return
      default:
        return
    }
  }

  function deleteImpact(field: FieldRecord): DeleteFieldImpactView {
    return buildDeleteImpact(fields, field, loadedRecordCount, hasMoreRecords)
  }

  const showGrid = recordsReady && !records.isError && records.rows.length > 0
  const totalWidth = columnWidths.reduce((sum, width) => sum + width, 0)

  function stateNode() {
    if (fields.length === 0) {
      return (
        <EmptyState
          title="请先新增字段"
          description={
            canManageStructure
              ? '点击表头末列「＋」创建第一个字段'
              : '请联系表格管理员添加字段'
          }
        />
      )
    }
    if (records.isError) {
      return (
        <div role="alert" className={gridStyles.recordError}>
          <p>{apiErrorText(records.error)}</p>
          <Button variant="secondary" onClick={() => void records.refetch()}>
            重新加载记录
          </Button>
        </div>
      )
    }
    if (recordsReady && records.isFetching && records.rows.length === 0) {
      return <p role="status">正在加载记录…</p>
    }
    if (baseId !== undefined && tableId !== null && viewId === null) {
      return <p role="status">正在加载记录…</p>
    }
    if (recordsReady && records.rows.length === 0) {
      if (hasActiveFilters) {
        return (
          <div className={gridStyles.gridMessage}>
            <p>没有符合筛选条件的记录</p>
            {onClearFilters === undefined ? null : (
              <Button variant="secondary" onClick={onClearFilters}>
                清除筛选
              </Button>
            )}
          </div>
        )
      }
      return (
        <EmptyState
          title="从第一行开始"
          description="还没有记录，点击下方「+ 新增记录」开始录入"
        />
      )
    }
    return null
  }

  return (
    <section aria-label="网格" className={styles.gridSection}>
      {fieldsLoading ? (
        <div className={styles.statusRow}>
          <p role="status">正在加载字段…</p>
        </div>
      ) : fieldsError !== undefined && fieldsError !== null ? (
        <div className={styles.statusRow}>
          <p role="alert">{apiErrorTitle(fieldsError)}</p>
          {onRetryFields === undefined ? null : (
            <Button variant="secondary" onClick={onRetryFields}>
              重新加载字段
            </Button>
          )}
        </div>
      ) : allFieldsHidden ? (
        <div className={gridStyles.gridState}>
          <EmptyState
            title="所有字段均已隐藏"
            description="隐藏字段不影响数据，可随时恢复展示"
            action={
              onRestoreFields === undefined
                ? undefined
                : { label: '恢复字段显示', onClick: onRestoreFields }
            }
          />
        </div>
      ) : (
        <>
          <div
            ref={viewportRef}
            className={gridStyles.gridViewport}
            role="grid"
            aria-label="记录网格"
            tabIndex={0}
            onScroll={handleScroll}
          >
            <FieldHeaderRow
              fields={fields}
              descriptions={descriptions}
              canManageStructure={canManageStructure}
              canEditFields={canEditFields}
              atCapacity={atCapacity}
              onCreate={() => setDialog({ kind: 'create' })}
              onAction={openAction}
              columnWidths={columnWidths}
              frozenCount={sortedFields.length > 0 ? 1 : 0}
              sticky
            />
            {showGrid ? (
              <div
                className={gridStyles.gridContent}
                style={{ width: totalWidth, height: rowWindow.totalSize }}
              >
                {rowWindow.sections.map((section) => {
                  const record = records.rows[section.index]
                  if (record === undefined) return null
                  return (
                    <div
                      key={record.id}
                      role="row"
                      className={gridStyles.gridRow}
                      data-row-index={section.index}
                      style={{ top: section.offset, height: section.size, width: totalWidth }}
                    >
                      {sortedFields.map((field, columnIndex) => (
                        <RecordCell
                          key={field.id}
                          recordId={record.id}
                          field={field}
                          value={record.cellsMap[field.id]}
                          config={cellConfigs[field.id]}
                          renderer={renderers[field.id]}
                          width={columnWidths[columnIndex] ?? DEFAULT_COLUMN_WIDTH}
                          frozen={columnIndex === 0}
                          selected={
                            activeCell?.recordId === record.id && activeCell.fieldId === field.id
                          }
                          onSelect={handleSelectCell}
                          onOpenRecord={openCallbacks[field.id]}
                        />
                      ))}
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className={gridStyles.gridState}>{stateNode()}</div>
            )}
          </div>
          {canManageStructure && atCapacity ? (
            <p className={styles.capacityHint}>字段数已达上限（100），无法新增字段</p>
          ) : null}
          {actionError === null ? null : (
            <p role="alert" className={styles.inlineError}>
              {actionError}
            </p>
          )}
        </>
      )}

      {dialog?.kind === 'create' ? (
        <FieldCreateDialog
          fields={fields}
          tables={tables}
          onCreate={handleCreate}
          onClose={closeDialog}
        />
      ) : null}

      {dialog?.kind === 'rename' ? (
        <FieldRenameDialog
          field={dialog.field}
          fields={fields}
          onRename={(name) => handleRename(dialog.field, name)}
          onClose={closeDialog}
        />
      ) : null}

      {dialog?.kind === 'config' ? (
        <FieldConfigPanel
          field={dialog.field}
          tables={tables}
          currentTableFields={fields}
          onSaveConfig={(configMap) => handleSaveConfig(dialog.field, configMap)}
          onSaveOptions={(options, allowCreate) =>
            handleSaveOptions(dialog.field, options, allowCreate)
          }
          onClose={closeDialog}
        />
      ) : null}

      {dialog?.kind === 'convert' ? (
        <FieldConvertDialog
          field={dialog.field}
          onConvert={(target, confirm) => handleConvert(dialog.field, target, confirm)}
          onClose={closeDialog}
        />
      ) : null}

      {dialog?.kind === 'options' ? (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="field-options-dialog-title"
            className={styles.dialog}
          >
            <h2 id="field-options-dialog-title" className={styles.dialogTitle}>
              维护选项：{dialog.field.name}
            </h2>
            <OptionsEditor
              configMap={dialog.field.configMap}
              onSave={(options, allowCreate) =>
                handleSaveOptions(dialog.field, options, allowCreate)
              }
              onClose={closeDialog}
            />
          </div>
        </div>
      ) : null}

      {dialog?.kind === 'delete' ? (
        <FieldDeleteDialog
          field={dialog.field}
          impact={deleteImpact(dialog.field)}
          onConfirm={() => handleDelete(dialog.field)}
          onClose={closeDialog}
        />
      ) : null}
    </section>
  )
}
