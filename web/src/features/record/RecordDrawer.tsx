// 记录详情抽屉（票 08）：右侧约 480px、无遮罩，网格保持可交互。
// - 顶部主字段与字段区全部字段（含视图隐藏字段）；8 类编辑器复用票 06 组件，公式/引用只读标注；
// - 提交走 H-51（带当前 version），失败保留输入；4001 复用三动作弹窗，4002 保留内容可复制；
// - 上一条/下一条按当前视图已加载顺序切换，末行 hasMore 时先补拉（H-48 共享 useRecords key）；
// - 附件字段走 H-77 上传、H-78 下载、H-79 缩略图、H-80 重命名、H-81 删除，并回写 H-51 的 file_id 列表。
import { useMemo, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

import { useQuery, useQueryClient } from '@tanstack/react-query'

import {
  createCellRegistry,
  registerM2Cells,
  registerM2Editors,
  type CellEditorDef,
  type CellRendererDef,
  type EditorCommitMove,
} from '@/core/grid'
import { fieldKeys, recordsKeys } from '@/query-keys'
import { trackWrite, workspaceStore } from '@/stores'

import { updateFieldOptions, type FieldRecord } from '@/features/workspace/api/field'
import { RecordConflictDialog } from '@/features/workspace/components/record/RecordConflictDialog'
import { sortFieldsForHeader } from '@/features/workspace/components/field/FieldHeaderRow'
import { apiErrorText, apiErrorTitle } from '@/features/workspace/errorText'
import { fetchRecord, type RecordRow } from '@/features/records/api'
import {
  defaultClipboard,
  serializeCellValue,
  type ClipboardAdapter,
} from '@/features/records/clipboard'
import {
  deleteFile,
  fileDownloadUrl,
  fileThumbUrl,
  isFileRejectedError,
  MAX_ATTACHMENT_BYTES,
  MAX_ATTACHMENTS_PER_RECORD,
  renameFile,
  uploadFile,
  type FileMeta,
} from '@/features/records/file-api'
import {
  usePersonDirectory,
  useRelationMetadata,
  type RelationMetadata,
} from '@/features/records/metadata'
import {
  applyOptimisticCell,
  type RecordsData,
} from '@/features/records/optimistic'
import { useRecords } from '@/features/records/useRecords'
import {
  isDeletedError,
  parseConflictMeta,
  updateCell,
} from '@/features/records/write-api'

import styles from './RecordDrawer.module.css'

const READONLY_FIELD_TYPES = new Set(['formula', 'lookup'])

export interface RecordDrawerProps {
  readonly baseId: string
  readonly tableId: string | null
  readonly viewId: string | null
  /** 与网格一致的筛选/排序/分组签名，确保共享同一 records query key。 */
  readonly signature?: string
  readonly recordId: string
  /** 当前表全部字段（含视图隐藏字段）。 */
  readonly fields: readonly FieldRecord[]
  /** 可编辑单元格（viewer 只读；filler 由 editOwnOnly 进一步限制）。 */
  readonly canEditCell: boolean
  readonly editOwnOnly?: boolean
  readonly currentUserId?: string
  readonly onClose: () => void
  /** 上一条/下一条切换：由工作台更新持有的 recordId。 */
  readonly onNavigateRecord?: (recordId: string) => void
  /** 测试注入的剪贴板。 */
  readonly clipboard?: ClipboardAdapter
}

interface DrawerEdit {
  readonly fieldId: string
  readonly lastValue: unknown
  readonly error: string | null
}

type ConflictState =
  | {
      readonly kind: 'version'
      readonly fieldId: string
      readonly myValue: unknown
      readonly theirValue: unknown
      readonly theirVersion: number
      readonly error: string | null
    }
  | {
      readonly kind: 'deleted'
      readonly fieldId: string
      readonly myValue: unknown
      readonly error: string | null
    }

function attachmentIds(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value
      .map((item) => (typeof item === 'string' ? item : String(item ?? '')))
      .filter((item) => item !== '')
  }
  if (typeof value === 'string' && value !== '') return [value]
  return []
}

interface AttachmentFieldProps {
  readonly baseId: string
  readonly field: FieldRecord
  readonly ids: readonly string[]
  readonly editable: boolean
  readonly onApply: (ids: string[]) => void
}

