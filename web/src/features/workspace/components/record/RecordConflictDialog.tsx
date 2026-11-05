// 记录写入冲突弹窗（票 06 / 需求 4.6 / FR-06-3）：
// - 4001：展示对方新值 + 三动作（采用对方 / 仍用我的覆盖 / 复制我的内容），禁止静默覆盖；
// - 4002：记录或字段已删除，说明原因并保留内容可复制，不自动重建。
import { Button } from '@/ui-kit'

import styles from '../field/FieldPanels.module.css'

export interface RecordConflictDialogProps {
  /** version=4001 版本冲突；deleted=4002 记录/字段已删除。 */
  readonly kind: 'version' | 'deleted'
  /** 4001 对方新值的可读文本。 */
  readonly theirDisplay?: string
  /** 我的编辑内容可读文本（复制用）。 */
  readonly myDisplay: string
  readonly error?: string | null
  readonly pending?: boolean
  readonly onAdopt?: () => void
  readonly onOverwrite?: () => void
  readonly onCopy: () => void
  readonly onClose: () => void
}

export function RecordConflictDialog({
  kind,
  theirDisplay = '',
  myDisplay,
  error = null,
  pending = false,
  onAdopt,
  onOverwrite,
  onCopy,
  onClose,
}: RecordConflictDialogProps) {
  const isVersion = kind === 'version'
  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="record-conflict-dialog-title"
        className={styles.dialog}
      >
        <h2 id="record-conflict-dialog-title" className={styles.dialogTitle}>
          {isVersion ? '内容已被他人修改' : '该记录或字段已被删除'}
        </h2>
        <div className={styles.form}>
          {isVersion ? (
            <>
              <p>该单元格已被修改为「{theirDisplay}」。</p>
              <p>我的内容：{myDisplay === '' ? '（空）' : myDisplay}</p>
              <p className={styles.hint}>
                请选择处理方式：采用对方的值、仍用我的覆盖，或复制我的内容后再决定。
              </p>
            </>
          ) : (
            <>
              <p>该记录或字段已被删除，编辑内容已保留可复制，不会自动重建。</p>
              <p>我的内容：{myDisplay === '' ? '（空）' : myDisplay}</p>
            </>
          )}
          {error === null ? null : <p role="alert">{error}</p>}
          <div className={styles.dialogActions}>
            {isVersion && onAdopt !== undefined ? (
              <Button variant="secondary" disabled={pending} onClick={onAdopt}>
                采用对方
              </Button>
            ) : null}
            {isVersion && onOverwrite !== undefined ? (
              <Button variant="danger" disabled={pending} onClick={onOverwrite}>
                仍用我的覆盖
              </Button>
            ) : null}
            <Button variant="secondary" disabled={pending} onClick={onCopy}>
              复制我的内容
            </Button>
            <Button variant="secondary" disabled={pending} onClick={onClose}>
              关闭
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
