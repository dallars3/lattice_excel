import { useState } from 'react'

import { Button } from '@/ui-kit'

import type { FieldConversionResult, FieldRecord } from '../../api/field'
import { apiErrorText } from '../../errorText'
import {
  FIELD_TYPE_OPTIONS,
  allowedConvertTargets,
  type FieldType,
} from '../../types/field-config'
import styles from './FieldPanels.module.css'

export interface FieldConvertDialogProps {
  field: FieldRecord
  onConvert: (targetType: FieldType, confirm: boolean) => Promise<FieldConversionResult>
  onClose: () => void
}

/** 类型转换对话框：先 confirm=false 预览受影响数/将清空数，再 confirm=true 提交。 */
export function FieldConvertDialog({ field, onConvert, onClose }: FieldConvertDialogProps) {
  const targets = allowedConvertTargets(field.type)
  const [target, setTarget] = useState<FieldType>(targets[0] ?? 'text')
  const [preview, setPreview] = useState<FieldConversionResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  function changeTarget(next: FieldType) {
    setTarget(next)
    setPreview(null)
    setError(null)
  }

  function runPreview() {
    setError(null)
    setPending(true)
    onConvert(target, false)
      .then((result) => setPreview(result))
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  function confirm() {
    setError(null)
    setPending(true)
    onConvert(target, true)
      .then(onClose)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="field-convert-dialog-title"
        className={styles.dialog}
      >
        <h2 id="field-convert-dialog-title" className={styles.dialogTitle}>
          更改字段类型
        </h2>
        <div className={styles.form}>
          <label htmlFor="field-convert-target">目标类型</label>
          <select
            id="field-convert-target"
            value={target}
            onChange={(event) => changeTarget(event.target.value as FieldType)}
          >
            {targets.map((value) => {
              const option = FIELD_TYPE_OPTIONS.find((entry) => entry.type === value)
              return (
                <option key={value} value={value}>
                  {option?.label ?? value}
                </option>
              )
            })}
          </select>
          {preview === null ? null : (
            <p role="status">
              受影响记录 {preview.affectedCount} 条，将清空值 {preview.clearedCount} 个
            </p>
          )}
          {error === null ? null : <p role="alert">{error}</p>}
          <div className={styles.dialogActions}>
            <Button variant="secondary" disabled={pending} onClick={runPreview}>
              预览影响
            </Button>
            <Button disabled={pending || preview === null} onClick={confirm}>
              确认转换
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
