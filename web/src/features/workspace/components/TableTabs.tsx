import { useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'
import { workspaceStore } from '@/stores'

import type { TableSummary } from '../api/table'
import { apiErrorText } from '../errorText'
import styles from '../BaseWorkspacePage.module.css'

const MIN_TABLE_NAME_LENGTH = 1
const MAX_TABLE_NAME_LENGTH = 30

export interface TableTabsProps {
  tables: TableSummary[]
  currentTableId: string | null
  canManageStructure: boolean
  onSelect: (tableId: string) => void
  onCreate: (name: string) => Promise<void>
  onRename: (tableId: string, name: string) => Promise<void>
  onDuplicate: (tableId: string) => Promise<void>
  onDelete: (tableId: string) => Promise<void>
}

type DialogMode = 'create' | 'rename' | 'delete'

interface DialogState {
  mode: DialogMode
  table: TableSummary | null
  draft: string
}

/** 数据表标签页：切换、结构管理入口（仅 manage_structure=allow）与在途写「保存中」微状态。 */
export function TableTabs({
  tables,
  currentTableId,
  canManageStructure,
  onSelect,
  onCreate,
  onRename,
  onDuplicate,
  onDelete,
}: TableTabsProps) {
  const saving = workspaceStore((state) => state.inFlightWriteCount > 0)
  const [menuTableId, setMenuTableId] = useState<string | null>(null)
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function resetError() {
    setError(null)
  }

  function openDialog(mode: DialogMode, table: TableSummary | null) {
    setMenuTableId(null)
    setError(null)
    setDialog({ mode, table, draft: table?.name ?? '' })
  }

  function validateName(name: string): boolean {
    return name.length >= MIN_TABLE_NAME_LENGTH && name.length <= MAX_TABLE_NAME_LENGTH
  }

  function submitName(event: FormEvent) {
    event.preventDefault()
    if (dialog === null) return
    const name = dialog.draft.trim()
    if (!validateName(name)) {
      setError(`名称需为 ${MIN_TABLE_NAME_LENGTH}–${MAX_TABLE_NAME_LENGTH} 个字符`)
      return
    }
    setError(null)
    setPending(true)
    const action =
      dialog.mode === 'create'
        ? onCreate(name)
        : dialog.table === null
          ? Promise.resolve()
          : onRename(dialog.table.id, name)
    action
      .then(() => setDialog(null))
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  function confirmDelete() {
    if (dialog === null || dialog.table === null) return
    setError(null)
    setPending(true)
    onDelete(dialog.table.id)
      .then(() => setDialog(null))
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  function runDuplicate(table: TableSummary) {
    setMenuTableId(null)
    setError(null)
    setPending(true)
    onDuplicate(table.id)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <div className={styles.tabsRow}>
      <div role="tablist" aria-label="数据表" className={styles.tabList}>
        {tables.map((table) => {
          const active = table.id === currentTableId
          return (
            <div key={table.id} className={styles.tabWrap}>
              <button
                type="button"
                role="tab"
                aria-selected={active}
                className={active ? `${styles.tab} ${styles.tabActive}` : styles.tab}
                onClick={() => onSelect(table.id)}
              >
                <span>{table.name}</span>
                {saving && active ? <span className={styles.tabBadge}>保存中</span> : null}
              </button>
              {canManageStructure ? (
                <>
                  <button
                    type="button"
                    className={styles.tabMenuButton}
                    aria-label={`表操作：${table.name}`}
                    aria-haspopup="menu"
                    aria-expanded={menuTableId === table.id}
                    onClick={() =>
                      setMenuTableId((current) => (current === table.id ? null : table.id))
                    }
                  >
                    ⋯
                  </button>
                  {menuTableId === table.id ? (
                    <div className={styles.menu} role="menu" aria-label={`${table.name} 操作菜单`}>
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => openDialog('rename', table)}
                      >
                        重命名表
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        disabled={pending}
                        onClick={() => runDuplicate(table)}
                      >
                        复制表
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        disabled={tables.length <= 1}
                        onClick={() => openDialog('delete', table)}
                      >
                        删除表
                      </button>
                    </div>
                  ) : null}
                </>
              ) : null}
            </div>
          )
        })}
      </div>
      {canManageStructure ? (
        <Button variant="secondary" onClick={() => openDialog('create', null)}>
          新建数据表
        </Button>
      ) : null}
      {error !== null && dialog === null ? (
        <p role="alert" className={styles.inlineError}>
          {error}
        </p>
      ) : null}

      {dialog === null ? null : (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="table-dialog-title"
            className={styles.dialog}
          >
            <h2 id="table-dialog-title" className={styles.dialogTitle}>
              {dialog.mode === 'create'
                ? '新建数据表'
                : dialog.mode === 'rename'
                  ? '重命名数据表'
                  : '删除数据表'}
            </h2>
            {dialog.mode === 'delete' ? (
              <>
                <p>确定删除「{dialog.table?.name}」吗？</p>
                <p className={styles.dangerHint}>删除后不可恢复（P0）</p>
                {error === null ? null : <p role="alert">{error}</p>}
                <div className={styles.dialogActions}>
                  <Button variant="danger" disabled={pending} onClick={confirmDelete}>
                    确认删除
                  </Button>
                  <Button variant="secondary" onClick={() => setDialog(null)}>
                    取消
                  </Button>
                </div>
              </>
            ) : (
              <form onSubmit={submitName} className={styles.form}>
                <label htmlFor="table-name">数据表名称</label>
                <input
                  id="table-name"
                  value={dialog.draft}
                  maxLength={MAX_TABLE_NAME_LENGTH}
                  onChange={(event) => {
                    setDialog({ ...dialog, draft: event.target.value })
                    resetError()
                  }}
                />
                {error === null ? null : <p role="alert">{error}</p>}
                <div className={styles.dialogActions}>
                  <Button
                    type="submit"
                    disabled={pending || dialog.draft.trim() === ''}
                  >
                    {dialog.mode === 'create' ? '创建' : '保存'}
                  </Button>
                  <Button variant="secondary" onClick={() => setDialog(null)}>
                    取消
                  </Button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
