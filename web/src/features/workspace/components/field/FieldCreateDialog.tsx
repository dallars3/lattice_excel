import { useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'
import { isApiError } from '@/core/http'

import type { CreateFieldInput, FieldRecord } from '../../api/field'
import type { TableSummary } from '../../api/table'
import { apiErrorText } from '../../errorText'
import {
  ALL_FIELD_TYPES,
  FIELD_TYPE_OPTIONS,
  defaultFieldConfig,
  type FieldType,
} from '../../types/field-config'
import {
  FormulaConfigFields,
  LookupConfigFields,
  RelationConfigFields,
  relationFieldsOf,
  type ConfigValue,
} from './ConfigFields'
import {
  FIELD_CREATE_LIMIT_MESSAGE,
  FIELD_LIMIT_MESSAGE,
  MAX_FIELD_DESCRIPTION_LENGTH,
  isFieldCapacityReached,
  validateFieldName,
} from './impact'
import styles from './FieldPanels.module.css'

export interface FieldCreateDialogProps {
  fields: FieldRecord[]
  tables: TableSummary[]
  onCreate: (input: CreateFieldInput, description: string) => Promise<void>
  onClose: () => void
}

function createErrorText(error: unknown): string {
  if (isApiError(error) && error.code === 5001) {
    return `${FIELD_LIMIT_MESSAGE}。已达容量上限，请清理不需要的数据后重试。`
  }
  return apiErrorText(error)
}

/** 新增字段对话框：11 种类型 + 名称/描述 + 类型所需的最小初始配置。 */
export function FieldCreateDialog({
  fields,
  tables,
  onCreate,
  onClose,
}: FieldCreateDialogProps) {
  const [type, setType] = useState<FieldType>('text')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [config, setConfig] = useState<ConfigValue>(() => defaultFieldConfig('text'))
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  const typeOption = FIELD_TYPE_OPTIONS.find((option) => option.type === type)
  const relationFields = relationFieldsOf(fields)

  function changeType(next: FieldType) {
    setType(next)
    setConfig(defaultFieldConfig(next))
    setError(null)
  }

  function validate(): string | null {
    const nameError = validateFieldName(name, fields)
    if (nameError !== null) return nameError
    if (Array.from(description.trim()).length > MAX_FIELD_DESCRIPTION_LENGTH) {
      return `字段描述不能超过 ${MAX_FIELD_DESCRIPTION_LENGTH} 个字符`
    }
    if (type === 'formula') {
      const expression = String(config.expression ?? '').trim()
      if (expression === '') return '公式表达式不可为空'
    }
    if (type === 'relation' && String(config.target_table_id ?? '') === '') {
      return '请选择目标数据表'
    }
    if (type === 'lookup') {
      if (String(config.relation_field_id ?? '') === '') return '请选择关联字段'
      if (String(config.target_field_id ?? '') === '') return '请选择目标字段'
    }
    return null
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    if (isFieldCapacityReached(fields.length)) {
      setError(FIELD_CREATE_LIMIT_MESSAGE)
      return
    }
    const message = validate()
    if (message !== null) {
      setError(message)
      return
    }
    setError(null)
    setPending(true)
    onCreate({ name: name.trim(), type, configMap: config }, description.trim())
      .then(onClose)
      .catch((failure: unknown) => setError(createErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <div className={styles.mask}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="field-create-dialog-title"
        className={styles.dialog}
      >
        <h2 id="field-create-dialog-title" className={styles.dialogTitle}>
          新增字段
        </h2>
        <form className={styles.form} onSubmit={submit}>
          <label htmlFor="field-type">字段类型</label>
          <select
            id="field-type"
            value={type}
            onChange={(event) => changeType(event.target.value as FieldType)}
          >
            {ALL_FIELD_TYPES.map((value) => {
              const option = FIELD_TYPE_OPTIONS.find((entry) => entry.type === value)
              return (
                <option key={value} value={value}>
                  {option?.label ?? value}
                </option>
              )
            })}
          </select>
          {typeOption === undefined ? null : <p className={styles.hint}>{typeOption.hint}</p>}

          <label htmlFor="field-name">字段名称</label>
          <input
            id="field-name"
            value={name}
            maxLength={30}
            placeholder="1–30 个字符"
            onChange={(event) => {
              setName(event.target.value)
              setError(null)
            }}
          />

          <label htmlFor="field-description">字段描述</label>
          <textarea
            id="field-description"
            value={description}
            maxLength={MAX_FIELD_DESCRIPTION_LENGTH}
            placeholder="悬停字段名时展示（可选，≤500 字符）"
            onChange={(event) => {
              setDescription(event.target.value)
              setError(null)
            }}
          />

          {type === 'formula' ? (
            <FormulaConfigFields config={config} onChange={setConfig} />
          ) : type === 'relation' ? (
            <RelationConfigFields config={config} onChange={setConfig} tables={tables} />
          ) : type === 'lookup' ? (
            <LookupConfigFields
              config={config}
              onChange={setConfig}
              relationFields={relationFields}
            />
          ) : null}

          {error === null ? null : <p role="alert">{error}</p>}
          <div className={styles.dialogActions}>
            <Button type="submit" disabled={pending || name.trim() === ''}>
              创建
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
