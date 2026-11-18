import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, UIEvent } from 'react'

import { useQueryClient } from '@tanstack/react-query'

import {
  LoadedRangeCache,
  RowHeightModel,
  applyKey,
  clampCell,
  computeWindow,
  createCellRegistry,
  EDITOR_POPUP_ATTR,
  EditorPopup,
  extendRange,
  GRID_KEYS,
  ROW_HEIGHT_ESTIMATE_PX,
  initialKeyboardState,
  registerM2Cells,
  registerM2Editors,
  requestEdit,
  selectCell,
  selectionRect,
  withSelection,
  type CellEditorDef,
  type CellRef,
  type CellRendererDef,
  type EditorCommitMove,
  type GridBounds,
  type GridKey,
  type GridKeyEffect,
  type GridKeyEvent,
  type KeyboardState,
  type RowHeightTier,
  type SelectionRect,
} from '@/core/grid'
import {
  RECORD_LIMIT_MESSAGE,
  isRecordCapacityReached,
} from '@/features/records/capacity'
import {
  buildPastePlan,
  buildPasteSummary,
  defaultClipboard,
  parseTsv,
  serializeCellValue,
  serializeSelection,
  type ClipboardAdapter,
  type ClipboardField,
} from '@/features/records/clipboard'
import { editHistoryStore } from '@/features/records/edit-history'
import {
  usePersonDirectory,
  useRelationMetadata,
  type RelationMetadata,
} from '@/features/records/metadata'
import {
  appendRecord,
  applyBatchVersions,
  applyOptimisticCell,
  collectReferencedOptionIds,
  findRecord,
  removeRecords as removeRecordsData,
  revertBatchFailures,
  upsertRecordRow,
  type BatchOperationSnapshot,
  type BatchResultItem,
  type RecordsData,
} from '@/features/records/optimistic'
import { refreshRecordRows } from '@/features/records/refresh-after-write'
import { useRecords } from '@/features/records/useRecords'
import {
  batchUpdate,
  copyRecord,
  createRecord,
  deleteRecords,
  isCapacityError,
  isDeletedError,
  parseConflictMeta,
  undoDelete,
  updateCell,
} from '@/features/records/write-api'
import { fieldKeys, recordsKeys, viewKeys } from '@/query-keys'
import { track, TRACK_EVENTS } from '@/lib/tracker'
import { trackWrite, uiStore, workspaceStore } from '@/stores'
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
import { FIELD_LIMIT_MESSAGE } from '../capacity'
import { apiErrorText, apiErrorTitle } from '../errorText'
import type { FieldOptionValue, FieldType } from '../types/field-config'
import { DEFAULT_FIELD_WIDTH, type ViewConfig } from '../types/view-config'
import { viewDataSignature } from '../types/view-config-codec'
import { buildViewRows, type VisualRow } from '../view-grouping'
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
import { RecordConflictDialog } from './record/RecordConflictDialog'

/** 底栏/父组件触发的记录命令（token 递增即执行一次）。 */
export interface RecordCommand {
  readonly kind: 'create' | 'delete-selected' | 'copy-selected' | 'undo-delete'
  readonly token: number
}

export interface GridAreaProps {
  tableId: string | null
  /** 当前 Base；缺省时不请求记录（孤立表头测试/字段面板场景）。 */
  baseId?: string
  /** 当前视图；与 tableId 一起作为 H-48 查询标识。 */
  viewId?: string | null
  /** 当前视图配置（票 07）：显隐/冻结/宽度/行高/分组/条件签名。 */
  viewConfig?: ViewConfig | null
  /** 列宽拖拽松手：写入 field_width_map 并即时保存。 */
  onColumnWidthChange?: (fieldId: string, width: number) => void
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

  // ---- 票 06：记录写入 ----
  /** 权限矩阵：可新增记录（可查看/隐藏）。 */
  canCreateRecord?: boolean
  /** 权限矩阵：可编辑单元格（可填写=filter-self 由 editOwnOnly 表达）。 */
  canEditCell?: boolean
  /** 权限矩阵：可删除记录。 */
  canDeleteRecord?: boolean
  /** 可填写只能编辑自己创建的记录。 */
  editOwnOnly?: boolean
  /** 当前登录用户 id（editOwnOnly 判定）。 */
  currentUserId?: string
  /** 视图存在排序/分组：禁止拖行并提示「当前为排序视图」。 */
  hasSortOrGroup?: boolean
  /** 详情抽屉占位（票 08 接线）。 */
  onOpenDrawer?: (recordId: string) => void
  /** 勾选行变化（底栏批量条）。 */
  onSelectedRecordsChange?: (recordIds: readonly string[]) => void
  /** 底栏命令。 */
  recordCommand?: RecordCommand | null
  /** 测试注入的剪贴板。 */
  clipboard?: ClipboardAdapter
}

/** 分组行高（组头仅组名与折叠控件，N1；估算值，实测可校准）。 */
const GROUP_ROW_HEIGHT = 40
/** 行头（复选框 + 抽屉占位）宽度。 */
const ROW_HEADER_WIDTH = 64
/** 表头高度（虚拟窗口纵向偏移补偿）。 */
const GRID_HEADER_HEIGHT = 36
/** jsdom 无布局环境下的视口兜底（与 GridDemoPage 一致）。 */
const FALLBACK_VIEWPORT = { width: 800, height: 600 }
/** 距已加载末尾剩余行数 ≤ 该值时预取下一页。 */
const FETCH_AHEAD_ROWS = 5
/** 删除撤销窗口（H-54）。 */
const UNDO_WINDOW_MS = 7000

const READONLY_FIELD_TYPES = new Set(['formula', 'lookup', 'attachment'])

/** H-52 失败项：result_list 中 ok=false/无版本，以及 fail_row_list；按 index 去重。 */
function collectBatchFailures(result: {
  readonly resultList: readonly BatchResultItem[]
  readonly failedRows: readonly BatchResultItem[]
}): BatchResultItem[] {
  const byIndex = new Map<number, BatchResultItem>()
  for (const item of result.failedRows) byIndex.set(item.index, item)
  for (const item of result.resultList) {
    if (!item.ok || item.version <= 0) byIndex.set(item.index, item)
  }
  return [...byIndex.values()]
}

function cx(...parts: ReadonlyArray<string | false | undefined>): string {
  return parts.filter((part) => part !== undefined && part !== '').join(' ')
}

interface EditingState {
  readonly row: number
  readonly col: number
  readonly recordId: string
  readonly fieldId: string
  /** 直接打字进入编辑的首字符。 */
  readonly seed?: string
  /** 当前草稿的可提交值（失败重试 / 冲突复制用）。 */
  readonly lastValue?: unknown
  readonly error?: string | null
}

interface PendingEdit {
  readonly edit: EditingState
  readonly value: unknown
  readonly move: EditorCommitMove
  readonly before: unknown
}

type ConflictState =
  | {
      readonly kind: 'version'
      readonly pending: PendingEdit
      readonly theirValue: unknown
      readonly theirVersion: number
      readonly error: string | null
    }
  | { readonly kind: 'deleted'; readonly pending: PendingEdit; readonly error: string | null }

interface RecordCellProps {
  readonly recordId: string
  readonly field: FieldRecord
  readonly value: unknown
  readonly config: unknown
  readonly renderer: CellRendererDef
  readonly width: number
  readonly frozen: boolean
  /** 冻结列 sticky left（票 05 / BUG-03）：统一计入行头宽度，与表头冻结偏移同基。 */
  readonly frozenOffset?: number
  readonly selected: boolean
  readonly matched: boolean
  readonly editing: boolean
  readonly editor?: CellEditorDef
  readonly editorSeed?: string
  readonly writeError?: string | null
  readonly onSelect: (recordId: string, fieldId: string, event: React.MouseEvent) => void
  readonly onDoubleClick: (recordId: string, fieldId: string) => void
  readonly onOpenRecord?: (recordId: string) => void
  readonly onEditorCommit?: (value: unknown, move: EditorCommitMove) => void
  readonly onEditorReject?: () => void
  readonly onEditorDraftChange?: (value: unknown) => void
  /** 编辑器失焦（焦点真正离开宿主）：失焦提交缝（需求 4.6「回车/失焦」）。 */
  readonly onEditorBlur?: () => void
  readonly onRetryWrite?: () => void
}

/**
 * 单元格可访问名（票 04）：只呈现字段名（空值补「（空）」），
 * 不携带记录 ID（冒烟证据：空单元格可访问名出现「标题（01M6P7PD…）」式泄漏）。
 * 记录定位由 data-testid 与行头承担，屏幕阅读器按行上下文获取记录身份。
 */
