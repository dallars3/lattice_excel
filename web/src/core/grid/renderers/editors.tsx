// 单元格编辑器（票 06）：text / number / select / multiselect / date / person / url / relation 八类。
// 统一契约（技术方案 6.4）：三键经 resolveEditorKey（Enter 提交下移、Tab 提交右移、Shift+Tab 左移、
// Esc 取消不落盘）；validate 失败行内报错且不提交、内容不丢失。公式/引用只读、附件网格内只读。
// 跨域依赖（H-40 增选项、H-63 协作者、关联目标标签）由网格层注入 config.createOption/users/labels，
// 本文件保持 DOM 与领域无关（不直接发请求）。
import { useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

import {
  defineEditor,
  resolveEditorKey,
  type CellEditorDef,
  type CellEditorLifecycle,
  type CellEditorProps,
  type CellRegistry,
  type EditorCommitMove,
} from '../registry'
import { formatDate, type DateCellConfig } from './format-date'
import { type NumberCellConfig } from './format-number'
import {
  MIN_DATE_MS,
  matchOptionId,
  normalizeUrlInput,
  parseDateInput,
  parseNumberDraft,
} from './editor-values'
import { parseUrlValue } from './url'
import { resolveSelectOptions, type SelectCellConfig } from './select'
import { TEXT_EDITOR } from './text'

import styles from './editors.module.css'

/** 网格层注入的选项新建回调（H-40）：返回新建 option_id；失败抛错由编辑器行内展示。 */
export type CreateOptionHandler = (name: string) => Promise<string | null>

interface OptionEditorConfig extends SelectCellConfig {
  readonly createOption?: CreateOptionHandler
  readonly allow_create_option?: boolean
}

interface PersonEditorConfig {
  readonly allow_multiple?: boolean
  readonly users?: Readonly<Record<string, { readonly name: string; readonly disabled?: boolean }>>
}

interface RelationEditorConfig {
  readonly allow_multiple?: boolean
  readonly labels?: Readonly<Record<string, string>>
}

const ERROR_ID = 'grid-cell-editor-error'

/** 行内错误提示。 */
function EditorError({ error }: { readonly error: string | null }) {
  if (error === null) return null
  return (
    <span id={ERROR_ID} role="alert" className={styles.error}>
      {error}
    </span>
  )
}

function asConfig<T>(config: unknown): T {
  return (config ?? {}) as T
}

/** 统一三键分发：返回 true = 已消费；Esc 走 reject，Enter/Tab 调 run(move)。 */
function consumeEditorKey(
  event: ReactKeyboardEvent<HTMLElement>,
  enterMove: EditorCommitMove | undefined,
  onReject: () => void,
  run: (move: EditorCommitMove) => void,
): boolean {
  const action = resolveEditorKey(event.key, {
    enterMove,
    shiftKey: event.shiftKey,
  })
  if (action.kind === 'pass') return false
  event.preventDefault()
  event.stopPropagation()
  if (action.kind === 'reject') {
    onReject()
    return true
  }
  run(action.move)
  return true
}

/** 计算小数位（与 format-number 同口径）。 */
function resolveDecimals(config: NumberCellConfig): number {
  if (config.format === 'int') return 0
  const raw = config.decimals ?? config.precision ?? (config.format === undefined ? 0 : 2)
  const truncated = Math.trunc(raw)
  if (!Number.isFinite(truncated)) return 0
  return Math.min(Math.max(0, truncated), 4)
}

// ---------------------------------------------------------------------------------------------
// text（复用 M1 统一生命周期实现）
// ---------------------------------------------------------------------------------------------

export const TEXT_EDITOR_M2 = TEXT_EDITOR

// ---------------------------------------------------------------------------------------------
// number
// ---------------------------------------------------------------------------------------------

const NUMBER_LIFECYCLE: CellEditorLifecycle<string, string, NumberCellConfig, number | null> = {
  begin: ({ value }) => (value === null || value === undefined ? '' : String(value)),
  input: ({ input }) => input,
  validate: ({ draft, config }) =>
    parseNumberDraft(draft, resolveDecimals(config)) === undefined ? '请输入有效数字' : null,
  commit: ({ draft, config }) => parseNumberDraft(draft, resolveDecimals(config)) ?? null,
}

function NumberCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const numberConfig = asConfig<NumberCellConfig>(config)
  const decimals = resolveDecimals(numberConfig)
  const [draft, setDraft] = useState(() =>
    initialInput !== undefined
      ? initialInput
      : value === null || value === undefined
        ? ''
        : String(value),
  )
  const [error, setError] = useState<string | null>(null)

  const handleInput = (raw: string) => {
    // 非法字符不可录（仅保留数字、负号、小数点）
    const next = raw.replace(/[^0-9.-]/g, '')
    setDraft(next)
    const parsed = parseNumberDraft(next, decimals)
    if (parsed !== undefined) onDraftChange?.(parsed)
    if (error !== null && parsed !== undefined) setError(null)
  }

  function commit(move: EditorCommitMove) {
    const parsed = parseNumberDraft(draft, decimals)
    if (parsed === undefined) {
      setError('请输入有效数字')
      return
    }
    onCommit(parsed, move)
  }

  return (
    <span className={styles.editor}>
      <input
        type="text"
        inputMode="decimal"
        className={styles.input}
        aria-label="数字编辑"
        value={draft}
        aria-invalid={error !== null}
        aria-describedby={error !== null ? ERROR_ID : undefined}
        onChange={(event) => handleInput(event.target.value)}
        onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
      />
      <EditorError error={error} />
    </span>
  )
}

