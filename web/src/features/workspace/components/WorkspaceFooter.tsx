import { useEffect, useState } from 'react'

import { RECORD_LIMIT_MESSAGE } from '@/features/records/capacity'
import { uiStore } from '@/stores'
import { Button } from '@/ui-kit'

import styles from '../BaseWorkspacePage.module.css'

export interface WorkspaceFooterProps {
  /** 已加载记录数（G-3：契约无总数）。 */
  loadedCount?: number
  /** 仍有下一页时展示 n+，不虚构总数。 */
  hasMore?: boolean
  /** 当前 Base（撤销窗口跨页面恢复时校验归属）。 */
  baseId?: string
  /** 权限矩阵：新增入口（可查看隐藏）。 */
  canCreateRecord?: boolean
  /** 权限矩阵：删除/批量条（可填写/可查看隐藏）。 */
  canDeleteRecord?: boolean
  /** 已加载达到单表 5 万行上限：禁用新增并提示联系管理者。 */
  recordLimitReached?: boolean
  /** 勾选的行（批量操作条）。 */
  selectedRecordIds?: readonly string[]
  onCreateRecord?: () => void
  onDeleteSelected?: () => void
  onCopySelected?: () => void
  onUndoDelete?: () => void
}

/** 底栏（票 06）：新增入口 + 批量操作条 + 「已删除 n 条 [撤销]」7 秒窗口 + 已加载计数（G-3）。 */
export function WorkspaceFooter({
  loadedCount = 0,
  hasMore = false,
  baseId,
  canCreateRecord = false,
  canDeleteRecord = false,
  recordLimitReached = false,
  selectedRecordIds = [],
  onCreateRecord,
  onDeleteSelected,
  onCopySelected,
  onUndoDelete,
}: WorkspaceFooterProps) {
  const undoWindow = uiStore((state) => state.undoWindow)
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  // 7 秒撤销窗口：跨页面切换不重置，按 deadline 剩余时间安排清理；到期入口消失。
  useEffect(() => {
    if (undoWindow === null) return undefined
    const remaining = undoWindow.deadlineAt - Date.now()
    if (remaining <= 0) {
      uiStore.getState().clearUndoWindow()
      return undefined
    }
    const timer = setTimeout(() => {
      const current = uiStore.getState().undoWindow
      if (current !== null && current.deadlineAt === undoWindow.deadlineAt) {
        uiStore.getState().clearUndoWindow()
      }
    }, remaining)
    return () => clearTimeout(timer)
  }, [undoWindow])

  const undoVisible =
    undoWindow !== null &&
    undoWindow.recordIds.length > 0 &&
    (baseId === undefined || undoWindow.baseId === baseId)
  const selectedCount = selectedRecordIds.length
  const batchVisible = selectedCount > 0 && (canCreateRecord || canDeleteRecord)

  return (
    <footer role="contentinfo" aria-label="底栏" className={styles.footer}>
      <div className={styles.footerLeft}>
        {canCreateRecord ? (
          <Button
            variant="secondary"
            disabled={recordLimitReached}
            title={recordLimitReached ? RECORD_LIMIT_MESSAGE : undefined}
            onClick={onCreateRecord}
          >
            + 新增记录
          </Button>
        ) : null}
        {recordLimitReached ? (
          <span className={styles.footerHint} role="status">
            {RECORD_LIMIT_MESSAGE}
          </span>
        ) : null}

        {batchVisible ? (
          <span className={styles.batchBar} aria-label="批量操作">
            <span>已选择 {selectedCount} 条</span>
            {canCreateRecord ? (
              <Button variant="secondary" onClick={onCopySelected}>
                复制记录
              </Button>
            ) : null}
            {canDeleteRecord ? (
              <Button variant="danger" onClick={() => setConfirmingDelete(true)}>
                删除
              </Button>
            ) : null}
          </span>
        ) : null}

        {undoVisible ? (
          <span className={styles.undoBar} role="status">
            <span>已删除 {undoWindow.recordIds.length} 条</span>
            <Button variant="secondary" onClick={onUndoDelete}>
              撤销
            </Button>
          </span>
        ) : null}
      </div>
      <span className={styles.footerCount} data-testid="footer-record-count">
        已加载 {loadedCount}
        {hasMore ? '+' : ''} 条
      </span>

      {confirmingDelete ? (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="record-batch-delete-title"
            className={styles.dialog}
          >
            <h2 id="record-batch-delete-title" className={styles.dialogTitle}>
              删除记录
            </h2>
            <p>确定删除选中的 {selectedCount} 条记录吗？删除后可在 7 秒内撤销。</p>
            <div className={styles.dialogActions}>
              <Button
                variant="danger"
                onClick={() => {
                  setConfirmingDelete(false)
                  onDeleteSelected?.()
                }}
              >
                确认删除
              </Button>
              <Button variant="secondary" onClick={() => setConfirmingDelete(false)}>
                取消
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </footer>
  )
}