function cellAccessibleName(fieldName: string, value: unknown): string {
  const isEmpty =
    value === null ||
    value === undefined ||
    value === '' ||
    (Array.isArray(value) && value.length === 0)
  return isEmpty ? `${fieldName}（空）` : fieldName
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
  frozenOffset,
  selected,
  matched,
  editing,
  editor,
  editorSeed,
  writeError,
  onSelect,
  onDoubleClick,
  onOpenRecord,
  onEditorCommit,
  onEditorReject,
  onEditorDraftChange,
  onEditorBlur,
  onRetryWrite,
}: RecordCellProps) {
  const Cell = renderer.component
  const Editor = editor?.component
  // 写失败提示弹层锚（票 01 收口）：宿主节点挂载后触发一次重渲染再挂载 portal 弹层。
  const [editorHost, setEditorHost] = useState<HTMLElement | null>(null)
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
        matched ? gridStyles.gridCellMatch : undefined,
      )}
      style={{
        width,
        flex: `0 0 ${width}px`,
        left: frozen ? frozenOffset : undefined,
      }}
      aria-selected={selected}
      aria-label={cellAccessibleName(field.name, value)}
      tabIndex={selected ? 0 : -1}
      data-testid={`cell-${recordId}-${field.id}`}
      data-cell-match={matched ? 'true' : undefined}
      onClick={(event) => onSelect(recordId, field.id, event)}
      onDoubleClick={() => onDoubleClick(recordId, field.id)}
    >
      {editing && Editor !== undefined ? (
        <span
          className={gridStyles.editorHost}
          data-testid={`editor-${recordId}-${field.id}`}
          ref={setEditorHost}
          onBlur={(event) => {
            // 失焦提交缝（票 02）：焦点真正离开编辑器宿主才提交；编辑器内部控件
            // （选项面板/日历/显示文本框/重试按钮）之间的焦点移动不打断编辑。
            // 票 01 portal 化配套：弹层（选项面板/日历/行内错误/写失败提示）现渲染在
            // document.body（EDITOR_POPUP_ATTR 标记、脱离宿主 DOM 子树），焦点移入本
            // 编辑器弹层仍属编辑器内部移动，不触发提交；提交链语义不变。
            const next = event.relatedTarget as Element | null
            if (next !== null && event.currentTarget.contains(next)) return
            if (next !== null && next.closest(`[${EDITOR_POPUP_ATTR}]`) !== null) return
            onEditorBlur?.()
          }}
        >
          <Editor
            value={value}
            config={config}
            initialInput={editorSeed}
            onCommit={(submitted, move) => onEditorCommit?.(submitted, move)}
            onReject={() => onEditorReject?.()}
            onDraftChange={(draft) => onEditorDraftChange?.(draft)}
          />
          {writeError === undefined || writeError === null || editorHost === null ? null : (
            // 票 01 收口：写失败提示（含重试按钮）与编辑器弹层同走 EditorPopup portal——
            // 不被单元格 overflow:hidden 裁剪；弹层自带 EDITOR_POPUP_ATTR 标记，
            // 失焦归属判定（含重试按钮点击的失焦序列）与网格/抽屉弹层同口径。
            <EditorPopup
              anchor={editorHost}
              role="alert"
              className={gridStyles.writeError}
              testId="cell-write-error"
            >
              {writeError}
              <button type="button" className={gridStyles.retryButton} onClick={onRetryWrite}>
                重试
              </button>
            </EditorPopup>
          )}
        </span>
      ) : (
        <Cell value={value} config={config} onOpenRecord={onOpenRecord} />
      )}
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

