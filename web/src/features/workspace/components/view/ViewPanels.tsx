// 视图配置面板（票 07）：筛选 ≤5 / 排序 ≤3 / 单字段分组 / 字段显隐+冻结+行高。
// 所有改动通过 onChange(updater) 立即写回（useViewConfigAutoSave 负责串行提交 H-47）。
import { useState } from 'react'

import { parseDateInput } from '@/core/grid'
import { Button } from '@/ui-kit'

import type { FieldRecord } from '../../api/field'
import {
  filterOpNeedsList,
  filterOpNeedsRange,
  filterOpNeedsValue,
  FILTER_OP_LABELS,
  MAX_FILTERS,
  MAX_SORTS,
  operatorOptionsFor,
} from '../../types/view-config-validation'
import type {
  ViewConfig,
  ViewFilter,
  ViewFilterOp,
  ViewRowHeight,
} from '../../types/view-config'

import styles from './ViewPanels.module.css'

export interface ViewPanelProps {
  fields: FieldRecord[]
  config: ViewConfig
  onChange: (updater: (current: ViewConfig) => ViewConfig) => void
}

const GROUPABLE_TYPES = new Set<FieldRecord['type']>(['select', 'multiselect', 'person', 'date'])

function orderedFields(fields: readonly FieldRecord[]): FieldRecord[] {
  return [...fields].sort((left, right) => {
    if (left.isPrimary !== right.isPrimary) return left.isPrimary ? -1 : 1
    return left.position - right.position
  })
}

function fieldName(fields: readonly FieldRecord[], fieldId: string): string {
  return fields.find((field) => field.id === fieldId)?.name ?? fieldId
}

function optionList(field: FieldRecord | undefined): Array<{ id: string; name: string }> {
  const raw = field?.configMap.option_list
  if (!Array.isArray(raw)) return []
  return raw
    .map((entry) => {
      const record = entry as { id?: unknown; name?: unknown }
      return { id: String(record.id ?? ''), name: String(record.name ?? '') }
    })
    .filter((option) => option.id !== '')
}

function toDateInputValue(value: unknown): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return ''
  const date = new Date(value)
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

function fromDateInputValue(text: string): number | null {
  return text === '' ? null : parseDateInput(text, false)
}

function editableFields(fields: FieldRecord[]): FieldRecord[] {
  return fields.filter((field) => operatorOptionsFor(field.type).length > 0)
}

function FilterValueEditor({
  filter,
  index,
  field,
  update,
}: {
  filter: ViewFilter
  index: number
  field: FieldRecord | undefined
  update: (value: unknown) => void
}) {
  if (field === undefined || !filterOpNeedsValue(filter.op)) return null

  if (filterOpNeedsRange(filter.op)) {
    const bounds = Array.isArray(filter.value) ? filter.value : [null, null]
    const isDate = field.type === 'date'
    return (
      <span className={styles.rangeValues}>
        <input
          aria-label={`筛选值 ${index + 1}-1`}
          type={isDate ? 'date' : field.type === 'number' ? 'number' : 'text'}
          value={isDate ? toDateInputValue(bounds[0]) : bounds[0] === null ? '' : String(bounds[0])}
          onChange={(event) => {
            const raw = event.target.value
            const next = isDate
              ? fromDateInputValue(raw)
              : field.type === 'number'
                ? raw === ''
                  ? null
                  : Number(raw)
                : raw
            update([next, bounds[1] ?? null])
          }}
        />
        <span className={styles.rangeSeparator}>至</span>
        <input
          aria-label={`筛选值 ${index + 1}-2`}
          type={isDate ? 'date' : field.type === 'number' ? 'number' : 'text'}
          value={isDate ? toDateInputValue(bounds[1]) : bounds[1] === null ? '' : String(bounds[1])}
          onChange={(event) => {
            const raw = event.target.value
            const next = isDate
              ? fromDateInputValue(raw)
              : field.type === 'number'
                ? raw === ''
                  ? null
                  : Number(raw)
                : raw
            update([bounds[0] ?? null, next])
          }}
        />
      </span>
    )
  }

  if (filterOpNeedsList(filter.op)) {
    const options = optionList(field)
    if (options.length === 0) {
      return <span className={styles.hint}>暂无选项</span>
    }
    const selected = Array.isArray(filter.value) ? filter.value.map(String) : []
    return (
      <span className={styles.optionValues}>
        {options.map((option) => (
          <label key={option.id} className={styles.optionItem}>
            <input
              type="checkbox"
              aria-label={`筛选选项 ${index + 1}：${option.name}`}
              checked={selected.includes(option.id)}
              onChange={() => {
                update(
                  selected.includes(option.id)
                    ? selected.filter((id) => id !== option.id)
                    : [...selected, option.id],
                )
              }}
            />
            {option.name}
          </label>
        ))}
      </span>
    )
  }

  const isDate = field.type === 'date'
  return (
    <input
      aria-label={`筛选值 ${index + 1}`}
      type={isDate ? 'date' : field.type === 'number' ? 'number' : 'text'}
      value={
        isDate
          ? toDateInputValue(filter.value)
          : filter.value === null || filter.value === undefined
            ? ''
            : String(filter.value)
      }
      onChange={(event) => {
        const raw = event.target.value
        if (isDate) update(fromDateInputValue(raw))
        else if (field.type === 'number') update(raw === '' ? null : Number(raw))
        else update(raw)
      }}
    />
  )
}

