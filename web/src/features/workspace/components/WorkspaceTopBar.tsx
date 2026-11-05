import { useEffect, useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'

import { PermissionGate, usePermission } from '@/features/permissions'
import type { BaseSummary } from '@/features/home/api'
import type { SaveState } from '@/stores'

import { apiErrorText } from '../errorText'
import styles from '../BaseWorkspacePage.module.css'

const MAX_BASE_NAME_LENGTH = 50

const SAVE_STATE_LABEL: Record<SaveState, string> = {
  saving: '保存中…',
  saved: '已保存',
  offline: '离线',
}

export interface WorkspaceTopBarProps {
  base: BaseSummary
  saveState: SaveState
  onRename: (name: string) => Promise<void>
  onDuplicate: () => Promise<void>
  onDelete: () => Promise<void>
  onToggleStar: () => Promise<void>
}

/** 工作台顶栏：Base 名称、保存状态位、在线头像占位、分享入口位与「更多」菜单（按角色显隐）。 */
export function WorkspaceTopBar({
  base,
  saveState,
  onRename,
  onDuplicate,
  onDelete,
  onToggleStar,
}: WorkspaceTopBarProps) {
  const permission = usePermission(base)
  const canManageBase = permission.resolve('manage_base') === 'allow'
  const shareState = permission.resolve('manage_share')
  // 分享入口位（M4 占位）：可管理/可编辑可见；可填写/可查看/外部隐藏。
  const canShare = permission.internal && (shareState === 'allow' || shareState === 'readonly')
  // 顶栏「更多」：可管理全项；可编辑仅保留星标；可填写/可查看/外部隐藏（需求 §5.1）。
  const showMenu = permission.internal && (canManageBase || permission.role === 'editor')

  const [menuOpen, setMenuOpen] = useState(false)
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameDraft, setRenameDraft] = useState('')
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 票 09：权限中途降级后立即收起写入口与已打开的 Base 管理对话框。
  useEffect(() => {
    if (canManageBase) return
    setMenuOpen(false)
    setRenameOpen(false)
    setDeleteOpen(false)
  }, [canManageBase])

  function resetError() {
    setError(null)
  }

  function openRename() {
    setMenuOpen(false)
    setRenameDraft(base.name)
    resetError()
    setRenameOpen(true)
  }

  function openDelete() {
    setMenuOpen(false)
    resetError()
    setDeleteOpen(true)
  }

  async function runAction(action: () => Promise<void>) {
    resetError()
    setPending(true)
    try {
      await action()
    } catch (failure) {
      setError(apiErrorText(failure))
    } finally {
      setPending(false)
    }
  }

  function submitRename(event: FormEvent) {
    event.preventDefault()
    const name = renameDraft.trim()
    if (name.length < 1 || name.length > MAX_BASE_NAME_LENGTH) {
      setError(`名称需为 1–${MAX_BASE_NAME_LENGTH} 个字符`)
      return
    }
    void runAction(async () => {
      await onRename(name)
      setRenameOpen(false)
    })
  }

  function confirmDelete() {
    void runAction(async () => {
      await onDelete()
      setDeleteOpen(false)
    })
  }

  return (
    <header role="banner" aria-label="工作台顶栏" className={styles.topBar}>
      <div className={styles.topBarMain}>
        <h1 className={styles.topBarName}>{base.name}</h1>
        <span className={styles.saveStatus} aria-live="polite" data-state={saveState}>
          {SAVE_STATE_LABEL[saveState]}
        </span>
      </div>
      <div className={styles.topBarActions}>
        <div className={styles.avatarSlot} aria-label="在线协作者头像（M3 接入）">
          在线头像（M3）
        </div>
        {canShare ? (
          <Button variant="secondary" disabled>
            分享
          </Button>
        ) : null}
        {showMenu ? (
          <div className={styles.menuWrap}>
            <Button
              variant="secondary"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              更多
            </Button>
            {menuOpen ? (
              <div className={styles.menu} role="menu" aria-label="Base 操作菜单">
                <PermissionGate state={permission.resolve('manage_base')}>
                  <button type="button" role="menuitem" onClick={openRename}>
                    重命名
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    disabled={pending}
                    onClick={() => {
                      setMenuOpen(false)
                      void runAction(onDuplicate)
                    }}
                  >
                    复制
                  </button>
                  <button type="button" role="menuitem" onClick={openDelete}>
                    删除
                  </button>
                </PermissionGate>
                <PermissionGate state={permission.resolve('star')}>
                  <button
                    type="button"
                    role="menuitem"
                    disabled={pending}
                    onClick={() => {
                      setMenuOpen(false)
                      void runAction(onToggleStar)
                    }}
                  >
                    {base.starred ? '取消星标' : '星标'}
                  </button>
                </PermissionGate>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>

      {error === null || renameOpen || deleteOpen ? null : (
        <p role="alert" className={styles.inlineError}>
          {error}
        </p>
      )}

      {renameOpen ? (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="rename-base-title"
            className={styles.dialog}
          >
            <h2 id="rename-base-title" className={styles.dialogTitle}>
              重命名多维表格
            </h2>
            <form onSubmit={submitRename} className={styles.form}>
              <label htmlFor="workspace-base-name">Base 名称</label>
              <input
                id="workspace-base-name"
                value={renameDraft}
                maxLength={MAX_BASE_NAME_LENGTH}
                onChange={(event) => {
                  setRenameDraft(event.target.value)
                  resetError()
                }}
              />
              {error === null ? null : <p role="alert">{error}</p>}
              <div className={styles.dialogActions}>
                <Button type="submit" disabled={pending || renameDraft.trim() === ''}>
                  保存
                </Button>
                <Button variant="secondary" onClick={() => setRenameOpen(false)}>
                  取消
                </Button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

      {deleteOpen ? (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-base-title"
            className={styles.dialog}
          >
            <h2 id="delete-base-title" className={styles.dialogTitle}>
              删除多维表格
            </h2>
            <p>确定删除「{base.name}」吗？</p>
            <p className={styles.dangerHint}>删除后不可恢复（P0）</p>
            {error === null ? null : <p role="alert">{error}</p>}
            <div className={styles.dialogActions}>
              <Button variant="danger" disabled={pending} onClick={confirmDelete}>
                确认删除
              </Button>
              <Button variant="secondary" onClick={() => setDeleteOpen(false)}>
                取消
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </header>
  )
}