/** 网格区：字段表头 + 真实记录写入（票 06：新增/编辑/批量/删除/撤销/冲突）。 */
export function GridArea({
  tableId,
  baseId,
  viewId = null,
  viewConfig = null,
  onColumnWidthChange,
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
  canCreateRecord = true,
  canEditCell = true,
  canDeleteRecord = true,
  editOwnOnly = false,
  currentUserId = '',
  hasSortOrGroup = false,
  onOpenDrawer,
  onSelectedRecordsChange,
  recordCommand = null,
  clipboard,
}: GridAreaProps) {
  const queryClient = useQueryClient()
  const toast = useOptionalToast()
  const descriptions = workspaceStore((state) => state.fieldDescriptions)
  const fileMetaMap = workspaceStore((state) => state.fileMetaMap)
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

  const hiddenFieldIds = useMemo(
    () => new Set(viewConfig?.hiddenFieldIds ?? []),
    [viewConfig],
  )
  // hidden 覆盖所有字段 id：此时不再强行显示主字段，直接呈现「所有字段均已隐藏」。
  const allIdsHidden =
    viewConfig !== null && fields.length > 0 && fields.every((field) => hiddenFieldIds.has(field.id))
  // 主字段强制可见；其余按 hidden_field_list 过滤。
  const displayFields = useMemo(
    () =>
      allIdsHidden
        ? []
        : sortedFields.filter((field) => field.isPrimary || !hiddenFieldIds.has(field.id)),
    [allIdsHidden, hiddenFieldIds, sortedFields],
  )
  const effectiveAllFieldsHidden = allFieldsHidden || allIdsHidden
  const configSignature = useMemo(() => viewDataSignature(viewConfig), [viewConfig])

  const recordsReady =
    baseId !== undefined &&
    baseId !== '' &&
    tableId !== null &&
    viewId !== null &&
    !fieldsLoading &&
    !fieldsFailed &&
    !effectiveAllFieldsHidden &&
    displayFields.length > 0

  const records = useRecords({
    baseId: baseId ?? '',
    tableId,
    viewId,
    signature: configSignature,
    enabled: recordsReady,
  })

  const recordsKey = recordsKeys.listFor(tableId ?? '', viewId ?? '', configSignature)

  // 票 04：当前视图已加载记录中被引用的选项 ID（select/multiselect 单元格值），
  // 供「维护选项」删除选项前的前端引用检测；只覆盖已加载窗口（G-3 不虚构全量）。
  // 扫描纯逻辑下沉 features/records/optimistic.ts（collectReferencedOptionIds，与 findRecord 同文件）。
  const referencedOptionIds = useMemo(() => {
    const optionFieldIds = sortedFields
      .filter((field) => field.type === 'select' || field.type === 'multiselect')
      .map((field) => field.id)
    return collectReferencedOptionIds(records.rows, optionFieldIds)
  }, [records.rows, sortedFields])

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
    registerM2Editors(instance)
    return instance
  }, [])

  const renderers = useMemo(() => {
    const map: Record<string, CellRendererDef> = {}
    for (const field of sortedFields) {
      map[field.id] = registry.queryRenderer(field.type)
    }
    return map
  }, [registry, sortedFields])

  const editors = useMemo(() => {
    const map: Record<string, CellEditorDef> = {}
    for (const field of sortedFields) {
      map[field.id] = registry.queryEditor(field.type)
    }
    return map
  }, [registry, sortedFields])

  const columnWidths = useMemo(
    () =>
      displayFields.map((field) => {
        const width = viewConfig?.fieldWidthMap[field.id]
        return typeof width === 'number' && Number.isFinite(width) ? width : DEFAULT_FIELD_WIDTH
      }),
    [displayFields, viewConfig],
  )

  // 冻结列数：取 field_width 之外由 frozen_field_list 指定的最左连续前缀；
  // 无配置时保持票 05 行为（默认冻结主字段首列）。
  const frozenCount = useMemo(() => {
    if (viewConfig === null) return displayFields.length > 0 ? 1 : 0
    const frozenIds = new Set(viewConfig.frozenFieldIds)
    let count = 0
    for (const field of displayFields) {
      if (!frozenIds.has(field.id)) break
      count += 1
    }
    return count
  }, [displayFields, viewConfig])

  // 数据行冻结列 sticky left（票 05 / BUG-03）：统一计入行头宽度，与表头冻结偏移同基。
  const frozenCellOffsets = useMemo(() => {
    const offsets: number[] = []
    let offset = ROW_HEADER_WIDTH
    for (let index = 0; index < displayFields.length; index += 1) {
      offsets[index] = offset
      if (index < frozenCount) offset += columnWidths[index] ?? DEFAULT_FIELD_WIDTH
    }
    return offsets
  }, [columnWidths, displayFields.length, frozenCount])

  const createOptionFor = useCallback(
    (field: FieldRecord) => async (name: string): Promise<string | null> => {
      const rawList = Array.isArray(field.configMap.option_list)
        ? (field.configMap.option_list as Array<Record<string, unknown>>)
        : []
      const entries = rawList.map((option) => ({
        ...(typeof option.id === 'string' && option.id !== '' ? { id: option.id } : {}),
        name: String(option.name ?? ''),
        color: String(option.color ?? 'blue'),
      }))
      const updated = await trackWrite(() =>
        updateFieldOptions(
          field.id,
          [...entries, { name: name.trim(), color: 'blue' }],
          true,
        ),
      )
      await queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId ?? '') })
      const target = (updated.configMap.option_list as Array<Record<string, unknown>> | undefined)
        ?.slice()
        .reverse()
        .find((option) => String(option.name ?? '') === name.trim())
      if (target !== undefined && typeof target.id === 'string') return target.id
      const byName = updated.configMap.option_list as Array<Record<string, unknown>> | undefined
      const fallback = byName?.find((option) => String(option.name ?? '') === name.trim())
      return typeof fallback?.id === 'string' ? fallback.id : null
    },
    [queryClient, tableId],
  )

  // 票 08：会话内上传/重命名结果并入附件渲染配置，网格无需重拉即可显示文件名。
  const attachmentFiles = useMemo(() => {
    const files: Record<string, { name: string; display_name: string }> = {}
    for (const [fileId, meta] of Object.entries(fileMetaMap)) {
      files[fileId] = { name: meta.displayName || meta.name, display_name: meta.displayName }
    }
    return files
  }, [fileMetaMap])

  const cellConfigs = useMemo(() => {
    const configs: Record<string, unknown> = {}

    const attachmentConfigFor = (field: FieldRecord): Record<string, unknown> => {
      const configured = field.configMap.files
      const base =
        configured !== null && typeof configured === 'object' && !Array.isArray(configured)
          ? (configured as Record<string, unknown>)
          : {}
      return { ...field.configMap, files: { ...base, ...attachmentFiles } }
    }

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
        return attachmentConfigFor(targetField)
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
        case 'attachment':
          configs[field.id] = attachmentConfigFor(field)
          break
        case 'select':
        case 'multiselect':
          // H-40 要求 manage_structure：editor 注入 createOption 会被后端 3001 打成全局只读，
          // 因此仅 manager 注入；其他角色由编辑器显示「无匹配选项，请联系表格管理员添加」。
          configs[field.id] = canManageStructure
            ? { ...field.configMap, createOption: createOptionFor(field) }
            : field.configMap
          break
        case 'url':
          // 票 08 / BUG-02：网格窄列紧凑化——URL 编辑器只渲染链接地址单输入（显示文本缺省取
          // 地址本身）；地址+显示文本双字段完整编辑保留在记录详情抽屉（抽屉不注入 compact）。
          configs[field.id] = { ...field.configMap, compact: true }
          break
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
  }, [
    attachmentFiles,
    canManageStructure,
    createOptionFor,
    fieldsById,
    personDirectory,
    relationMetadata,
    sortedFields,
  ])

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

  // ---- 票 06：编辑/选择/键盘 ----
  const [keyboard, setKeyboard] = useState<KeyboardState>(initialKeyboardState)
  const [editing, setEditing] = useState<EditingState | null>(null)
  const [conflict, setConflict] = useState<ConflictState | null>(null)
  const [selectedRecordIds, setSelectedRecordIds] = useState<string[]>([])
  const [rowOrder, setRowOrder] = useState<string[] | null>(null)
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())
  const [dragHint, setDragHint] = useState<string | null>(null)
  const [pendingFocus, setPendingFocus] = useState<{ recordId: string; fieldId: string } | null>(
    null,
  )
  // 缺陷 #29：Ctrl+F 视图内搜索——纯客户端过滤已加载数据，不改服务端视图配置。
  const [findOpen, setFindOpen] = useState(false)
  const [findQuery, setFindQuery] = useState('')
  const findInputRef = useRef<HTMLInputElement | null>(null)
  const dragSourceRef = useRef<number | null>(null)
  const commandTokenRef = useRef(0)
  const viewportRef = useRef<HTMLDivElement | null>(null)
  /** 单元格提交单飞（票 02）：同一草稿的失焦/键盘重入只发一次请求。 */
  const commitInFlightRef = useRef(false)
  /**
   * 编辑器最新草稿（票 02）：ref 同步承载（onDraftChange 直写、显式提交回写、新编辑会话清空），
   * 失焦/点击离开提交一律读它。React 状态批处理下 blur 可能在同批次内先于 setEditing 落地触发
   * （如弹层点击冒泡抢焦），闭包 state（edit.lastValue）会读到旧草稿 → 判定「未改动」→ 选择丢失。
   */
  const editorDraftRef = useRef<unknown>(undefined)
  const clipboardAdapter = clipboard ?? defaultClipboard()

  // 票 09：权限中途降级为只读时立即退出编辑/冲突态与写对话框。
  useEffect(() => {
    if (canEditCell) return
    setEditing(null)
    setConflict(null)
    setKeyboard((current) =>
      current.mode === 'editing' ? { ...current, mode: 'idle' } : current,
    )
  }, [canEditCell])

  useEffect(() => {
    if (canManageStructure || canEditFields) return
    setDialog(null)
  }, [canManageStructure, canEditFields])

  const orderedRows = useMemo(() => {
    if (rowOrder === null) return records.rows
    const position = new Map(rowOrder.map((id, index) => [id, index]))
    return [...records.rows].sort((left, right) => {
      const a = position.get(left.id) ?? Number.MAX_SAFE_INTEGER
      const b = position.get(right.id) ?? Number.MAX_SAFE_INTEGER
      return a - b
    })
  }, [records.rows, rowOrder])

  const groupConfig = viewConfig?.group ?? null

  // 复制/搜索共用的取值上下文（人员目录、关联标签）；搜索取值口径与复制一致。
  const clipboardExtras = useMemo(
    () => ({
      personDirectory,
      relationLabels: Object.fromEntries(
        displayFields
          .filter((field) => field.type === 'relation')
          .flatMap((field) => {
            const targetTableId = String(field.configMap.target_table_id ?? '')
            return Object.entries(relationMetadata[targetTableId]?.labels ?? {})
          }),
      ),
    }),
    [personDirectory, relationMetadata, displayFields],
  )

  // ---- 缺陷 #29：Ctrl+F 视图内搜索（纯客户端，只作用于已加载数据） ----
  const findTerm = findQuery.trim().toLowerCase()
  const findActive = findOpen && findTerm !== ''
  /** 记录 id → 命中字段集合；文本口径与复制一致（serializeCellValue）。 */
  const matchCellsByRecord = useMemo(() => {
    const map = new Map<string, Set<string>>()
    if (!findActive) return map
    for (const record of orderedRows) {
      const hits = new Set<string>()
      for (const field of displayFields) {
        const text = serializeCellValue(field, record.cellsMap[field.id], clipboardExtras)
        if (text.toLowerCase().includes(findTerm)) hits.add(field.id)
      }
      if (hits.size > 0) map.set(record.id, hits)
    }
    return map
  }, [clipboardExtras, displayFields, findActive, findTerm, orderedRows])
  const searchedRows = useMemo(
    () =>
      findActive
        ? orderedRows.filter((record) => (matchCellsByRecord.get(record.id)?.size ?? 0) > 0)
        : orderedRows,
    [findActive, matchCellsByRecord, orderedRows],
  )
  const findSummary = findActive
    ? `匹配 ${searchedRows.length}/${orderedRows.length} 条`
    : `已加载 ${orderedRows.length} 条`

  function closeFind() {
    setFindOpen(false)
    setFindQuery('')
    focusViewport()
  }

  // 分组平铺呈现（N1）：组头 + 组内数据行；折叠组隐藏其数据行。
  // 搜索激活时输入为过滤后的行，组头随命中行收敛。
  const visualRows: VisualRow[] = useMemo(
    () =>
      buildViewRows({
        rows: searchedRows,
        group: groupConfig,
        fields,
        personDirectory,
        collapsedKeys: collapsedGroups,
      }),
    [collapsedGroups, fields, groupConfig, searchedRows, personDirectory],
  )
  const visualIndexByRecord = useMemo(() => {
    const map = new Map<number, number>()
    visualRows.forEach((row, index) => {
      if (row.kind === 'record') map.set(row.recordIndex, index)
    })
    return map
  }, [visualRows])

  // 当前视图可见记录（票 05 / BUG-03）：随搜索过滤与分组折叠收敛，供表头全选复选框使用。
  const visibleRecordIds = useMemo(() => {
    const ids: string[] = []
    for (const row of visualRows) {
      if (row.kind === 'record') ids.push(row.record.id)
    }
    return ids
  }, [visualRows])

  const toggleGroup = useCallback((key: string) => {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])

  useEffect(() => {
    setCollapsedGroups(new Set())
  }, [tableId, viewId, groupConfig?.fieldId, groupConfig?.granularity])

  const bounds: GridBounds = { rowCount: searchedRows.length, colCount: displayFields.length }
  const activeRect: SelectionRect | null =
    keyboard.focus === null
      ? null
      : selectionRect({
          kind: 'range',
          anchor: keyboard.anchor ?? keyboard.focus,
          focus: keyboard.focus,
        })

  const capacityReached = isRecordCapacityReached(orderedRows.length, records.hasMore)

  function setRecordsData(updater: (data: RecordsData | undefined) => RecordsData | undefined) {
    if (tableId === null || viewId === null) return
    queryClient.setQueryData<RecordsData>(recordsKey, (data) => updater(data))
  }

  function getRecordsData(): RecordsData | undefined {
    return queryClient.getQueryData<RecordsData>(recordsKey)
  }

  /**
   * 写后整行刷新（票 06 / BUG-05 前端部分）：写链成功后拉取 H-49 整行并合并进本地缓存，
   * 公式/引用列随之即时更新，无需切页。拉取失败静默降级：写成功 UI 不回滚，下次拉取自愈。
   */
  function refreshRows(recordIds: readonly string[]) {
    if (baseId === undefined || baseId === '') return
    const unique = [...new Set(recordIds)].filter((id) => id !== '')
    if (unique.length === 0) return
    void refreshRecordRows(baseId, unique, (row) =>
      setRecordsData((data) => upsertRecordRow(data, row)),
    )
  }

  const fieldIsReadonly = (field: FieldRecord): boolean =>
    READONLY_FIELD_TYPES.has(field.type) || !canEditCell

  const recordEditable = (record: { createdBy: string }): boolean =>
    canEditCell && (!editOwnOnly || record.createdBy === currentUserId)

  const cellEditable = (record: { createdBy: string } | undefined, field: FieldRecord | undefined): boolean =>
    record !== undefined && field !== undefined && !fieldIsReadonly(field) && recordEditable(record)

  const clipboardFields: ClipboardField[] = useMemo(
    () =>
      displayFields.map((field) => ({
        id: field.id,
        name: field.name,
        type: field.type,
        configMap: field.configMap,
      })),
    [displayFields],
  )

  const focusViewport = useCallback(() => {
    viewportRef.current?.focus()
  }, [])

  function focusEditor() {
    const input = viewportRef.current
      ?.querySelector('[data-testid^="editor-"]')
      ?.querySelector('input, textarea') as HTMLElement | null | undefined
    input?.focus()
  }

  const closeEditing = useCallback((move: EditorCommitMove) => {
    setEditing(null)
    if (move === 'none') {
      focusViewport()
      return
    }
    setKeyboard((current) => {
      const transition = applyKey(
        { ...current, mode: 'editing' },
        move === 'down'
          ? { key: 'Enter' }
          : { key: 'Tab', shift: move === 'left' },
        bounds,
      )
      return transition.state
    })
    focusViewport()
  }, [bounds.colCount, bounds.rowCount, focusViewport])

  function beginEditing(cell: CellRef, seed?: string): boolean {
    const record = searchedRows[cell.row]
    const field = displayFields[cell.col]
    if (record === undefined || field === undefined) return false
    if (!cellEditable(record, field)) return false
    setKeyboard({ mode: 'editing', focus: cell, anchor: cell })
    // 新编辑会话：清空上一会话草稿（票 02 草稿新鲜度 ref）。
    editorDraftRef.current = undefined
    setEditing({
      row: cell.row,
      col: cell.col,
      recordId: record.id,
      fieldId: field.id,
      seed,
      lastValue: record.cellsMap[field.id],
      error: null,
    })
    return true
  }

  async function submitCell(edit: EditingState, value: unknown, move: EditorCommitMove) {
    if (baseId === undefined || baseId === '' || tableId === null) return
    const record = searchedRows[edit.row]
    const field = displayFields[edit.col]
    if (record === undefined || field === undefined) return
    // 单飞（票 02）：失焦与 click/Enter 可能先后触发同一草稿提交，重入只发一次请求。
    if (commitInFlightRef.current) return
    commitInFlightRef.current = true
    const before = record.cellsMap[edit.fieldId]
    const expectVersion = record.version
    // 显式提交值回写草稿 ref（票 02）：失败重试/失焦兜底读取的草稿与本次提交一致。
    editorDraftRef.current = value

    setRecordsData((data) =>
      applyOptimisticCell(data, {
        recordId: edit.recordId,
        fieldId: edit.fieldId,
        value,
      }),
    )
    setEditing({ ...edit, lastValue: value, error: null })

    try {
      const result = await trackWrite(() =>
        updateCell(baseId, edit.recordId, edit.fieldId, value, expectVersion),
      )
      // 埋点（H-94 / 工单 17）：提交保存成功（附录 C fe_cell_edit_submit，字段 field_type）。
      track(TRACK_EVENTS.cellEditSubmit, { field_type: field.type })
      setRecordsData((data) =>
        applyOptimisticCell(data, {
          recordId: edit.recordId,
          fieldId: edit.fieldId,
          value: result.value,
          version: result.version,
        }),
      )
      editHistoryStore.getState().push({
        baseId,
        recordId: edit.recordId,
        fieldId: edit.fieldId,
        before,
        after: result.value,
        versionAtEdit: expectVersion,
      })
      refreshRows([edit.recordId])
      closeEditing(move)
    } catch (failure) {
      // 失败回滚到提交前值（版本不变），编辑器与输入保留
      setRecordsData((data) =>
        applyOptimisticCell(data, {
          recordId: edit.recordId,
          fieldId: edit.fieldId,
          value: before,
        }),
      )
      // 票 09：写请求失败时若本次响应已触发全局只读（3001/3002）或已离线，立即退出编辑态。
      if (workspaceStore.getState().forcedReadOnly || workspaceStore.getState().offline) {
        setEditing(null)
        setKeyboard((current) =>
          current.mode === 'editing' ? { ...current, mode: 'idle' } : current,
        )
        return
      }
      const conflictMeta = parseConflictMeta(failure)
      if (conflictMeta !== null) {
        setConflict({
          kind: 'version',
          pending: { edit, value, move, before },
          theirValue: conflictMeta.theirValue,
          theirVersion: conflictMeta.theirVersion,
          error: null,
        })
        return
      }
      if (isDeletedError(failure)) {
        setConflict({
          kind: 'deleted',
          pending: { edit, value, move, before },
          error: null,
        })
        return
      }
      const message = isCapacityError(failure)
        ? RECORD_LIMIT_MESSAGE
        : apiErrorText(failure)
      setEditing({ ...edit, lastValue: value, error: message })
    } finally {
      commitInFlightRef.current = false
    }
  }

  /**
   * 失焦/点击离开提交（票 02，需求 4.6「编辑提交（回车/失焦）即发 UpdateCell」）：
   * 把编辑器当前草稿发往服务端；草稿未改动时不发出请求、按提交语义直接收起编辑器；
   * 冲突弹窗裁决中（4001/4002）不重入提交，避免弹窗动作触发二次落盘。
   * 草稿一律取 editorDraftRef（同步承载的最新草稿，票 02），不读批处理下可能滞后的闭包 state。
   */
  function commitPendingDraft(move: EditorCommitMove) {
    if (editing === null || conflict !== null) return
    const edit = editing
    const before = searchedRows[edit.row]?.cellsMap[edit.fieldId]
    const draft = editorDraftRef.current
    if (draft === undefined || Object.is(draft, before)) {
      closeEditing(move)
      return
    }
    void submitCell(edit, draft, move)
  }

  function handleEditorCommit(edit: EditingState, value: unknown, move: EditorCommitMove) {
    void submitCell(edit, value, move)
  }

  function handleEditorReject() {
    // 票 09（E2E 回归）：取消不落盘——先清在途草稿 ref。Esc 经 onReject 卸载编辑器会触发
    // 宿主失焦提交缝，草稿 ref 若仍持有 Esc 前的最新草稿，提交缝会把它当作未提交改动发 PUT
    // （票 02 草稿新鲜度 ref 与 reject 路径的交叉缺陷，组件级测试未覆盖该交叉时序）。
    editorDraftRef.current = undefined
    setEditing(null)
    setKeyboard((current) => ({ ...current, mode: 'idle' }))
    focusViewport()
  }

  function handleEditorDraftChange(draft: unknown) {
    // 票 02：最新草稿同步写入 ref（失焦提交缝读 ref 规避批处理时序缺陷），state 仅作镜像。
    editorDraftRef.current = draft
    setEditing((current) => (current === null ? current : { ...current, lastValue: draft }))
  }

  /** 重试（票 02）：与失焦/键盘兜底同源走 commitPendingDraft，草稿未改动时零 PUT 直接收起。 */
  function retryWrite() {
    commitPendingDraft('down')
  }

  async function createRow() {
    if (!canCreateRecord) return
    if (baseId === undefined || baseId === '' || tableId === null) return
    // 无字段/视图时不能新增记录：给出可操作反馈，而不是静默忽略点击。
    if (!recordsReady) {
      const reason =
        fields.length === 0
          ? '请先新增字段后再新增记录'
          : viewId === null
            ? '请先创建视图后再新增记录'
            : fieldsLoading
              ? '正在加载表格，请稍候再试'
              : '当前表格暂不可新增记录'
      toast?.push(reason)
      return
    }
    if (capacityReached) {
      toast?.push(RECORD_LIMIT_MESSAGE)
      return
    }
    try {
      const created = await trackWrite(() => createRecord(baseId, tableId, {}))
      // 无排序视图的新记录通常在末页：先追加到当前无限查询缓存，保证新行立即可见；
      // 若走 invalidate 重拉，未加载末页时新记录不在已加载页，pendingFocus 永远不会生效。
      setRecordsData((data) => appendRecord(data, created))
      const primary = displayFields.find((field) => field.isPrimary) ?? displayFields[0]
      if (primary !== undefined) {
        setPendingFocus({ recordId: created.id, fieldId: primary.id })
      }
      // 新建响应不含重算后的公式/引用列：整行刷新让公式列立即呈现计算结果（票 06）。
      refreshRows([created.id])
    } catch (failure) {
      toast?.push(isCapacityError(failure) ? RECORD_LIMIT_MESSAGE : apiErrorText(failure))
    }
  }

  async function deleteSelectedRecords() {
    if (!canDeleteRecord) return
    if (baseId === undefined || baseId === '' || selectedRecordIds.length === 0) return
    const ids = [...selectedRecordIds]
    try {
      await trackWrite(() => deleteRecords(baseId, ids))
      setRecordsData((data) => removeRecordsData(data, ids))
      setSelectedRecordIds([])
      uiStore.getState().setUndoWindow({
        baseId,
        recordIds: ids,
        deadlineAt: Date.now() + UNDO_WINDOW_MS,
      })
      await queryClient.invalidateQueries({ queryKey: recordsKey })
      toast?.push(`已删除 ${ids.length} 条记录`)
    } catch (failure) {
      toast?.push(apiErrorText(failure))
    }
  }

  async function undoDeletedRecords() {
    if (!canDeleteRecord) return
    if (baseId === undefined || baseId === '') return
    const window = uiStore.getState().undoWindow
    if (window === null || window.baseId !== baseId) return
    if (Date.now() > window.deadlineAt) {
      uiStore.getState().clearUndoWindow()
      return
    }
    try {
      for (const recordId of window.recordIds) {
        await trackWrite(() => undoDelete(baseId, recordId))
      }
      uiStore.getState().clearUndoWindow()
      refreshRows(window.recordIds)
      await queryClient.invalidateQueries({ queryKey: recordsKey })
      toast?.push(`已恢复 ${window.recordIds.length} 条记录`)
    } catch (failure) {
      toast?.push(apiErrorText(failure))
    }
  }

  async function copySelectedRecords() {
    if (!canCreateRecord || !canDeleteRecord) return
    if (baseId === undefined || baseId === '' || tableId === null) return
    const ids = [...selectedRecordIds]
    if (ids.length === 0) return
    try {
      const createdIds: string[] = []
      for (const recordId of ids) {
        const created = await trackWrite(() => copyRecord(baseId, tableId, recordId, sortedFields))
        createdIds.push(created.id)
      }
      await queryClient.invalidateQueries({ queryKey: recordsKey })
      // 复制行同样整行刷新（票 06 补路径）：副本响应不含重算后的公式/引用列，且副本可能落在
      // 已加载页之外——H-49 补拉合并（upsertRecordRow 命中合并/缺失追加）使其即时可见并带公式。
      refreshRows(createdIds)
      setSelectedRecordIds([])
      toast?.push(`已复制 ${ids.length} 条记录`)
    } catch (failure) {
      toast?.push(isCapacityError(failure) ? RECORD_LIMIT_MESSAGE : apiErrorText(failure))
    }
  }

  async function clearCells(rect: SelectionRect) {
    if (baseId === undefined || baseId === '' || tableId === null) return
    const operations: BatchOperationSnapshot[] = []
    for (let row = rect.top; row <= rect.bottom; row += 1) {
      const record = searchedRows[row]
      if (record === undefined) continue
      for (let col = rect.left; col <= rect.right; col += 1) {
        const field = displayFields[col]
        if (field === undefined) continue
        if (!cellEditable(record, field)) continue
        operations.push({
          recordId: record.id,
          fieldId: field.id,
          before: record.cellsMap[field.id],
        })
      }
    }
    if (operations.length === 0) return

    setRecordsData((data) => {
      let next = data
      for (const operation of operations) {
        next = applyOptimisticCell(next, {
          recordId: operation.recordId,
          fieldId: operation.fieldId,
          value: null,
        })
      }
      return next
    })
    try {
      const result = await trackWrite(() =>
        batchUpdate(
          baseId,
          operations.map((operation) => ({
            recordId: operation.recordId,
            fieldId: operation.fieldId,
            value: null,
            expectVersion: 0,
          })),
        ),
      )
      const failures = collectBatchFailures(result)
      setRecordsData((data) =>
        revertBatchFailures(applyBatchVersions(data, result.resultList), operations, failures),
      )
      refreshRows(
        result.resultList.filter((item) => item.ok).map((item) => item.recordId),
      )
      if (failures.length > 0 || result.skippedCount > 0) {
        const parts: string[] = []
        if (failures.length > 0) parts.push(`失败 ${failures.length} 个`)
        if (result.skippedCount > 0) parts.push(`跳过 ${result.skippedCount} 个`)
        toast?.push(`清空未全部完成：${parts.join('，')}`)
      }
    } catch (failure) {
      setRecordsData((data) => {
        let next = data
        for (const operation of operations) {
          next = applyOptimisticCell(next, {
            recordId: operation.recordId,
            fieldId: operation.fieldId,
            value: operation.before,
          })
        }
        return next
      })
      toast?.push(apiErrorText(failure))
    }
  }

  async function copySelection(rect: SelectionRect, cut: boolean) {
    const text = serializeSelection({
      rows: searchedRows,
      fields: clipboardFields,
      rect,
      extras: clipboardExtras,
    })
    try {
      await clipboardAdapter.writeText(text)
    } catch {
      toast?.push('复制到剪贴板失败')
      return
    }
    if (cut) await clearCells(rect)
  }

  async function pasteAtFocus() {
    if (!canEditCell) return
    if (keyboard.focus === null || baseId === undefined || baseId === '') return
    const record = searchedRows[keyboard.focus.row]
    if (record === undefined) return
    let text = ''
    try {
      text = await clipboardAdapter.readText()
    } catch {
      toast?.push('读取剪贴板失败')
      return
    }
    const parsed = parseTsv(text)
    if (parsed.length === 0) return
    const plan = buildPastePlan({
      parsed,
      fields: clipboardFields,
      startCol: keyboard.focus.col,
      extras: clipboardExtras,
    })
    if (plan.unsupported) {
      toast?.push(buildPasteSummary(plan))
      return
    }
    if (plan.operations.length === 0) {
      toast?.push(buildPasteSummary(plan))
      return
    }

    const operations = plan.operations.map((operation) => ({
      recordId: record.id,
      fieldId: operation.fieldId,
      value: operation.value,
      expectVersion: operation.expectVersion,
      before: record.cellsMap[operation.fieldId],
    }))
    setRecordsData((data) => {
      let next = data
      for (const operation of operations) {
        next = applyOptimisticCell(next, {
          recordId: operation.recordId,
          fieldId: operation.fieldId,
          value: operation.value,
        })
      }
      return next
    })
    try {
      const result = await trackWrite(() => batchUpdate(baseId, operations))
      const failures = collectBatchFailures(result)
      setRecordsData((data) =>
        revertBatchFailures(applyBatchVersions(data, result.resultList), operations, failures),
      )
      refreshRows(
        result.resultList.filter((item) => item.ok).map((item) => item.recordId),
      )
      toast?.push(buildPasteSummary(plan, result.skippedCount, failures.length))
    } catch (failure) {
      setRecordsData((data) => {
        let next = data
        for (const operation of operations) {
          next = applyOptimisticCell(next, {
            recordId: operation.recordId,
            fieldId: operation.fieldId,
            value: record.cellsMap[operation.fieldId],
          })
        }
        return next
      })
      toast?.push(apiErrorText(failure))
    }
  }

  async function undoLastEdit() {
    if (!canEditCell) return
    if (baseId === undefined || baseId === '') return
    const entry = editHistoryStore.getState().peek(baseId)
    if (entry === null) return
    const data = getRecordsData()
    const record = findRecord(data, entry.recordId)
    if (record === undefined) {
      editHistoryStore.getState().drop(entry.id)
      return
    }
    const currentVersion = record.version
    try {
      const result = await trackWrite(() =>
        updateCell(baseId, entry.recordId, entry.fieldId, entry.before, currentVersion),
      )
      setRecordsData((current) =>
        applyOptimisticCell(current, {
          recordId: entry.recordId,
          fieldId: entry.fieldId,
          value: result.value,
          version: result.version,
        }),
      )
      refreshRows([entry.recordId])
      editHistoryStore.getState().drop(entry.id)
      toast?.push('已撤销最近一步编辑')
    } catch (failure) {
      toast?.push(apiErrorText(failure))
    }
  }

  function consumeEffect(effect: GridKeyEffect) {
    switch (effect.kind) {
      case 'beginEdit': {
        const ok = beginEditing(effect.cell)
        if (!ok) {
          setKeyboard({ mode: 'idle', focus: effect.cell, anchor: effect.cell })
        }
        return
      }
      case 'clearCells':
        void clearCells(effect.rect)
        return
      case 'clipboard':
        if (effect.action === 'copy') void copySelection(effect.rect, false)
        else if (effect.action === 'cut') void copySelection(effect.rect, true)
        else void pasteAtFocus()
        return
      case 'undo':
        void undoLastEdit()
        return
      case 'appendRow':
        void createRow()
        return
      case 'find':
        // 缺陷 #29：状态机信号与 Ctrl+F 直连分支殊途同归——打开视图内搜索条。
        setFindOpen(true)
        return
      default:
        return
    }
  }

  const handleGridKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (conflict !== null) return
    const target = event.target as HTMLElement
    const onEditorControl =
      target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT'
    if (editing !== null) {
      // 编辑态键盘兜底（票 02）：焦点不在编辑器控件内（真实键鼠焦点丢失）时，
      // 三键仍归提交/取消语义（AC-REC-02 编辑态分支此前不可达，Enter 被静默吞掉）。
      // 提交统一走 commitPendingDraft：草稿未改动时零 PUT，仍按三键移动语义收起编辑器。
      if (onEditorControl) return
      if (event.key === 'Enter') {
        event.preventDefault()
        commitPendingDraft('down')
        return
      }
      if (event.key === 'Tab') {
        event.preventDefault()
        commitPendingDraft(event.shiftKey ? 'left' : 'right')
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        handleEditorReject()
        return
      }
      return
    }
    if (onEditorControl) {
      return
    }
    const ctrl = event.ctrlKey || event.metaKey
    const focus = keyboard.focus

    // 缺陷 #29：Ctrl/Cmd+F 不依赖单元格焦点即可呼出视图内搜索条；
    // 过滤只作用于已加载数据，不写服务端视图配置。
    if (ctrl && !event.shiftKey && !event.altKey && event.key === 'f') {
      event.preventDefault()
      setFindOpen(true)
      return
    }

    if (focus !== null && event.shiftKey && !ctrl) {
      const deltas: Record<string, [number, number]> = {
        ArrowUp: [-1, 0],
        ArrowDown: [1, 0],
        ArrowLeft: [0, -1],
        ArrowRight: [0, 1],
      }
      const delta = deltas[event.key]
      if (delta !== undefined) {
        event.preventDefault()
        const next = clampCell(
          { row: focus.row + delta[0], col: focus.col + delta[1] },
          bounds,
        )
        const selection = extendRange(
          { kind: 'range', anchor: keyboard.anchor ?? focus, focus },
          next,
          bounds,
        )
        setKeyboard(withSelection({ ...keyboard, mode: 'idle' }, selection))
        return
      }
    }

    if (focus !== null && !ctrl && !event.altKey && event.key.length === 1) {
      const started = beginEditing(focus, event.key)
      if (started) {
        event.preventDefault()
        return
      }
    }

    if (!(GRID_KEYS as readonly string[]).includes(event.key)) return
    event.preventDefault()
    const keyEvent: GridKeyEvent = {
      key: event.key as GridKey,
      ctrl,
      shift: event.shiftKey,
    }
    const transition = applyKey(keyboard, keyEvent, bounds)
    setKeyboard(transition.state)
    transition.effects.forEach(consumeEffect)
  }

  function handleCellClick(recordId: string, fieldId: string, event: React.MouseEvent) {
    event.stopPropagation()
    // 票 02：编辑器弹层内部的点击不参与网格选择——弹层 portal 后 React 树仍与单元格连通，
    // 选项/日历点击会经 React 树冒泡抵达本 onClick；其后 focusViewport 抢焦触发失焦提交，
    // 与编辑器刚排队的草稿更新竞态（React 批处理下闭包读到旧草稿）→ 选择被吞。
    const clickTarget = event.target as Element | null
    if (clickTarget !== null && clickTarget.closest(`[${EDITOR_POPUP_ATTR}]`) !== null) return
    const row = searchedRows.findIndex((record) => record.id === recordId)
    const col = displayFields.findIndex((field) => field.id === fieldId)
    if (row < 0 || col < 0) return
    // 失焦提交（票 02，需求 4.6「回车/失焦」）：点击其他单元格 = 编辑器失焦，
    // 提交在途草稿而不是静默丢弃（冒烟零 PUT 断点）；点击正在编辑的格不打断编辑。
    if (editing !== null && !(editing.recordId === recordId && editing.fieldId === fieldId)) {
      commitPendingDraft('none')
    }
    const selection = selectCell({ row, col }, bounds)
    setKeyboard(withSelection({ mode: 'idle', focus: null, anchor: null }, selection))
    focusViewport()
  }

  function handleCellDoubleClick(recordId: string, fieldId: string) {
    const row = searchedRows.findIndex((record) => record.id === recordId)
    const col = displayFields.findIndex((field) => field.id === fieldId)
    if (row < 0 || col < 0) return
    const started = beginEditing({ row, col })
    if (started) {
      setKeyboard(requestEdit(keyboard, { row, col }, bounds).state)
      return
    }
    const selection = selectCell({ row, col }, bounds)
    setKeyboard(withSelection({ mode: 'idle', focus: null, anchor: null }, selection))
  }

  function toggleRecordSelection(recordId: string) {
    setSelectedRecordIds((current) =>
      current.includes(recordId)
        ? current.filter((id) => id !== recordId)
        : [...current, recordId],
    )
  }

  /** 表头全选复选框（票 05 / BUG-03）：勾选并入可见记录，复选清空选择。 */
  function toggleSelectAllRecords() {
    if (allVisibleSelected) {
      setSelectedRecordIds([])
      return
    }
    setSelectedRecordIds((current) => {
      const merged = new Set(current)
      for (const id of visibleRecordIds) merged.add(id)
      return [...merged]
    })
  }

  // 焦点行滚动跟随（窗口外行不渲染）
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
  // 行高三档：compact/medium/tall → RowHeightModel compact/medium/high；分组行单独估算。
  const rowTier: RowHeightTier =
    viewConfig?.rowHeight === 'compact'
      ? 'compact'
      : viewConfig?.rowHeight === 'tall'
        ? 'high'
        : 'medium'
  const rowModel = useMemo(
    () =>
      new RowHeightModel(rowTier, {
        estimateFor: (index) =>
          visualRows[index]?.kind === 'group'
            ? GROUP_ROW_HEIGHT
            : ROW_HEIGHT_ESTIMATE_PX[rowTier],
      }),
    [rowTier, visualRows],
  )
  const rowSizes = useMemo(
    () => rowModel.sizes(visualRows.length),
    [rowModel, visualRows.length],
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
    hasMore: false,
    fetchingNext: false,
    sizes: [] as number[],
    viewportH: 0,
  })
  loadRef.current = {
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
      window.contentEnd >= snapshot.sizes.length - FETCH_AHEAD_ROWS &&
      snapshot.hasMore &&
      !snapshot.fetchingNext
    ) {
      void records.fetchNextPage()
    }
  }

  useEffect(() => {
    // 记录窗口未就绪（结构加载中/字段全隐藏/首窗口数据未返回）不回报：避免把「未加载」
    // 误报成「0 行」，首屏探针（BaseWorkspacePage fe_perf_screen_ready）以首次回报为准。
    if (recordsReady && records.hasData) {
      onRecordCountChange?.(orderedRows.length, records.hasMore)
    }
  }, [onRecordCountChange, orderedRows.length, records.hasMore, records.hasData, recordsReady])

  useEffect(() => {
    onSelectedRecordsChange?.(selectedRecordIds)
  }, [onSelectedRecordsChange, selectedRecordIds])

  useEffect(() => {
    if (pendingFocus === null) return
    const row = searchedRows.findIndex((record) => record.id === pendingFocus.recordId)
    const col = displayFields.findIndex((field) => field.id === pendingFocus.fieldId)
    if (row < 0 || col < 0) return
    const record = searchedRows[row]
    if (record === undefined) return
    if (!cellEditable(record, displayFields[col])) {
      setPendingFocus(null)
      return
    }
    setKeyboard({ mode: 'editing', focus: { row, col }, anchor: { row, col } })
    // 新编辑会话：清空上一会话草稿（票 02 草稿新鲜度 ref）。
    editorDraftRef.current = undefined
    setEditing({
      row,
      col,
      recordId: record.id,
      fieldId: pendingFocus.fieldId,
      lastValue: record.cellsMap[pendingFocus.fieldId],
      error: null,
    })
    setPendingFocus(null)
  }, [searchedRows, pendingFocus, displayFields])

  useEffect(() => {
    if (editing === null) return
    focusEditor()
  }, [editing])

  useEffect(() => {
    if (keyboard.focus === null) return
    const element = viewportRef.current
    if (element === null) return
    const recordRow = keyboard.focus.row
    const row = visualIndexByRecord.get(recordRow)
    if (row === undefined || row < 0 || row >= rowSizes.length) return
    const size = rowSizes[row] ?? 0
    let top = 0
    for (let index = 0; index < row; index += 1) top += rowSizes[index] ?? 0
    const bottom = top + size
    if (top < element.scrollTop) {
      element.scrollTop = top
      setScrollTop(top)
    } else if (bottom > element.scrollTop + viewportHeight) {
      const next = bottom - viewportHeight
      element.scrollTop = next
      setScrollTop(next)
    }
  }, [keyboard.focus, rowSizes, viewportHeight, visualIndexByRecord])

  useEffect(() => {
    setRowOrder(null)
    setKeyboard(initialKeyboardState())
    setEditing(null)
    setConflict(null)
    setSelectedRecordIds([])
    setPendingFocus(null)
    setFindOpen(false)
    setFindQuery('')
  }, [tableId, viewId])

  // 搜索条打开时聚焦输入框（Ctrl+F 呼出后可直接输入）。
  useEffect(() => {
    if (!findOpen) return
    findInputRef.current?.focus()
  }, [findOpen])

  // 底栏命令：token 递增执行一次
  useEffect(() => {
    if (recordCommand === null || recordCommand.token === commandTokenRef.current) return
    commandTokenRef.current = recordCommand.token
    switch (recordCommand.kind) {
      case 'create':
        void createRow()
        return
      case 'delete-selected':
        void deleteSelectedRecords()
        return
      case 'copy-selected':
        void copySelectedRecords()
        return
      case 'undo-delete':
        void undoDeletedRecords()
        return
      default:
        return
    }
  })

  // LRU 区间缓存（票 05 保留）
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

  // ---- 冲突弹窗动作 ----
  const conflictField =
    conflict === null
      ? undefined
      : displayFields.find((field) => field.id === conflict.pending.edit.fieldId)
  const conflictMyDisplay =
    conflict === null || conflictField === undefined
      ? ''
      : serializeCellValue(conflictField, conflict.pending.value, clipboardExtras)
  const conflictTheirDisplay =
    conflict !== null && conflict.kind === 'version' && conflictField !== undefined
      ? serializeCellValue(conflictField, conflict.theirValue, clipboardExtras)
      : ''

  function adoptTheirValue() {
    if (conflict === null || conflict.kind !== 'version') return
    const { pending, theirValue, theirVersion } = conflict
    setRecordsData((data) =>
      applyOptimisticCell(data, {
        recordId: pending.edit.recordId,
        fieldId: pending.edit.fieldId,
        value: theirValue,
        version: theirVersion,
      }),
    )
    setConflict(null)
    setEditing(null)
    focusViewport()
  }

  async function overwriteWithMine() {
    if (conflict === null || conflict.kind !== 'version' || baseId === undefined) return
    const { pending, theirVersion } = conflict
    try {
      const result = await trackWrite(() =>
        updateCell(
          baseId,
          pending.edit.recordId,
          pending.edit.fieldId,
          pending.value,
          theirVersion,
        ),
      )
      setRecordsData((data) =>
        applyOptimisticCell(data, {
          recordId: pending.edit.recordId,
          fieldId: pending.edit.fieldId,
          value: result.value,
          version: result.version,
        }),
      )
      editHistoryStore.getState().push({
        baseId,
        recordId: pending.edit.recordId,
        fieldId: pending.edit.fieldId,
        before: pending.before,
        after: result.value,
        versionAtEdit: theirVersion,
      })
      refreshRows([pending.edit.recordId])
      setConflict(null)
      setEditing(null)
      closeEditing(pending.move)
    } catch (failure) {
      setConflict({ ...conflict, error: apiErrorText(failure) })
    }
  }

  async function copyConflictContent() {
    if (conflict === null) return
    try {
      await clipboardAdapter.writeText(conflictMyDisplay)
    } catch {
      setConflict({ ...conflict, error: '复制到剪贴板失败' })
      return
    }
    if (conflict.kind === 'version') {
      // 保留编辑态供继续修改
      setConflict(null)
      setTimeout(() => focusEditor(), 0)
      return
    }
    setConflict(null)
    setEditing(null)
    focusViewport()
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: recordsKey }),
      tableId === null
        ? Promise.resolve()
        : queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId) }),
    ])
  }

  function closeConflict() {
    if (conflict === null) return
    const kind = conflict.kind
    setConflict(null)
    if (kind === 'deleted') {
      setEditing(null)
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: recordsKey }),
        tableId === null
          ? Promise.resolve()
          : queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId) }),
      ])
      focusViewport()
      return
    }
    setTimeout(() => focusEditor(), 0)
  }

  // ---- 手动排序（本地行序；契约无记录排序字段，持久化不可用 → G 项） ----
  function handleDragStart(rowIndex: number) {
    if (!canEditCell || hasSortOrGroup) {
      if (hasSortOrGroup) setDragHint('当前为排序视图')
      return
    }
    dragSourceRef.current = rowIndex
    setDragHint(null)
  }

  function handleDrop(rowIndex: number) {
    if (!canEditCell || hasSortOrGroup) {
      if (hasSortOrGroup) setDragHint('当前为排序视图')
      return
    }
    const source = dragSourceRef.current
    dragSourceRef.current = null
    if (source === null || source === rowIndex) return
    const ids = searchedRows.map((record) => record.id)
    const moved = ids[source]
    if (moved === undefined) return
    ids.splice(source, 1)
    ids.splice(rowIndex, 0, moved)
    setRowOrder(ids)
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

  // ---- 字段结构管理（票 04 原样保留） ----
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

  function deleteImpact(field: FieldRecord): DeleteFieldImpactView {
    return buildDeleteImpact(fields, field, loadedRecordCount, hasMoreRecords)
  }

  const showGrid =
    recordsReady && !records.isError && searchedRows.length > 0
  const totalWidth = ROW_HEADER_WIDTH + columnWidths.reduce((sum, width) => sum + width, 0)
  const canSelectRecords = canDeleteRecord
  // 表头全选（票 05 / BUG-03）：勾选=可见记录全部进入既有选择状态；复选=清空。
  const allVisibleSelected =
    canSelectRecords &&
    visibleRecordIds.length > 0 &&
    visibleRecordIds.every((id) => selectedRecordIds.includes(id))

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
    if (recordsReady && records.isFetching && orderedRows.length === 0) {
      return <p role="status">正在加载记录…</p>
    }
    if (baseId !== undefined && tableId !== null && viewId === null) {
      return <p role="status">正在加载记录…</p>
    }
    if (recordsReady && findActive && searchedRows.length === 0) {
      return (
        <div className={gridStyles.gridMessage}>
          <p>没有匹配「{findQuery.trim()}」的已加载记录</p>
        </div>
      )
    }
    if (recordsReady && orderedRows.length === 0) {
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
      {editOwnOnly ? (
        <p role="status" className={styles.emptyHint}>
          仅显示你创建的记录
        </p>
      ) : null}
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
      ) : effectiveAllFieldsHidden ? (
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
          {findOpen ? (
            <div
              role="search"
              aria-label="视图内搜索"
              data-testid="grid-find-bar"
              className={gridStyles.findBar}
            >
              <input
                ref={findInputRef}
                data-testid="grid-find-input"
                type="text"
                className={gridStyles.findInput}
                placeholder="在已加载数据中搜索"
                aria-label="视图内搜索"
                maxLength={100}
                value={findQuery}
                onChange={(event) => setFindQuery(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== 'Escape') return
                  event.preventDefault()
                  event.stopPropagation()
                  closeFind()
                }}
              />
              <span className={gridStyles.findCount} data-testid="grid-find-count" role="status">
                {findSummary}
              </span>
              <button
                type="button"
                className={gridStyles.findClose}
                data-testid="grid-find-close"
                aria-label="关闭视图内搜索"
                onClick={closeFind}
              >
                ×
              </button>
            </div>
          ) : null}
          <div
            ref={viewportRef}
            className={gridStyles.gridViewport}
            role="grid"
            aria-label="记录网格"
            tabIndex={0}
            onScroll={handleScroll}
            onKeyDown={handleGridKeyDown}
          >
            <FieldHeaderRow
              fields={displayFields}
              descriptions={descriptions}
              canManageStructure={canManageStructure}
              canEditFields={canEditFields}
              atCapacity={atCapacity}
              onCreate={() => setDialog({ kind: 'create' })}
              onAction={openAction}
              columnWidths={columnWidths}
              rowHeaderWidth={ROW_HEADER_WIDTH}
              selectable={canSelectRecords}
              allSelected={allVisibleSelected}
              onToggleSelectAll={toggleSelectAllRecords}
              frozenCount={frozenCount}
              onColumnResize={onColumnWidthChange}
              sticky
            />
            {recordsReady && canCreateRecord ? (
              <button
                type="button"
                className={gridStyles.ghostRow}
                aria-label="首行新增记录"
                disabled={capacityReached}
                onClick={() => void createRow()}
              >
                + 点击新增记录
              </button>
            ) : null}
            {showGrid ? (
              <div
                className={gridStyles.gridContent}
                style={{ width: totalWidth, height: rowWindow.totalSize }}
              >
                {rowWindow.sections.map((section) => {
                  const item = visualRows[section.index]
                  if (item === undefined) return null
                  if (item.kind === 'group') {
                    return (
                      <div
                        key={`group-${item.key}`}
                        role="row"
                        className={gridStyles.groupRow}
                        data-group-key={item.key}
                        style={{ top: section.offset, height: section.size, width: totalWidth }}
                      >
                        <button
                          type="button"
                          className={gridStyles.groupToggle}
                          aria-expanded={!item.collapsed}
                          aria-label={`分组：${item.label}`}
                          onClick={() => toggleGroup(item.key)}
                        >
                          <span aria-hidden="true">{item.collapsed ? '▸' : '▾'}</span>
                          {item.label}
                        </button>
                      </div>
                    )
                  }
                  const record = item.record
                  const recordIndex = item.recordIndex
                  const isEditingRow = editing?.recordId === record.id
                  return (
                    <div
                      key={record.id}
                      role="row"
                      className={gridStyles.gridRow}
                      data-row-index={recordIndex}
                      data-record-id={record.id}
                      draggable={!hasSortOrGroup && canEditCell}
                      onDragStart={(event) => {
                        if (!hasSortOrGroup && canEditCell) {
                          event.dataTransfer?.setData('text/plain', String(recordIndex))
                        }
                        handleDragStart(recordIndex)
                      }}
                      onDragOver={(event) => {
                        if (!hasSortOrGroup) event.preventDefault()
                      }}
                      onDrop={(event) => {
                        event.preventDefault()
                        handleDrop(recordIndex)
                      }}
                      style={{ top: section.offset, height: section.size, width: totalWidth }}
                    >
                      <div
                        className={gridStyles.rowHeader}
                        style={{ width: ROW_HEADER_WIDTH, flex: `0 0 ${ROW_HEADER_WIDTH}px` }}
                      >
                        {canSelectRecords ? (
                          <input
                            type="checkbox"
                            aria-label={`选择记录 ${record.id}`}
                            checked={selectedRecordIds.includes(record.id)}
                            onChange={() => toggleRecordSelection(record.id)}
                            onClick={(event) => event.stopPropagation()}
                          />
                        ) : null}
                        <button
                          type="button"
                          className={gridStyles.expandButton}
                          aria-label={`展开记录 ${record.id}`}
                          disabled={onOpenDrawer === undefined}
                          onClick={() => onOpenDrawer?.(record.id)}
                        >
                          ›
                        </button>
                      </div>
                      {displayFields.map((field, columnIndex) => {
                        const cell = { row: recordIndex, col: columnIndex }
                        const isEditingCell =
                          isEditingRow === true &&
                          editing?.col === columnIndex &&
                          editing.fieldId === field.id
                        const selected =
                          activeRect !== null &&
                          cell.row >= activeRect.top &&
                          cell.row <= activeRect.bottom &&
                          cell.col >= activeRect.left &&
                          cell.col <= activeRect.right
                        const matched =
                          findActive &&
                          (matchCellsByRecord.get(record.id)?.has(field.id) ?? false)
                        return (
                          <RecordCell
                            key={field.id}
                            recordId={record.id}
                            field={field}
                            value={record.cellsMap[field.id]}
                            config={cellConfigs[field.id]}
                            renderer={renderers[field.id]}
                            width={columnWidths[columnIndex] ?? DEFAULT_FIELD_WIDTH}
                            frozen={columnIndex < frozenCount}
                            frozenOffset={
                              columnIndex < frozenCount ? frozenCellOffsets[columnIndex] : undefined
                            }
                            selected={selected}
                            matched={matched}
                            editing={isEditingCell && canEditCell}
                            editor={editors[field.id]}
                            editorSeed={isEditingCell && canEditCell ? editing?.seed : undefined}
                            writeError={isEditingCell && canEditCell ? editing?.error : null}
                            onSelect={handleCellClick}
                            onDoubleClick={handleCellDoubleClick}
                            onOpenRecord={openCallbacks[field.id]}
                            onEditorCommit={(value, move) => {
                              if (editing !== null) handleEditorCommit(editing, value, move)
                            }}
                            onEditorReject={handleEditorReject}
                            onEditorDraftChange={handleEditorDraftChange}
                            onEditorBlur={() => commitPendingDraft('none')}
                            onRetryWrite={retryWrite}
                          />
                        )
                      })}
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className={gridStyles.gridState}>{stateNode()}</div>
            )}
          </div>
          {dragHint === null ? null : (
            <p role="alert" className={styles.inlineError}>
              {dragHint}
            </p>
          )}
          {canManageStructure && atCapacity ? (
            <p className={styles.capacityHint}>{FIELD_LIMIT_MESSAGE}</p>
          ) : null}
          {actionError === null ? null : (
            <p role="alert" className={styles.inlineError}>
              {actionError}
            </p>
          )}
        </>
      )}

      {conflict === null ? null : (
        <RecordConflictDialog
          kind={conflict.kind}
          theirDisplay={conflictTheirDisplay}
          myDisplay={conflictMyDisplay}
          error={conflict.error}
          onAdopt={conflict.kind === 'version' ? adoptTheirValue : undefined}
          onOverwrite={conflict.kind === 'version' ? () => void overwriteWithMine() : undefined}
          onCopy={() => void copyConflictContent()}
          onClose={closeConflict}
        />
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
              usedOptionIds={referencedOptionIds}
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
