import { useState } from 'react'

import { Button } from '@/ui-kit'

import type { FieldRecord } from '../../api/field'
import { apiErrorText } from '../../errorText'
import type { DeleteFieldImpactView } from './impact'
import styles from './FieldPanels.module.css'

export interface FieldDeleteDialogProps {
  field: FieldRecord
  impact: DeleteFieldImpactView
  onConfirm: () => Promise<number | void>
  onClose: () => void
}

/** 删除字段确认：展示影响面（已加载记录数/依赖公式数/依赖引用数）并二次确认。 */
export function FieldDeleteDialog({ field, impact, onConfirm, onClose }: FieldDeleteDialogProps) {
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function confirm() {
    setError(null)
    setPending(true)
    onConfirm()
      .then(onClose)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="field-delete-dialog-title"
        className={styles.dialog}
      >
        <h2 id="field-delete-dialog-title" className={styles.dialogTitle}>
          删除字段
        </h2>
        <div className={styles.form}>
          <p>
            确定删除「{field.name}」吗？删除后字段与全部记录值不可恢复。
          </p>
          <p>
            影响记录：当前已加载 {impact.recordCount} 条
            {impact.hasMoreRecords ? '（另有更多未加载）' : ''}
          </p>
          <p>依赖该字段的公式：{impact.dependentFormulas} 个</p>
          <p>依赖该字段的引用：{impact.dependentLookups} 个</p>
          <p className={styles.dangerHint}>
            相关公式/引用将失效；引用该字段的视图条件会被自动移除。
          </p>
          {error === null ? null : <p role="alert">{error}</p>}
          <div className={styles.dialogActions}>
            <Button variant="danger" disabled={pending} onClick={confirm}>
              确认删除
            </Button>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
