import { useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'

import type { FieldRecord } from '../../api/field'
import type { TableSummary } from '../../api/table'
import { apiErrorText } from '../../errorText'
import { isOptionFieldType, type FieldOptionValue } from '../../types/field-config'
import {
  DateConfigFields,
  FormulaConfigFields,
  LookupConfigFields,
  NumberConfigFields,
  PersonConfigFields,
  RelationConfigFields,
  mergeConfig,
  relationFieldsOf,
  type ConfigValue,
} from './ConfigFields'
import { OptionsEditor } from './OptionsEditor'
import styles from './FieldPanels.module.css'

export interface FieldConfigPanelProps {
  field: FieldRecord
  tables: TableSummary[]
  currentTableFields: FieldRecord[]
  onSaveConfig: (configMap: ConfigValue) => Promise<void>
  onSaveOptions: (options: FieldOptionValue[], allowCreate: boolean) => Promise<void>
  onClose: () => void
}

/** 字段配置面板：按类型渲染专有参数；单选/多选走选项维护（H-40），其余走 H-37。 */
export function FieldConfigPanel({
  field,
  tables,
  currentTableFields,
  onSaveConfig,
  onSaveOptions,
  onClose,
}: FieldConfigPanelProps) {
  const [config, setConfig] = useState<ConfigValue>(() => mergeConfig(field.type, field.configMap))
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const relationFields = relationFieldsOf(currentTableFields)

  function validate(): string | null {
    switch (field.type) {
      case 'number': {
        const decimals = config.decimals
        if (typeof decimals !== 'number' || decimals < 0 || decimals > 4) {
          return '小数位数需为 0–4 的整数'
        }
        return null
      }
      case 'formula':
        return String(config.expression ?? '').trim() === '' ? '公式表达式不可为空' : null
      case 'relation':
        return String(config.target_table_id ?? '') === '' ? '请选择目标数据表' : null
      case 'lookup':
        if (String(config.relation_field_id ?? '') === '') return '请选择关联字段'
        if (String(config.target_field_id ?? '') === '') return '请选择目标字段'
        return null
      default:
        return null
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    const message = validate()
    if (message !== null) {
      setError(message)
      return
    }
    setError(null)
    setPending(true)
    onSaveConfig(config)
      .then(onClose)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  if (isOptionFieldType(field.type)) {
    return (
      <div className={styles.mask}>
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="field-config-dialog-title"
          className={styles.dialog}
        >
          <h2 id="field-config-dialog-title" className={styles.dialogTitle}>
            字段配置：{field.name}
          </h2>
          <OptionsEditor configMap={config} onSave={onSaveOptions} onClose={onClose} />
        </div>
      </div>
    )
  }

  const readonlyType = field.type === 'text' || field.type === 'attachment' || field.type === 'url'

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="field-config-dialog-title"
        className={styles.dialog}
      >
        <h2 id="field-config-dialog-title" className={styles.dialogTitle}>
          字段配置：{field.name}
        </h2>
        {readonlyType ? (
          <>
            <p className={styles.hint}>该类型暂无可配置参数。</p>
            <div className={styles.dialogActions}>
              <Button variant="secondary" onClick={onClose}>
                关闭
              </Button>
            </div>
          </>
        ) : (
          <form className={styles.form} onSubmit={submit}>
            {field.type === 'number' ? (
              <NumberConfigFields config={config} onChange={setConfig} />
            ) : field.type === 'date' ? (
              <DateConfigFields config={config} onChange={setConfig} />
            ) : field.type === 'person' ? (
              <PersonConfigFields config={config} onChange={setConfig} />
            ) : field.type === 'formula' ? (
              <FormulaConfigFields config={config} onChange={setConfig} />
            ) : field.type === 'relation' ? (
              <RelationConfigFields config={config} onChange={setConfig} tables={tables} />
            ) : (
              <LookupConfigFields
                config={config}
                onChange={setConfig}
                relationFields={relationFields}
              />
            )}
            {error === null ? null : <p role="alert">{error}</p>}
            <div className={styles.dialogActions}>
              <Button type="submit" disabled={pending}>
                保存
              </Button>
              <Button variant="secondary" onClick={onClose}>
                取消
              </Button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}