/** 筛选面板：最多 5 条 AND；第 6 条被拒并提示上限；操作符按字段类型白名单。 */
export function FilterPanel({ fields, config, onChange }: ViewPanelProps) {
  const [limitError, setLimitError] = useState<string | null>(null)
  const ordered = orderedFields(fields)
  const filterFields = editableFields(ordered)

  function updateFilter(index: number, next: ViewFilter) {
    onChange((current) => ({
      ...current,
      filters: current.filters.map((filter, position) => (position === index ? next : filter)),
    }))
  }

  function updateField(index: number, fieldId: string) {
    const field = filterFields.find((candidate) => candidate.id === fieldId)
    const op = field === undefined ? 'contains' : operatorOptionsFor(field.type)[0] ?? 'contains'
    updateFilter(index, { fieldId, op, value: null })
  }

  function addFilter() {
    if (config.filters.length >= MAX_FILTERS) {
      setLimitError(`筛选条件最多 ${MAX_FILTERS} 条`)
      return
    }
    const field = filterFields[0]
    if (field === undefined) return
    setLimitError(null)
    onChange((current) => ({
      ...current,
      filters: [
        ...current.filters,
        {
          fieldId: field.id,
          op: operatorOptionsFor(field.type)[0] ?? 'contains',
          value: null,
        },
      ],
    }))
  }

  return (
    <section aria-label="筛选面板" className={styles.panel}>
      <div className={styles.panelHeader}>
        <h3 className={styles.panelTitle}>筛选</h3>
        <span className={styles.hint}>条件间为 AND，最多 {MAX_FILTERS} 条</span>
      </div>
      {config.filters.length === 0 ? <p className={styles.hint}>暂无筛选条件</p> : null}
      <div className={styles.rows}>
        {config.filters.map((filter, index) => {
          const field = fields.find((candidate) => candidate.id === filter.fieldId)
          const opOptions = field === undefined ? [] : operatorOptionsFor(field.type)
          return (
            <div key={`${filter.fieldId}-${index}`} className={styles.row}>
              <select
                aria-label={`筛选字段 ${index + 1}`}
                value={filter.fieldId}
                onChange={(event) => updateField(index, event.target.value)}
              >
                {filterFields.map((candidate) => (
                  <option key={candidate.id} value={candidate.id}>
                    {candidate.name}
                  </option>
                ))}
              </select>
              <select
                aria-label={`筛选操作 ${index + 1}`}
                value={filter.op}
                onChange={(event) =>
                  updateFilter(index, {
                    ...filter,
                    op: event.target.value as ViewFilterOp,
                    value: null,
                  })
                }
              >
                {opOptions.map((op) => (
                  <option key={op} value={op}>
                    {FILTER_OP_LABELS[op]}
                  </option>
                ))}
              </select>
              <FilterValueEditor
                filter={filter}
                index={index}
                field={field}
                update={(value) => updateFilter(index, { ...filter, value })}
              />
              <Button
                variant="secondary"
                aria-label={`删除筛选条件 ${index + 1}`}
                onClick={() =>
                  onChange((current) => ({
                    ...current,
                    filters: current.filters.filter((_, position) => position !== index),
                  }))
                }
              >
                删除
              </Button>
            </div>
          )
        })}
      </div>
      <div className={styles.panelActions}>
        <Button variant="secondary" onClick={addFilter} disabled={filterFields.length === 0}>
          添加筛选条件
        </Button>
        <Button
          variant="secondary"
          onClick={() => onChange((current) => ({ ...current, filters: [] }))}
          disabled={config.filters.length === 0}
        >
          清除全部
        </Button>
      </div>
      {limitError === null ? null : (
        <p role="alert" className={styles.error}>
          {limitError}
        </p>
      )}
      <p className={styles.hint}>当前 {config.filters.length} 条条件</p>
    </section>
  )
}