export const NUMBER_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: NUMBER_LIFECYCLE,
  component: NumberCellEditor,
})

// ---------------------------------------------------------------------------------------------
// select / multiselect
// ---------------------------------------------------------------------------------------------

function optionCreateEnabled(config: OptionEditorConfig): boolean {
  return config.allow_create === true || config.allow_create_option === true
}

function SelectCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const optionConfig = asConfig<OptionEditorConfig>(config)
  const options = resolveSelectOptions(optionConfig)
  const allowCreate = optionCreateEnabled(optionConfig)
  const [search, setSearch] = useState(() =>
    initialInput !== undefined ? initialInput : value === null || value === undefined ? '' : String(value),
  )
  const [selectedId, setSelectedId] = useState<string | null>(() =>
    typeof value === 'string' && value !== '' ? value : null,
  )
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const needle = search.trim().toLocaleLowerCase()
  const filtered =
    needle === ''
      ? options
      : options.filter(
          (option) =>
            option.label.toLocaleLowerCase().includes(needle) ||
            option.id.toLocaleLowerCase().includes(needle),
        )
  const noMatch = needle !== '' && filtered.length === 0

  function setSelection(id: string | null) {
    setSelectedId(id)
    setError(null)
    onDraftChange?.(id)
  }

  async function commitSelection(move: EditorCommitMove) {
    const trimmed = search.trim()
    const exact = matchOptionId(options, trimmed)
    if (exact !== null) {
      setSelection(exact)
      onCommit(exact, move)
      return
    }
    if (trimmed === '' && selectedId !== null) {
      onCommit(selectedId, move)
      return
    }
    if (trimmed !== '' && allowCreate && optionConfig.createOption !== undefined) {
      setCreating(true)
      try {
        const createdId = await optionConfig.createOption(trimmed)
        if (createdId !== null) {
          setSelection(createdId)
          onCommit(createdId, move)
          return
        }
      } catch (failure) {
        setError(failure instanceof Error ? failure.message : '新建选项失败')
        return
      } finally {
        setCreating(false)
      }
    }
    setError(
      trimmed === ''
        ? '请选择选项'
        : allowCreate
          ? '新建选项失败'
          : '无匹配选项，请联系表格管理员添加',
    )
  }

  return (
    <span className={styles.editor}>
      <input
        type="text"
        role="combobox"
        aria-label="搜索选项"
        aria-expanded="true"
        aria-invalid={error !== null}
        className={styles.input}
        value={search}
        autoFocus
        disabled={creating}
        onChange={(event) => {
          setSearch(event.target.value)
          setSelectedId(null)
        }}
        onKeyDown={(event) =>
          consumeEditorKey(event, enterMove, onReject, (move) => {
            void commitSelection(move)
          })
        }
      />
      <EditorError error={error} />
      <span className={styles.panel} role="listbox" aria-label="选项列表">
        {filtered.map((option) => (
          <button
            key={option.id}
            type="button"
            role="option"
            aria-selected={selectedId === option.id}
            data-selected={selectedId === option.id ? 'true' : undefined}
            className={styles.option}
            onClick={() => setSelection(option.id)}
          >
            {option.label}
          </button>
        ))}
        {noMatch ? (
          <span className={styles.panelError}>
            {allowCreate ? '按 Enter 新建并选中该选项' : '无匹配选项，请联系表格管理员添加'}
          </span>
        ) : null}
      </span>
    </span>
  )
}

