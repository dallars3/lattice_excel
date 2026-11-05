import { useState } from 'react'

import { useQueryClient } from '@tanstack/react-query'

import { fieldKeys, recordsKeys, viewKeys } from '@/query-keys'
import { trackWrite, workspaceStore } from '@/stores'
import { useOptionalToast, Button } from '@/ui-kit'

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
import { FieldHeaderRow, type FieldMenuAction } from './field/FieldHeaderRow'
import { FieldRenameDialog } from './field/FieldRenameDialog'
import { OptionsEditor } from './field/OptionsEditor'
import {
  buildDeleteImpact,
  isFieldCapacityReached,
  type DeleteFieldImpactView,
} from './field/impact'
import styles from './field/FieldPanels.module.css'

export interface GridAreaProps {
  tableId: string | null
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
  loadedRecordCount?: number
  hasMoreRecords?: boolean
}

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
}: GridAreaProps) {
  const queryClient = useQueryClient()
  const toast = useOptionalToast()
  const descriptions = workspaceStore((state) => state.fieldDescriptions)
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const atCapacity = isFieldCapacityReached(fields.length)

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
      ) : (
        <>
          <FieldHeaderRow
            fields={fields}
            descriptions={descriptions}
            canManageStructure={canManageStructure}
            canEditFields={canEditFields}
            atCapacity={atCapacity}
            onCreate={() => setDialog({ kind: 'create' })}
            onAction={openAction}
          />
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

      <div className={styles.gridBody}>
        <p className={styles.skeleton}>记录将在后续版本接入</p>
      </div>

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