/** 排序面板：最多 3 级、每级字段 + 升/降序；空值由后端统一排最后。 */
export function SortPanel({ fields, config, onChange }: ViewPanelProps) {
  const [limitError, setLimitError] = useState<string | null>(null)
  const ordered = orderedFields(fields)

  function updateSort(index: number, fieldId: string, dir: 'asc' | 'desc') {
    onChange((current) => ({
      ...current,
      sorts: current.sorts.map((sort, position) =>
        position === index ? { fieldId, dir } : sort,
      ),
    }))
  }

  function addSort() {
    if (config.sorts.length >= MAX_SORTS) {
      setLimitError(`排序最多 ${MAX_SORTS} 级`)
      return
    }
    const field = ordered[0]
    if (field === undefined) return
    setLimitError(null)
    onChange((current) => ({
      ...current,
      sorts: [...current.sorts, { fieldId: field.id, dir: 'asc' }],
    }))
  }

  return (
    <section aria-label="排序面板" className={styles.panel}>
      <div className={styles.panelHeader}>
        <h3 className={styles.panelTitle}>排序</h3>
        <span className={styles.hint}>最多 {MAX_SORTS} 级，空值统一排最后</span>
      </div>
      {config.sorts.length === 0 ? <p className={styles.hint}>暂无排序规则</p> : null}
      <div className={styles.rows}>
        {config.sorts.map((sort, index) => (
          <div key={`${sort.fieldId}-${index}`} className={styles.row}>
            <select
              aria-label={`排序字段 ${index + 1}`}
              value={sort.fieldId}
              onChange={(event) => updateSort(index, event.target.value, sort.dir)}
            >
              {ordered.map((field) => (
                <option key={field.id} value={field.id}>
                  {field.name}
                </option>
              ))}
            </select>
            <select
              aria-label={`排序方向 ${index + 1}`}
              value={sort.dir}
              onChange={(event) =>
                updateSort(index, sort.fieldId, event.target.value === 'desc' ? 'desc' : 'asc')
              }
            >
              <option value="asc">升序</option>
              <option value="desc">降序</option>
            </select>
            <Button
              variant="secondary"
              aria-label={`删除排序 ${index + 1}`}
              onClick={() =>
                onChange((current) => ({
                  ...current,
                  sorts: current.sorts.filter((_, position) => position !== index),
                }))
              }
            >
              删除
            </Button>
          </div>
        ))}
      </div>
      <div className={styles.panelActions}>
        <Button variant="secondary" onClick={addSort} disabled={ordered.length === 0}>
          添加排序
        </Button>
        <Button
          variant="secondary"
          onClick={() => onChange((current) => ({ ...current, sorts: [] }))}
          disabled={config.sorts.length === 0}
        >
          清除全部
        </Button>
      </div>
      {limitError === null ? null : (
        <p role="alert" className={styles.error}>
          {limitError}
        </p>
      )}
      <p className={styles.hint}>当前 {config.sorts.length} 级排序</p>
    </section>
  )
}