const SELECT_LIFECYCLE: CellEditorLifecycle<string, string, SelectCellConfig, string | null> = {
  begin: ({ value }) => (value === null || value === undefined ? '' : String(value)),
  input: ({ input }) => input,
  validate: () => null,
  commit: ({ draft }) => (draft === '' ? null : draft),
}

export const SELECT_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: SELECT_LIFECYCLE,
  component: SelectCellEditor,
})

function MultiselectCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const optionConfig = asConfig<OptionEditorConfig>(config)
  const options = resolveSelectOptions(optionConfig)
  const allowCreate = optionCreateEnabled(optionConfig)
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  function toggle(id: string) {
    const next = selected.includes(id)
      ? selected.filter((candidate) => candidate !== id)
      : [...selected, id]
    setSelected(next)
    setError(null)
    onDraftChange?.(next.length === 0 ? null : next)
  }

  async function addFromSearch() {
    const trimmed = search.trim()
    if (trimmed === '') return
    const matched = matchOptionId(options, trimmed)
    if (matched !== null) {
      if (!selected.includes(matched)) toggle(matched)
      setSearch('')
      return
    }
    if (allowCreate && optionConfig.createOption !== undefined) {
      setCreating(true)
      try {
        const created = await optionConfig.createOption(trimmed)
        if (created !== null) {
          setSelected((current) => [...current, created])
          onDraftChange?.(created)
          setSearch('')
        }
      } catch (failure) {
        setError(failure instanceof Error ? failure.message : '新建选项失败')
      } finally {
        setCreating(false)
      }
      return
    }
    setError('无匹配选项，请联系表格管理员添加')
  }

  const needle = search.trim().toLocaleLowerCase()
  const filtered =
    needle === ''
      ? options
      : options.filter((option) => option.label.toLocaleLowerCase().includes(needle))

  function commit(move: EditorCommitMove) {
    onCommit(selected.length === 0 ? null : selected, move)
  }

  return (
    <span className={styles.editor}>
      <span className={styles.tagList}>
        {selected.map((id) => {
          const option = options.find((candidate) => candidate.id === id)
          return (
            <span key={id} className={styles.tag}>
              {option?.label ?? id}
              <button
                type="button"
                aria-label={`移除选项：${option?.label ?? id}`}
                onClick={() => toggle(id)}
              >
                ×
              </button>
            </span>
          )
        })}
      </span>
      <input
        type="text"
        aria-label="搜索选项"
        className={styles.input}
        value={search}
        autoFocus
        disabled={creating}
        onChange={(event) => setSearch(event.target.value)}
        onKeyDown={(event) => {
          const handled = consumeEditorKey(event, enterMove, onReject, commit)
          if (handled && search.trim() !== '') void addFromSearch()
        }}
      />
      <EditorError error={error} />
      <span className={styles.panel} role="listbox" aria-label="选项列表">
        {filtered.map((option) => (
          <button
            key={option.id}
            type="button"
            role="option"
            aria-selected={selected.includes(option.id)}
            className={styles.option}
            onClick={() => toggle(option.id)}
          >
            {selected.includes(option.id) ? '☑ ' : '☐ '}
            {option.label}
          </button>
        ))}
        {needle !== '' && filtered.length === 0 ? (
          <span className={styles.panelError}>
            {allowCreate ? '按 Enter 新建并选中该选项' : '无匹配选项，请联系表格管理员添加'}
          </span>
        ) : null}
      </span>
    </span>
  )
}

export const MULTISELECT_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: SELECT_LIFECYCLE,
  component: MultiselectCellEditor,
})

// ---------------------------------------------------------------------------------------------
// date
// ---------------------------------------------------------------------------------------------

interface DateEditorConfig extends DateCellConfig {
  readonly format?: string
}

function DateCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const dateConfig = asConfig<DateEditorConfig>(config)
  const includeTime = dateConfig.include_time === true
  const initialTimestamp = typeof value === 'number' ? value : null
  const [draft, setDraft] = useState(() =>
    initialInput !== undefined ? initialInput : formatDate(value, dateConfig),
  )
  const [error, setError] = useState<string | null>(null)
  const [monthCursor, setMonthCursor] = useState(() => {
    const base =
      initialTimestamp === null || initialTimestamp < MIN_DATE_MS
        ? new Date()
        : new Date(initialTimestamp)
    return { year: base.getFullYear(), month: base.getMonth() }
  })

  function applyDraft(next: string) {
    setDraft(next)
    const parsed = parseDateInput(next, includeTime)
    if (parsed !== null) {
      onDraftChange?.(parsed)
      setError(null)
    }
  }

  function commit(move: EditorCommitMove) {
    if (draft.trim() === '') {
      onCommit(null, move)
      return
    }
    const parsed = parseDateInput(draft, includeTime)
    if (parsed === null) {
      setError('请输入有效日期（如 2024-01-05，且不早于 1900-01-01）')
      return
    }
    onCommit(parsed, move)
  }

  const first = new Date(monthCursor.year, monthCursor.month, 1)
  const offset = first.getDay()
  const cells = Array.from({ length: 42 }, (_, index) => {
    const date = new Date(monthCursor.year, monthCursor.month, 1 + index - offset)
    return { date, ms: date.getTime() }
  })

  function pickDay(ms: number) {
    const parsedCurrent = parseDateInput(draft, includeTime)
    const base = parsedCurrent === null ? new Date() : new Date(parsedCurrent)
    const picked = new Date(ms)
    picked.setHours(
      includeTime ? base.getHours() : 0,
      includeTime ? base.getMinutes() : 0,
      0,
      0,
    )
    const timestamp = picked.getTime()
    setDraft(formatDate(timestamp, dateConfig))
    onDraftChange?.(timestamp)
    setError(null)
    onCommit(timestamp, 'none')
  }

  return (
    <span className={styles.editor}>
      <span className={styles.editorRow}>
        <input
          type="text"
          aria-label="日期编辑"
          className={styles.input}
          value={draft}
          autoFocus
          aria-invalid={error !== null}
          onChange={(event) => applyDraft(event.target.value)}
          onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
        />
        {includeTime ? (
          <input
            type="text"
            aria-label="时间（HH:mm）"
            className={styles.input}
            placeholder="HH:mm"
            onChange={(event) => {
              const datePart = draft.split(/[ T]/)[0] ?? ''
              applyDraft(`${datePart} ${event.target.value}`)
            }}
          />
        ) : null}
      </span>
      <EditorError error={error} />
      <div className={styles.calendar} role="dialog" aria-label="日期选择器">
        <div className={styles.calendarHeader}>
          <button
            type="button"
            className={styles.smallButton}
            aria-label="上个月"
            onClick={() =>
              setMonthCursor((current) => {
                const prev = new Date(current.year, current.month - 1, 1)
                return { year: prev.getFullYear(), month: prev.getMonth() }
              })
            }
          >
            ‹
          </button>
          <span>
            {monthCursor.year} 年 {monthCursor.month + 1} 月
          </span>
          <button
            type="button"
            className={styles.smallButton}
            aria-label="下个月"
            onClick={() =>
              setMonthCursor((current) => {
                const next = new Date(current.year, current.month + 1, 1)
                return { year: next.getFullYear(), month: next.getMonth() }
              })
            }
          >
            ›
          </button>
        </div>
        <div className={styles.calendarGrid}>
          {['日', '一', '二', '三', '四', '五', '六'].map((weekday) => (
            <span key={weekday} className={styles.hint}>
              {weekday}
            </span>
          ))}
          {cells.map((cell) => {
            const disabled = cell.ms < MIN_DATE_MS
            const selected =
              initialTimestamp !== null &&
              new Date(initialTimestamp).getFullYear() === cell.date.getFullYear() &&
              new Date(initialTimestamp).getMonth() === cell.date.getMonth() &&
              new Date(initialTimestamp).getDate() === cell.date.getDate()
            return (
              <button
                key={cell.ms}
                type="button"
                className={styles.calendarCell}
                data-selected={selected ? 'true' : undefined}
                disabled={disabled}
                aria-label={formatDate(cell.ms, { format: 'YYYY-MM-DD' })}
                onClick={() => pickDay(cell.ms)}
              >
                {cell.date.getDate()}
              </button>
            )
          })}
        </div>
      </div>
    </span>
  )
}

const DATE_LIFECYCLE: CellEditorLifecycle<string, string, DateEditorConfig, number | null> = {
  begin: ({ value, config }) => formatDate(value, config),
  input: ({ input }) => input,
  validate: ({ draft, config }) =>
    draft.trim() === '' || parseDateInput(draft, config.include_time === true) !== null
      ? null
      : '请输入有效日期（如 2024-01-05，且不早于 1900-01-01）',
  commit: ({ draft, config }) =>
    draft.trim() === '' ? null : parseDateInput(draft, config.include_time === true),
}

