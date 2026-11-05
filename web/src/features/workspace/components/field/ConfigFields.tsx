import { useQuery } from '@tanstack/react-query'

import { fieldKeys } from '@/query-keys'

import { fetchFields, type FieldRecord } from '../../api/field'
import {
  DATE_FORMATS,
  NUMBER_CURRENCIES,
  NUMBER_FORMATS,
  defaultFieldConfig,
} from '../../types/field-config'

import styles from './FieldPanels.module.css'

export type ConfigValue = Record<string, unknown>

/** 合并类型默认配置与后端已有配置，避免控件读到 undefined。 */
export function mergeConfig(
  type: FieldRecord['type'],
  configMap: ConfigValue,
): Required<ConfigValue> & ConfigValue {
  return { ...defaultFieldConfig(type), ...configMap }
}

function stringValue(config: ConfigValue, key: string, fallback: string): string {
  const value = config[key]
  return typeof value === 'string' ? value : fallback
}

function numberValue(config: ConfigValue, key: string, fallback: number): number {
  const value = config[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

function booleanValue(config: ConfigValue, key: string, fallback: boolean): boolean {
  const value = config[key]
  return typeof value === 'boolean' ? value : fallback
}

const NUMBER_FORMAT_LABELS: Record<(typeof NUMBER_FORMATS)[number], string> = {
  int: '整数',
  decimal: '小数',
  percent: '百分比',
  currency: '货币',
}

export function NumberConfigFields({
  config,
  onChange,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
}) {
  return (
    <>
      <label htmlFor="number-format">数字格式</label>
      <select
        id="number-format"
        value={stringValue(config, 'format', 'int')}
        onChange={(event) => onChange({ ...config, format: event.target.value })}
      >
        {NUMBER_FORMATS.map((format) => (
          <option key={format} value={format}>
            {NUMBER_FORMAT_LABELS[format]}
          </option>
        ))}
      </select>
      <label htmlFor="number-decimals">小数位数（0–4）</label>
      <input
        id="number-decimals"
        type="number"
        min={0}
        max={4}
        value={numberValue(config, 'decimals', 0)}
        onChange={(event) =>
          onChange({ ...config, decimals: Number.parseInt(event.target.value, 10) || 0 })
        }
      />
      <label htmlFor="number-currency">货币符号</label>
      <select
        id="number-currency"
        value={stringValue(config, 'currency', '¥')}
        onChange={(event) => onChange({ ...config, currency: event.target.value })}
      >
        {NUMBER_CURRENCIES.map((currency) => (
          <option key={currency} value={currency}>
            {currency}
          </option>
        ))}
      </select>
      <label className={styles.checkboxRow} htmlFor="number-thousands">
        <input
          id="number-thousands"
          type="checkbox"
          checked={booleanValue(config, 'thousands', false)}
          onChange={(event) => onChange({ ...config, thousands: event.target.checked })}
        />
        使用千分位
      </label>
    </>
  )
}

export function DateConfigFields({
  config,
  onChange,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
}) {
  return (
    <>
      <label className={styles.checkboxRow} htmlFor="date-include-time">
        <input
          id="date-include-time"
          type="checkbox"
          checked={booleanValue(config, 'include_time', false)}
          onChange={(event) => onChange({ ...config, include_time: event.target.checked })}
        />
        包含时间
      </label>
      <label htmlFor="date-format">日期格式</label>
      <select
        id="date-format"
        value={stringValue(config, 'format', 'YYYY-MM-DD')}
        onChange={(event) => onChange({ ...config, format: event.target.value })}
      >
        {DATE_FORMATS.map((format) => (
          <option key={format} value={format}>
            {format}
          </option>
        ))}
      </select>
    </>
  )
}

export function PersonConfigFields({
  config,
  onChange,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
}) {
  return (
    <label className={styles.checkboxRow} htmlFor="person-allow-multiple">
      <input
        id="person-allow-multiple"
        type="checkbox"
        checked={booleanValue(config, 'allow_multiple', false)}
        onChange={(event) => onChange({ ...config, allow_multiple: event.target.checked })}
      />
      允许选择多位人员
    </label>
  )
}

export function FormulaConfigFields({
  config,
  onChange,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
}) {
  return (
    <>
      <label htmlFor="formula-expression">公式表达式</label>
      <textarea
        id="formula-expression"
        value={stringValue(config, 'expression', '')}
        placeholder="例如 SUM({金额})"
        onChange={(event) => onChange({ ...config, expression: event.target.value })}
      />
    </>
  )
}

export function RelationConfigFields({
  config,
  onChange,
  tables,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
  tables: ReadonlyArray<{ id: string; name: string }>
}) {
  return (
    <>
      <label htmlFor="relation-target-table">目标数据表</label>
      <select
        id="relation-target-table"
        value={stringValue(config, 'target_table_id', '')}
        onChange={(event) => onChange({ ...config, target_table_id: event.target.value })}
      >
        <option value="">请选择数据表</option>
        {tables.map((table) => (
          <option key={table.id} value={table.id}>
            {table.name}
          </option>
        ))}
      </select>
      <label className={styles.checkboxRow} htmlFor="relation-allow-multiple">
        <input
          id="relation-allow-multiple"
          type="checkbox"
          checked={booleanValue(config, 'allow_multiple', false)}
          onChange={(event) => onChange({ ...config, allow_multiple: event.target.checked })}
        />
        允许关联多条记录
      </label>
    </>
  )
}

export function LookupConfigFields({
  config,
  onChange,
  relationFields,
}: {
  config: ConfigValue
  onChange: (next: ConfigValue) => void
  relationFields: ReadonlyArray<FieldRecord>
}) {
  const relationFieldId = stringValue(config, 'relation_field_id', '')
  const relationField = relationFields.find((field) => field.id === relationFieldId)
  const targetTableId =
    relationField === undefined ? '' : stringValue(relationField.configMap, 'target_table_id', '')
  const targetFieldsQuery = useQuery({
    queryKey: fieldKeys.list(targetTableId),
    queryFn: () => fetchFields(targetTableId),
    enabled: targetTableId !== '',
    retry: false,
  })
  const targetFields = targetFieldsQuery.data ?? []

  return (
    <>
      <label htmlFor="lookup-relation-field">关联字段</label>
      <select
        id="lookup-relation-field"
        value={relationFieldId}
        onChange={(event) =>
          onChange({ ...config, relation_field_id: event.target.value, target_field_id: '' })
        }
      >
        <option value="">请选择关联字段</option>
        {relationFields.map((field) => (
          <option key={field.id} value={field.id}>
            {field.name}
          </option>
        ))}
      </select>
      <label htmlFor="lookup-target-field">目标字段</label>
      <select
        id="lookup-target-field"
        value={stringValue(config, 'target_field_id', '')}
        disabled={targetTableId === '' || targetFieldsQuery.isLoading}
        onChange={(event) => onChange({ ...config, target_field_id: event.target.value })}
      >
        <option value="">
          {targetTableId === ''
            ? '请先选择关联字段'
            : targetFieldsQuery.isLoading
              ? '正在加载目标表字段…'
              : '请选择目标字段'}
        </option>
        {targetFields.map((field) => (
          <option key={field.id} value={field.id}>
            {field.name}
          </option>
        ))}
      </select>
    </>
  )
}

/** 当前表中可作为引用来源的关联字段。 */
export function relationFieldsOf(fields: readonly FieldRecord[]): FieldRecord[] {
  return fields.filter((field) => field.type === 'relation')
}