/** 分组面板：仅 1 个字段（单选/多选/人员/日期按日/周/月）。 */
export function GroupPanel({ fields, config, onChange }: ViewPanelProps) {
  const ordered = orderedFields(fields)
  const groupable = ordered.filter((field) => GROUPABLE_TYPES.has(field.type))
  const groupField =
    config.group === null
      ? undefined
      : ordered.find((field) => field.id === config.group?.fieldId)

  function changeGroup(fieldId: string) {
    if (fieldId === '') {
      onChange((current) => ({ ...current, group: null }))
      return
    }
    const field = groupable.find((candidate) => candidate.id === fieldId)
    onChange((current) => ({
      ...current,
      group: {
        fieldId,
        ...(field?.type === 'date'
          ? { granularity: current.group?.granularity ?? 'day' }
          : {}),
      },
    }))
  }

  return (
    <section aria-label="分组面板" className={styles.panel}>
      <div className={styles.panelHeader}>
        <h3 className={styles.panelTitle}>分组</h3>
        <span className={styles.hint}>仅支持 1 个字段，组头不显示全量计数</span>
      </div>
      <div className={styles.row}>
        <select
          aria-label="分组字段"
          value={config.group?.fieldId ?? ''}
          onChange={(event) => changeGroup(event.target.value)}
        >
          <option value="">不分组</option>
          {groupable.map((field) => (
            <option key={field.id} value={field.id}>
              {field.name}
            </option>
          ))}
        </select>
        {groupField?.type === 'date' ? (
          <select
            aria-label="日期粒度"
            value={config.group?.granularity ?? 'day'}
            onChange={(event) =>
              onChange((current) => ({
                ...current,
                group: {
                  fieldId: config.group?.fieldId ?? '',
                  granularity: event.target.value as 'day' | 'week' | 'month',
                },
              }))
            }
          >
            <option value="day">按日</option>
            <option value="week">按周</option>
            <option value="month">按月</option>
          </select>
        ) : null}
      </div>
      {config.group === null ? null : (
        <p className={styles.hint}>
          按「{fieldName(ordered, config.group.fieldId)}」分组
        </p>
      )}
    </section>
  )
}

const ROW_HEIGHT_LABELS: Record<ViewRowHeight, string> = {
  compact: '紧凑',
  medium: '中等',
  tall: '宽松',
}

/** 字段显隐面板：隐藏非主字段、恢复全部、冻结到第 N 列、三档行高。 */
export function FieldVisibilityPanel({ fields, config, onChange }: ViewPanelProps) {
  const ordered = orderedFields(fields)
  const hidden = new Set(config.hiddenFieldIds)
  let frozenCount = 0
  for (const field of ordered) {
    if (!config.frozenFieldIds.includes(field.id)) break
    frozenCount += 1
  }

  return (
    <section aria-label="字段显隐面板" className={styles.panel}>
      <div className={styles.panelHeader}>
        <h3 className={styles.panelTitle}>字段显隐</h3>
        <Button
          variant="secondary"
          onClick={() => onChange((current) => ({ ...current, hiddenFieldIds: [] }))}
          disabled={config.hiddenFieldIds.length === 0}
        >
          恢复全部显示
        </Button>
      </div>
      <div className={styles.fieldList}>
        {ordered.map((field) => (
          <label key={field.id} className={styles.fieldItem}>
            <input
              type="checkbox"
              aria-label={`显示字段：${field.name}`}
              checked={field.isPrimary || !hidden.has(field.id)}
              disabled={field.isPrimary}
              onChange={() =>
                onChange((current) => ({
                  ...current,
                  hiddenFieldIds: current.hiddenFieldIds.includes(field.id)
                    ? current.hiddenFieldIds.filter((id) => id !== field.id)
                    : [...current.hiddenFieldIds, field.id],
                }))
              }
            />
            {field.name}
            {field.isPrimary ? <span className={styles.hint}>（主字段不可隐藏）</span> : null}
          </label>
        ))}
      </div>
      <div className={styles.row}>
        <label className={styles.inlineLabel}>
          冻结到第
          <select
            aria-label="冻结到第 N 列"
            value={frozenCount}
            onChange={(event) => {
              const count = Number(event.target.value)
              onChange((current) => ({
                ...current,
                frozenFieldIds: ordered.slice(0, count).map((field) => field.id),
              }))
            }}
          >
            {Array.from({ length: ordered.length + 1 }, (_, index) => (
              <option key={index} value={index}>
                {index === 0 ? '不冻结' : index}
              </option>
            ))}
          </select>
          列
        </label>
      </div>
      <div role="group" aria-label="行高" className={styles.row}>
        {(Object.keys(ROW_HEIGHT_LABELS) as ViewRowHeight[]).map((tier) => (
          <Button
            key={tier}
            variant="secondary"
            aria-pressed={config.rowHeight === tier}
            onClick={() => onChange((current) => ({ ...current, rowHeight: tier }))}
          >
            {ROW_HEIGHT_LABELS[tier]}
          </Button>
        ))}
      </div>
    </section>
  )
}