export const DATE_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: DATE_LIFECYCLE,
  component: DateCellEditor,
})

// ---------------------------------------------------------------------------------------------
// person
// ---------------------------------------------------------------------------------------------

function PersonCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const personConfig = asConfig<PersonEditorConfig>(config)
  const allowMultiple = personConfig.allow_multiple === true
  const users = personConfig.users ?? {}
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })

  const needle = search.trim().toLocaleLowerCase()
  const candidates = Object.entries(users).filter(
    ([userId, info]) =>
      needle === '' ||
      info.name.toLocaleLowerCase().includes(needle) ||
      userId.toLocaleLowerCase().includes(needle),
  )

  function toggle(userId: string) {
    const next = allowMultiple
      ? selected.includes(userId)
        ? selected.filter((candidate) => candidate !== userId)
        : [...selected, userId]
      : selected.includes(userId)
        ? []
        : [userId]
    setSelected(next)
    onDraftChange?.(
      allowMultiple ? (next.length === 0 ? null : next) : (next[0] ?? null),
    )
  }

  function commit(move: EditorCommitMove) {
    onCommit(
      allowMultiple ? (selected.length === 0 ? null : selected) : (selected[0] ?? null),
      move,
    )
  }

  return (
    <span className={styles.editor}>
      {selected.length === 0 ? null : (
        <span className={styles.tagList}>
          {selected.map((userId) => (
            <span key={userId} className={styles.tag}>
              {users[userId]?.name ?? userId}
              <button
                type="button"
                aria-label={`移除人员：${users[userId]?.name ?? userId}`}
                onClick={() => toggle(userId)}
              >
                ×
              </button>
            </span>
          ))}
        </span>
      )}
      <input
        type="text"
        aria-label="搜索人员"
        className={styles.input}
        value={search}
        autoFocus
        onChange={(event) => setSearch(event.target.value)}
        onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
      />
      <span className={styles.panel} role="listbox" aria-label="人员候选">
        {candidates.map(([userId, info]) => (
          <button
            key={userId}
            type="button"
            role="option"
            aria-selected={selected.includes(userId)}
            className={styles.option}
            onClick={() => toggle(userId)}
          >
            {selected.includes(userId) ? '☑ ' : '☐ '}
            {info.name}
            {info.disabled === true ? '（已停用成员）' : ''}
          </button>
        ))}
      </span>
    </span>
  )
}

const PERSON_LIFECYCLE: CellEditorLifecycle<
  string,
  string,
  PersonEditorConfig,
  string | string[] | null
> = {
  begin: ({ value }) =>
    Array.isArray(value)
      ? value.join(',')
      : value === null || value === undefined
        ? ''
        : String(value),
  input: ({ input }) => input,
  validate: () => null,
  commit: ({ draft }) =>
    draft.trim() === '' ? null : draft.split(',').map((item) => item.trim()),
}

export const PERSON_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: PERSON_LIFECYCLE,
  component: PersonCellEditor,
})

// ---------------------------------------------------------------------------------------------
// url
// ---------------------------------------------------------------------------------------------

function UrlCellEditor({
  value,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const parsed = parseUrlValue(value)
  const [url, setUrl] = useState(() =>
    initialInput !== undefined ? initialInput : parsed?.url ?? (typeof value === 'string' ? value : ''),
  )
  const [displayText, setDisplayText] = useState(parsed?.displayText ?? '')
  const [error, setError] = useState<string | null>(null)

  function commit(move: EditorCommitMove) {
    if (url.trim() === '') {
      onCommit(null, move)
      return
    }
    const normalized = normalizeUrlInput(url, displayText)
    if (normalized === null) {
      setError('请输入合法的 http/https 链接')
      return
    }
    onCommit({ url: normalized.url, display_text: normalized.displayText }, move)
  }

  return (
    <span className={styles.editor}>
      <span className={styles.editorRow}>
        <input
          type="text"
          aria-label="链接地址"
          className={styles.input}
          value={url}
          autoFocus
          aria-invalid={error !== null}
          onChange={(event) => {
            setUrl(event.target.value)
            const normalized = normalizeUrlInput(event.target.value, displayText)
            if (normalized !== null) onDraftChange?.(normalized)
            if (error !== null) setError(null)
          }}
          onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
        />
        <input
          type="text"
          aria-label="显示文本"
          className={styles.input}
          value={displayText}
          onChange={(event) => setDisplayText(event.target.value)}
          onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
        />
      </span>
      <EditorError error={error} />
    </span>
  )
}

const URL_LIFECYCLE: CellEditorLifecycle<string, string, unknown, unknown> = {
  begin: ({ value }) => {
    const parsed = parseUrlValue(value)
    return parsed?.url ?? (typeof value === 'string' ? value : '')
  },
  input: ({ input }) => input,
  validate: ({ draft }) =>
    draft.trim() === '' || normalizeUrlInput(draft, '') !== null
      ? null
      : '请输入合法的 http/https 链接',
  commit: ({ draft }) => (draft.trim() === '' ? null : normalizeUrlInput(draft, '')),
}

export const URL_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: URL_LIFECYCLE,
  component: UrlCellEditor,
})