/** 附件字段编辑区：缩略图 / 下载 / 上传 / 重命名 / 删除 + 客户端预检。 */
function AttachmentField({ baseId, field, ids, editable, onApply }: AttachmentFieldProps) {
  const fileMetaMap = workspaceStore((state) => state.fileMetaMap)
  const setFileMeta = workspaceStore((state) => state.setFileMeta)
  const removeFileMeta = workspaceStore((state) => state.removeFileMeta)
  const [error, setError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameDraft, setRenameDraft] = useState('')

  const full = ids.length >= MAX_ATTACHMENTS_PER_RECORD

  function fileName(fileId: string): string {
    const meta = fileMetaMap[fileId]
    return meta?.displayName || meta?.name || fileId
  }

  async function handleFiles(files: FileList | null) {
    setError(null)
    if (files === null || files.length === 0) return
    const incoming = Array.from(files)
    if (ids.length + incoming.length > MAX_ATTACHMENTS_PER_RECORD) {
      setError('每条记录最多 10 个附件')
      return
    }
    const oversized = incoming.find((file) => file.size > MAX_ATTACHMENT_BYTES)
    if (oversized !== undefined) {
      setError('单个文件不能超过 20MB')
      return
    }
    setUploading(true)
    try {
      const uploaded: FileMeta[] = []
      for (const file of incoming) {
        const meta = await trackWrite(() => uploadFile(baseId, file))
        uploaded.push(meta)
        setFileMeta(meta.id, {
          name: meta.name,
          displayName: meta.display_name || meta.name,
        })
      }
      if (uploaded.length > 0) {
        onApply([...ids, ...uploaded.map((meta) => meta.id)])
      }
    } catch (failure) {
      setError(
        isFileRejectedError(failure)
          ? '文件不符合要求（类型受限或超出大小上限），请更换文件后重试'
          : apiErrorText(failure),
      )
    } finally {
      setUploading(false)
    }
  }

  async function saveRename(fileId: string) {
    const name = renameDraft.trim()
    if (name === '') {
      setError('附件名称不能为空')
      return
    }
    setError(null)
    try {
      const meta = await trackWrite(() => renameFile(fileId, name, baseId))
      setFileMeta(meta.id, {
        name: meta.name,
        displayName: meta.display_name || name,
      })
      setRenamingId(null)
      setRenameDraft('')
    } catch (failure) {
      setError(apiErrorText(failure))
    }
  }

  async function removeAttachment(fileId: string) {
    setError(null)
    try {
      await trackWrite(() => deleteFile(baseId, fileId))
      removeFileMeta(fileId)
      onApply(ids.filter((candidate) => candidate !== fileId))
    } catch (failure) {
      setError(apiErrorText(failure))
    }
  }

  return (
    <div className={styles.attachment} aria-label={`附件字段：${field.name}`}>
      <div className={styles.attachmentList}>
        {ids.map((fileId) => {
          const name = fileName(fileId)
          return (
            <div key={fileId} className={styles.attachmentItem} data-testid={`drawer-file-${fileId}`}>
              <img className={styles.attachmentThumb} src={fileThumbUrl(fileId)} alt={name} />
              <a
                className={styles.attachmentLink}
                href={fileDownloadUrl(fileId)}
                download
                aria-label={`下载附件 ${name}`}
              >
                {name}
              </a>
              {!editable ? null : renamingId === fileId ? (
                <span className={styles.renameForm}>
                  <input
                    className={styles.renameInput}
                    aria-label="附件新名称"
                    value={renameDraft}
                    onChange={(event) => setRenameDraft(event.target.value)}
                  />
                  <button
                    type="button"
                    className={styles.smallButton}
                    onClick={() => void saveRename(fileId)}
                  >
                    保存名称
                  </button>
                  <button
                    type="button"
                    className={styles.smallButton}
                    onClick={() => {
                      setRenamingId(null)
                      setRenameDraft('')
                    }}
                  >
                    取消重命名
                  </button>
                </span>
              ) : (
                <span className={styles.attachmentActions}>
                  <button
                    type="button"
                    className={styles.smallButton}
                    aria-label={`重命名附件 ${name}`}
                    onClick={() => {
                      setRenamingId(fileId)
                      setRenameDraft(name)
                      setError(null)
                    }}
                  >
                    重命名
                  </button>
                  <button
                    type="button"
                    className={styles.smallButton}
                    aria-label={`删除附件 ${name}`}
                    onClick={() => void removeAttachment(fileId)}
                  >
                    删除
                  </button>
                </span>
              )}
            </div>
          )
        })}
      </div>
      {ids.length === 0 ? <p className={styles.attachmentEmpty}>暂无附件</p> : null}
      {full ? <p className={styles.attachmentHint}>每条记录最多 10 个附件</p> : null}
      {error === null ? null : (
        <p role="alert" className={styles.inlineError}>
          {error}
        </p>
      )}
      {!editable ? null : (
        <label className={styles.uploadLabel}>
          上传附件
          <input
            className={styles.uploadInput}
            type="file"
            aria-label="上传附件"
            multiple
            disabled={full || uploading}
            onChange={(event) => {
              void handleFiles(event.target.files)
              event.target.value = ''
            }}
          />
        </label>
      )}
    </div>
  )
}

