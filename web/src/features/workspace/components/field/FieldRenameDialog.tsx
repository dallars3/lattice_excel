import { useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'

import type { FieldRecord } from '../../api/field'
import { apiErrorText } from '../../errorText'
import { validateFieldName } from './impact'
import styles from './FieldPanels.module.css'

export interface FieldRenameDialogProps {
  field: FieldRecord
  fields: FieldRecord[]
  onRename: (name: string) => Promise<void>
  onClose: () => void
}

/** 重命名字段对话框：名称 1–30 且同表内不可重名（主字段可改名）。 */
export function FieldRenameDialog({ field, fields, onRename, onClose }: FieldRenameDialogProps) {
  const [draft, setDraft] = useState(field.name)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function submit(event: FormEvent) {
    event.preventDefault()
    const message = validateFieldName(draft, fields, field.id)
    if (message !== null) {
      setError(message)
      return
    }
    setError(null)
    setPending(true)
    onRename(draft.trim())
      .then(onClose)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="field-rename-dialog-title"
        className={styles.dialog}
      >
        <h2 id="field-rename-dialog-title" className={styles.dialogTitle}>
          重命名字段
        </h2>
        <form className={styles.form} onSubmit={submit}>
          <label htmlFor="field-rename-name">字段名称</label>
          <input
            id="field-rename-name"
            value={draft}
            maxLength={30}
            onChange={(event) => {
              setDraft(event.target.value)
              setError(null)
            }}
          />
          {error === null ? null : <p role="alert">{error}</p>}
          <div className={styles.dialogActions}>
            <Button type="submit" disabled={pending || draft.trim() === ''}>
              保存
            </Button>
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}