// ---------------------------------------------------------------------------------------------
// relation
// ---------------------------------------------------------------------------------------------

function RelationCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const relationConfig = asConfig<RelationEditorConfig>(config)
  const allowMultiple = relationConfig.allow_multiple === true
  const labels = relationConfig.labels ?? {}
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })

  const needle = search.trim().toLocaleLowerCase()
  const candidates = Object.entries(labels).filter(
    ([recordId, label]) =>
      needle === '' ||
      label.toLocaleLowerCase().includes(needle) ||
      recordId.toLocaleLowerCase().includes(needle),
  )

  function toggle(recordId: string) {
    const next = allowMultiple
      ? selected.includes(recordId)
        ? selected.filter((candidate) => candidate !== recordId)
        : [...selected, recordId]
      : selected.includes(recordId)
        ? []
        : [recordId]
    setSelected(next)
    onDraftChange?.(
      allowMultiple ? (next.length === 0 ? null : next) : (next[0] ?? null),
    )
  }

  function commit(move: EditorCommitMove) {
    onCommit(
      allowMultiple ? (selected.length === 0 ? null : selected) : (selected[0] ?? null),
      move,
    )
  }

  return (
    <span className={styles.editor}>
      {selected.length === 0 ? null : (
        <span className={styles.tagList}>
          {selected.map((recordId) => (
            <span key={recordId} className={styles.tag}>
              {labels[recordId] ?? recordId}
              <button
                type="button"
                aria-label={`移除关联：${labels[recordId] ?? recordId}`}
                onClick={() => toggle(recordId)}
              >
                ×
              </button>
            </span>
          ))}
        </span>
      )}
      <input
        type="text"
        aria-label="搜索关联记录"
        className={styles.input}
        value={search}
        autoFocus
        onChange={(event) => setSearch(event.target.value)}
        onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
      />
      <span className={styles.panel} role="listbox" aria-label="关联记录候选">
        {candidates.map(([recordId, label]) => (
          <button
            key={recordId}
            type="button"
            role="option"
            aria-selected={selected.includes(recordId)}
            className={styles.option}
            onClick={() => toggle(recordId)}
          >
            {selected.includes(recordId) ? '☑ ' : '☐ '}
            {label}
          </button>
        ))}
      </span>
    </span>
  )
}

const RELATION_LIFECYCLE: CellEditorLifecycle<
  string,
  string,
  RelationEditorConfig,
  string | string[] | null
> = {
  begin: ({ value }) =>
    Array.isArray(value)
      ? value.join(',')
      : value === null || value === undefined
        ? ''
        : String(value),
  input: ({ input }) => input,
  validate: () => null,
  commit: ({ draft }) =>
    draft.trim() === '' ? null : draft.split(',').map((item) => item.trim()),
}

export const RELATION_EDITOR = defineEditor<unknown, unknown, string, string>({
  lifecycle: RELATION_LIFECYCLE,
  component: RelationCellEditor,
})

// ---------------------------------------------------------------------------------------------
// 注册
// ---------------------------------------------------------------------------------------------

/** M2 编辑器清单（键为字段类型小写枚举名）；formula/lookup/attachment 无编辑器（只读）。 */
export const M2_EDITORS: Readonly<Record<string, CellEditorDef>> = {
  text: TEXT_EDITOR_M2,
  number: NUMBER_EDITOR,
  select: SELECT_EDITOR,
  multiselect: MULTISELECT_EDITOR,
  date: DATE_EDITOR,
  person: PERSON_EDITOR,
  url: URL_EDITOR,
  relation: RELATION_EDITOR,
}

/** 把 8 类 M2 编辑器注册进注册表（不改注册表对外语义）。 */
export function registerM2Editors(registry: CellRegistry): void {
  for (const [fieldType, def] of Object.entries(M2_EDITORS)) {
    registry.registerEditor(fieldType, def)
  }
}