/** 记录详情抽屉：字段区优先展示当前视图隐藏字段，附件集中可操作。 */
export function RecordDrawer({
  baseId,
  tableId,
  viewId,
  signature = '',
  recordId,
  fields,
  canEditCell,
  editOwnOnly = false,
  currentUserId = '',
  onClose,
  onNavigateRecord,
  clipboard,
}: RecordDrawerProps) {
  const queryClient = useQueryClient()
  const clipboardAdapter = useMemo(() => clipboard ?? defaultClipboard(), [clipboard])
  const [edit, setEdit] = useState<DrawerEdit | null>(null)
  const [conflict, setConflict] = useState<ConflictState | null>(null)

  const sortedFields = useMemo(() => sortFieldsForHeader([...fields]), [fields])
  const fieldsById = useMemo(() => {
    const map = new Map<string, FieldRecord>()
    for (const field of sortedFields) map.set(field.id, field)
    return map
  }, [sortedFields])

  const listKey = recordsKeys.listFor(tableId ?? '', viewId ?? '', signature)
  const records = useRecords({
    baseId,
    tableId,
    viewId,
    signature,
    enabled: baseId !== '' && tableId !== null && viewId !== null,
  })

  const recordQuery = useQuery({
    queryKey: recordsKeys.record(recordId),
    queryFn: () => fetchRecord(recordId),
    enabled: baseId !== '' && recordId !== '',
    retry: false,
  })

  const loadedRow = records.rows.find((row) => row.id === recordId)
  const record = recordQuery.data ?? loadedRow ?? null

  // ---- 元数据（与网格同 key，组件间复用缓存） ----
  const personFields = sortedFields.filter((field) => field.type === 'person')
  const relationFields = sortedFields.filter((field) => field.type === 'relation')
  const lookupFields = sortedFields.filter((field) => field.type === 'lookup')
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
    baseId,
    baseId !== '' && personFields.length > 0,
  )
  const relationQuery = useRelationMetadata(
    baseId,
    targetTableIds,
    baseId !== '' && targetTableIds.length > 0,
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
    for (const field of sortedFields) map[field.id] = registry.queryRenderer(field.type)
    return map
  }, [registry, sortedFields])

  const editors = useMemo(() => {
    const map: Record<string, CellEditorDef> = {}
    for (const field of sortedFields) map[field.id] = registry.queryEditor(field.type)
    return map
  }, [registry, sortedFields])

  const createOptionFor = (field: FieldRecord) => async (name: string): Promise<string | null> => {
    const rawList = Array.isArray(field.configMap.option_list)
      ? (field.configMap.option_list as Array<Record<string, unknown>>)
      : []
    const entries = rawList.map((option) => ({
      ...(typeof option.id === 'string' && option.id !== '' ? { id: option.id } : {}),
      name: String(option.name ?? ''),
      color: String(option.color ?? 'blue'),
    }))
    const updated = await trackWrite(() =>
      updateFieldOptions(field.id, [...entries, { name: name.trim(), color: 'blue' }], true),
    )
    await queryClient.invalidateQueries({ queryKey: fieldKeys.list(tableId ?? '') })
    const list = updated.configMap.option_list as Array<Record<string, unknown>> | undefined
    const target = list?.slice().reverse().find((option) => String(option.name ?? '') === name.trim())
    return typeof target?.id === 'string' ? target.id : null
  }

  const configs = useMemo(() => {
    const map: Record<string, unknown> = {}

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
      return targetField.configMap
    }

    for (const field of sortedFields) {
      switch (field.type) {
        case 'person':
          map[field.id] = { ...field.configMap, users: personDirectory }
          break
        case 'relation': {
          const targetTableId = String(field.configMap.target_table_id ?? '')
          map[field.id] = {
            ...field.configMap,
            labels: relationMetadata[targetTableId]?.labels ?? {},
          }
          break
        }
        case 'select':
        case 'multiselect':
          map[field.id] = { ...field.configMap, createOption: createOptionFor(field) }
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
          map[field.id] = {
            targetType: targetField?.type ?? 'text',
            targetConfig: targetField === undefined ? {} : targetConfigFor(targetField),
          }
          break
        }
        default:
          map[field.id] = field.configMap
          break
      }
    }
    return map
  }, [
    fieldsById,
    personDirectory,
    relationMetadata,
    sortedFields,
  ])

  const clipboardExtras = useMemo(
    () => ({
      personDirectory,
      relationLabels: Object.fromEntries(
        relationFields.flatMap((field) => {
          const targetTableId = String(field.configMap.target_table_id ?? '')
          return Object.entries(relationMetadata[targetTableId]?.labels ?? {})
        }),
      ),
    }),
    [personDirectory, relationFields, relationMetadata],
  )

  // ---- 本地缓存更新：列表 + 单记录，成功后标记失效 ----
  function patchCaches(fieldId: string, value: unknown, version?: number) {
    if (tableId !== null && viewId !== null) {
      queryClient.setQueryData<RecordsData>(listKey, (data) =>
        applyOptimisticCell(data, {
          recordId: recordId,
          fieldId,
          value,
          version,
        }),
      )
    }
    queryClient.setQueryData<RecordRow>(recordsKeys.record(recordId), (current) =>
      current === undefined
        ? current
        : {
            ...current,
            cellsMap: { ...current.cellsMap, [fieldId]: value },
            version: version ?? current.version,
          },
    )
  }

  function invalidateCaches() {
    if (tableId !== null && viewId !== null) {
      void queryClient.invalidateQueries({ queryKey: listKey, refetchType: 'none' })
    }
    void queryClient.invalidateQueries({
      queryKey: recordsKeys.record(recordId),
      refetchType: 'none',
    })
  }

  const recordEditable =
    record !== null && canEditCell && (!editOwnOnly || record.createdBy === currentUserId)

  function fieldEditable(field: FieldRecord): boolean {
    return recordEditable && !READONLY_FIELD_TYPES.has(field.type)
  }

  async function submitField(field: FieldRecord, value: unknown) {
    if (record === null || tableId === null) return
    const before = record.cellsMap[field.id]
    const expectVersion = record.version
    setEdit({ fieldId: field.id, lastValue: value, error: null })
    patchCaches(field.id, value)
    try {
      const result = await trackWrite(() =>
        updateCell(baseId, record.id, field.id, value, expectVersion),
      )
      patchCaches(field.id, result.value, result.version)
      setEdit(null)
      invalidateCaches()
    } catch (failure) {
      // 失败回滚到提交前值（版本保持），编辑器与输入保留
      patchCaches(field.id, before)
      const conflictMeta = parseConflictMeta(failure)
      if (conflictMeta !== null) {
        setConflict({
          kind: 'version',
          fieldId: field.id,
          myValue: value,
          theirValue: conflictMeta.theirValue,
          theirVersion: conflictMeta.theirVersion,
          error: null,
        })
        return
      }
      if (isDeletedError(failure)) {
        setConflict({ kind: 'deleted', fieldId: field.id, myValue: value, error: null })
        return
      }
      setEdit((current) =>
        current === null || current.fieldId !== field.id
          ? current
          : { ...current, lastValue: value, error: apiErrorText(failure) },
      )
    }
  }

  function openField(field: FieldRecord) {
    if (record === null || !fieldEditable(field)) return
    setEdit({ fieldId: field.id, lastValue: record.cellsMap[field.id], error: null })
  }

  function commitFromEditor(field: FieldRecord, value: unknown, _move: EditorCommitMove) {
    void submitField(field, value)
  }

  function draftFromEditor(fieldId: string, value: unknown) {
    setEdit((current) =>
      current === null || current.fieldId !== fieldId
        ? current
        : { ...current, lastValue: value },
    )
  }

  // ---- 上一条 / 下一条：按已加载视图顺序；末行补拉直到命中或没有更多 ----
  function cachedRows(): RecordRow[] {
    const data = queryClient.getQueryData<RecordsData>(listKey)
    if (data === undefined) return records.rows
    return data.pages.flatMap((page) => [...page.items])
  }

  const allRows = cachedRows()
  const currentIndex =
    record === null ? -1 : allRows.findIndex((row) => row.id === record.id)
  const canPrevious = currentIndex > 0
  const canNext =
    currentIndex >= 0 && (currentIndex < allRows.length - 1 || records.hasMore)

  async function navigateBy(direction: -1 | 1) {
    if (record === null) return
    let list = cachedRows()
    const index = list.findIndex((row) => row.id === record.id)
    if (index < 0) return
    const targetIndex = index + direction
    if (targetIndex < 0) return
    while (targetIndex >= list.length) {
      const data = queryClient.getQueryData<RecordsData>(listKey)
      const lastPage = data?.pages[data.pages.length - 1]
      if (lastPage === undefined || !lastPage.hasMore) break
      try {
        await records.fetchNextPage()
      } catch {
        break
      }
      list = cachedRows()
    }
    const target = list[targetIndex]
    if (target !== undefined) {
      setEdit(null)
      onNavigateRecord?.(target.id)
    }
  }

  const conflictField =
    conflict === null ? undefined : fieldsById.get(conflict.fieldId)
  const conflictMyDisplay =
    conflict === null || conflictField === undefined
      ? ''
      : serializeCellValue(conflictField, conflict.myValue, clipboardExtras)
  const conflictTheirDisplay =
    conflict !== null && conflict.kind === 'version' && conflictField !== undefined
      ? serializeCellValue(conflictField, conflict.theirValue, clipboardExtras)
      : ''

  function adoptTheirValue() {
    if (conflict === null || conflict.kind !== 'version') return
    patchCaches(conflict.fieldId, conflict.theirValue, conflict.theirVersion)
    setConflict(null)
    setEdit(null)
    invalidateCaches()
  }

  async function overwriteWithMine() {
    if (conflict === null || conflict.kind !== 'version' || record === null) return
    const { fieldId, myValue, theirVersion } = conflict
    try {
      const result = await trackWrite(() =>
        updateCell(baseId, record.id, fieldId, myValue, theirVersion),
      )
      patchCaches(fieldId, result.value, result.version)
      setConflict(null)
      setEdit(null)
      invalidateCaches()
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
      setConflict(null)
      return
    }
    setConflict(null)
    setEdit(null)
    invalidateCaches()
  }

  function closeConflict() {
    if (conflict === null) return
    const kind = conflict.kind
    setConflict(null)
    if (kind === 'deleted') {
      setEdit(null)
      invalidateCaches()
    }
  }

  function renderValue(field: FieldRecord) {
    if (record === null) return null
    const Renderer = renderers[field.id]?.component
    if (Renderer === undefined) return null
    return <Renderer value={record.cellsMap[field.id]} config={configs[field.id]} />
  }

  function editorNode(field: FieldRecord) {
    if (record === null) return null
    const Editor = editors[field.id]?.component
    if (Editor === undefined) return null
    return (
      <span className={styles.editorHost} data-testid={`drawer-editor-${field.id}`}>
        <Editor
          value={record.cellsMap[field.id]}
          config={configs[field.id]}
          onCommit={(value, move) => commitFromEditor(field, value, move)}
          onReject={() => setEdit(null)}
          onDraftChange={(value) => draftFromEditor(field.id, value)}
        />
      </span>
    )
  }

  function editError(field: FieldRecord) {
    if (edit === null || edit.fieldId !== field.id || edit.error === null) return null
    return (
      <span className={styles.inlineError} role="alert">
        {edit.error}
        <button
          type="button"
          className={styles.smallButton}
          onClick={() => void submitField(field, edit.lastValue)}
        >
          重试
        </button>
      </span>
    )
  }

  function activate(field: FieldRecord) {
    if (!fieldEditable(field)) return
    openField(field)
  }

  function handleActivateKey(event: ReactKeyboardEvent<HTMLElement>, field: FieldRecord) {
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    activate(field)
  }

  const primaryField = sortedFields.find((field) => field.isPrimary) ?? sortedFields[0]
  const otherFields = primaryField === undefined
    ? sortedFields
    : sortedFields.filter((field) => field.id !== primaryField.id)

  function primaryContent(field: FieldRecord) {
    if (record === null) return null
    const readonly = READONLY_FIELD_TYPES.has(field.type)
    const editing = edit?.fieldId === field.id
    return (
      <div className={styles.primary} data-testid="drawer-primary">
        <div className={styles.primaryHeader}>
          <span className={styles.fieldName}>{field.name}</span>
          {readonly ? <span className={styles.readonlyBadge}>只读</span> : null}
        </div>
        {editing ? (
          <>
            {editorNode(field)}
            {editError(field)}
          </>
        ) : fieldEditable(field) ? (
          <div
            role="button"
            tabIndex={0}
            aria-label={`编辑主字段：${field.name}`}
            className={styles.valueButton}
            onClick={() => activate(field)}
            onKeyDown={(event) => handleActivateKey(event, field)}
          >
            {renderValue(field)}
          </div>
        ) : (
          <div className={styles.value}>{renderValue(field)}</div>
        )}
      </div>
    )
  }

  function fieldRow(field: FieldRecord) {
    if (record === null) return null
    const readonly = READONLY_FIELD_TYPES.has(field.type)
    const editing = edit?.fieldId === field.id
    return (
      <div key={field.id} className={styles.fieldRow} data-testid={`drawer-field-${field.id}`}>
        <div className={styles.fieldHeader}>
          <span className={styles.fieldName}>{field.name}</span>
          {readonly ? <span className={styles.readonlyBadge}>只读</span> : null}
        </div>
        {field.type === 'attachment' ? (
          <AttachmentField
            baseId={baseId}
            field={field}
            ids={attachmentIds(record.cellsMap[field.id])}
            editable={recordEditable}
            onApply={(ids) => void submitField(field, ids.length === 0 ? null : ids)}
          />
        ) : editing ? (
          <>
            {editorNode(field)}
            {editError(field)}
          </>
        ) : fieldEditable(field) ? (
          <div
            role="button"
            tabIndex={0}
            aria-label={`编辑字段：${field.name}`}
            className={styles.valueButton}
            onClick={() => activate(field)}
            onKeyDown={(event) => handleActivateKey(event, field)}
          >
            {renderValue(field)}
          </div>
        ) : (
          <div className={styles.value}>{renderValue(field)}</div>
        )}
      </div>
    )
  }

  return (
    <aside
      role="dialog"
      aria-label="记录详情"
      className={styles.drawer}
      onClick={(event) => event.stopPropagation()}
    >
      <header className={styles.header}>
        <h2 className={styles.title}>记录详情</h2>
        <button
          type="button"
          className={styles.closeButton}
          aria-label="关闭记录详情"
          onClick={onClose}
        >
          ×
        </button>
      </header>

      <div className={styles.body}>
        {record === null ? (
          recordQuery.isLoading || records.isFetching ? (
            <p role="status" className={styles.hint}>
              正在加载记录…
            </p>
          ) : (
            <div role="alert" className={styles.inlineError}>
              {recordQuery.isError ? apiErrorTitle(recordQuery.error) : '记录不存在或已被删除'}
              <button
                type="button"
                className={styles.smallButton}
                onClick={() => void recordQuery.refetch()}
              >
                重新加载
              </button>
            </div>
          )
        ) : (
          <>
            {primaryField === undefined ? (
              <p className={styles.hint}>该表暂无字段</p>
            ) : (
              primaryContent(primaryField)
            )}
            <div className={styles.fields} aria-label="字段区">
              {otherFields.map(fieldRow)}
            </div>
          </>
        )}
      </div>

      <footer className={styles.footer}>
        <button
          type="button"
          className={styles.navButton}
          disabled={!canPrevious}
          onClick={() => void navigateBy(-1)}
        >
          上一条
        </button>
        <button
          type="button"
          className={styles.navButton}
          disabled={!canNext}
          onClick={() => void navigateBy(1)}
        >
          下一条
        </button>
      </footer>

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
    </aside>
  )
}
