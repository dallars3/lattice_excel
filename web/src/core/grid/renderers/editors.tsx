// 单元格编辑器（票 06）：text / number / select / multiselect / date / person / url / relation 八类。
// 统一契约（技术方案 6.4）：三键经 resolveEditorKey（Enter 提交下移、Tab 提交右移、Shift+Tab 左移、
// Esc 取消不落盘）；validate 失败行内报错且不提交、内容不丢失。公式/引用只读、附件网格内只读。
// 跨域依赖（H-40 增选项、H-63 协作者、关联目标标签）由网格层注入 config.createOption/users/labels，
// 本文件保持 DOM 与领域无关（不直接发请求）。
import { useId, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react'

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
import { EditorPopup } from './editor-popup'

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

/**
 * 行内错误提示（票 01 portal 化）：渲染到 document.body 脱离单元格 overflow 裁剪，
 * 以编辑器宿主为锚；锚未就绪（宿主 ref 未置入的首帧）退回内联渲染。
 */
function EditorError({
  error,
  anchor,
}: {
  readonly error: string | null
  readonly anchor: HTMLElement | null
}) {
  if (error === null) return null
  if (anchor === null) {
    return (
      <span id={ERROR_ID} role="alert" className={styles.error}>
        {error}
      </span>
    )
  }
  return (
    <EditorPopup id={ERROR_ID} role="alert" className={styles.error} anchor={anchor}>
      {error}
    </EditorPopup>
  )
}

/**
 * 编辑器宿主锚（票 01）：弹层以宿主 span 为锚定位。
 * 回调 ref 入 state：宿主节点挂载后触发一次重渲染，弹层 portal 随之挂载。
 */
function useEditorHostAnchor(): [HTMLElement | null, (node: HTMLElement | null) => void] {
  const [host, setHost] = useState<HTMLElement | null>(null)
  return [host, setHost]
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

// ---------------------------------------------------------------------------------------------
// 下拉编辑器公共提交链（票 02 / BUG-01+BUG-04 网格部分）：四类下拉（select/multiselect/
// person/relation）统一落地——点击选中语义、方向键高亮导航、pointerdown 不夺焦。
// ---------------------------------------------------------------------------------------------

/** 选项元素 id（aria-activedescendant 指向目标）＝ listbox id + 选项 id，跨编辑器实例不冲突。 */
function optionElementId(listboxId: string, optionId: string): string {
  return `${listboxId}-${optionId}`
}

/**
 * 下拉方向键高亮导航（票 02）：ArrowDown/ArrowUp 在候选间循环移动高亮，返回移动后的索引；
 * 无候选或非方向键返回 null（未消费，交还三键解析与输入框原生行为）。
 */
function moveCandidateHighlight(
  event: ReactKeyboardEvent<HTMLElement>,
  current: number,
  count: number,
): number | null {
  if (count === 0) return null
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    return current < 0 || current >= count - 1 ? 0 : current + 1
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault()
    return current < 0 || current <= 0 ? count - 1 : current - 1
  }
  return null
}

/**
 * 选项按钮按下不夺走输入框焦点（票 02）：preventDefault 阻止浏览器把焦点移到按钮，
 * 输入框保持焦点可继续搜索输入，也一并规避「mousedown 抢焦 → 失焦提交」竞态。
 */
function preventOptionPointerDown(event: ReactPointerEvent<HTMLElement>): void {
  event.preventDefault()
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
  const [host, setHost] = useEditorHostAnchor()
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
    <span className={styles.editor} ref={setHost}>
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
      <EditorError error={error} anchor={host} />
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
  // 未注入 createOption（如 editor 无 H-40 权限）时按「不可新建」处理，提示联系管理员。
  const allowCreate =
    optionCreateEnabled(optionConfig) && optionConfig.createOption !== undefined
  const [host, setHost] = useEditorHostAnchor()
  const [search, setSearch] = useState(() =>
    initialInput !== undefined ? initialInput : value === null || value === undefined ? '' : String(value),
  )
  const [selectedId, setSelectedId] = useState<string | null>(() =>
    typeof value === 'string' && value !== '' ? value : null,
  )
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  // 票 02：方向键高亮导航（aria-activedescendant）；-1 = 未高亮，回车提交高亮项。
  const listboxId = useId()
  const [highlight, setHighlight] = useState(-1)

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

  // aria-activedescendant 指向当前高亮候选（搜索词变化会重置高亮，见 setSearchTerm）。
  const activeOptionId =
    highlight >= 0 && highlight < filtered.length
      ? optionElementId(listboxId, filtered[highlight].id)
      : undefined

  function setSearchTerm(next: string) {
    setSearch(next)
    setSelectedId(null)
    setHighlight(-1)
  }

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
    // 票 02：回车选中高亮项（搜索词已先走精确匹配；高亮索引在当前候选集内）。
    const highlighted =
      highlight >= 0 && highlight < filtered.length ? filtered[highlight] : undefined
    if (highlighted !== undefined) {
      setSelection(highlighted.id)
      onCommit(highlighted.id, move)
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
    <span className={styles.editor} ref={setHost}>
      <input
        type="text"
        role="combobox"
        aria-label="搜索选项"
        aria-expanded="true"
        aria-controls={listboxId}
        aria-activedescendant={activeOptionId}
        aria-invalid={error !== null}
        className={styles.input}
        value={search}
        autoFocus
        disabled={creating}
        onChange={(event) => setSearchTerm(event.target.value)}
        onKeyDown={(event) => {
          const moved = moveCandidateHighlight(event, highlight, filtered.length)
          if (moved !== null) {
            setHighlight(moved)
            return
          }
          consumeEditorKey(event, enterMove, onReject, (move) => {
            void commitSelection(move)
          })
        }}
      />
      <EditorError error={error} anchor={host} />
      <EditorPopup
        id={listboxId}
        anchor={host}
        className={styles.panel}
        role="listbox"
        ariaLabel="选项列表"
        minWidth={220}
      >
        {filtered.map((option) => (
          <button
            key={option.id}
            id={optionElementId(listboxId, option.id)}
            type="button"
            role="option"
            aria-selected={selectedId === option.id}
            data-selected={selectedId === option.id ? 'true' : undefined}
            className={styles.option}
            onPointerDown={preventOptionPointerDown}
            onClick={(event) => {
              // 票 02 / BUG-01：点击选项即提交并收起编辑器（此前只写本地草稿从不提交，值不落盘）。
              // stopPropagation：弹层 portal 后 React 树仍与单元格连通，点击冒泡会触发网格
              // handleCellClick 抢焦（focusViewport）→ 失焦提交竞态吞掉本次选择。
              event.stopPropagation()
              setSelection(option.id)
              onCommit(option.id, 'none')
            }}
          >
            {option.label}
          </button>
        ))}
        {noMatch ? (
          <span className={styles.panelError}>
            {allowCreate ? '按 Enter 新建并选中该选项' : '无匹配选项，请联系表格管理员添加'}
          </span>
        ) : null}
      </EditorPopup>
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
  // 同 select：仅当网格层确实注入 createOption 时才展示/执行新建入口。
  const allowCreate =
    optionCreateEnabled(optionConfig) && optionConfig.createOption !== undefined
  const [host, setHost] = useEditorHostAnchor()
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  // 票 02：方向键高亮导航（aria-activedescendant）；-1 = 未高亮，回车勾选高亮项。
  const listboxId = useId()
  const [highlight, setHighlight] = useState(-1)

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
      setSearchTerm('')
      return
    }
    if (allowCreate && optionConfig.createOption !== undefined) {
      setCreating(true)
      try {
        const created = await optionConfig.createOption(trimmed)
        if (created !== null) {
          const next = selected.includes(created) ? selected : [...selected, created]
          setSelected(next)
          // 草稿取勾选后集合（此前只上报单个 id，失败重试会丢已选集合）。
          onDraftChange?.(next.length === 0 ? null : next)
          setSearchTerm('')
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

  // aria-activedescendant 指向当前高亮候选（搜索词变化会重置高亮，见 setSearchTerm）。
  const activeOptionId =
    highlight >= 0 && highlight < filtered.length
      ? optionElementId(listboxId, filtered[highlight].id)
      : undefined

  function setSearchTerm(next: string) {
    setSearch(next)
    setHighlight(-1)
  }

  function commit(move: EditorCommitMove) {
    onCommit(selected.length === 0 ? null : selected, move)
  }

  return (
    <span className={styles.editor} ref={setHost}>
      <span className={styles.tagList}>
        {selected.map((id) => {
          const option = options.find((candidate) => candidate.id === id)
          // 票 04 去乱码：失效选项 id（配置中已删除）显示「已删除的选项」灰色标签，不显示原始 ID 长串。
          const label = option?.label ?? '已删除的选项'
          return (
            <span
              key={id}
              className={option === undefined ? `${styles.tag} ${styles.tagMissing}` : styles.tag}
            >
              {label}
              <button
                type="button"
                aria-label={`移除选项：${label}`}
                onClick={(event) => {
                  // 票 02：不冒泡到单元格（同选项按钮——防抢焦失焦提交竞态吞掉勾选变更）。
                  event.stopPropagation()
                  toggle(id)
                }}
              >
                ×
              </button>
            </span>
          )
        })}
      </span>
      <input
        type="text"
        role="combobox"
        aria-label="搜索选项"
        aria-expanded="true"
        aria-controls={listboxId}
        aria-activedescendant={activeOptionId}
        className={styles.input}
        value={search}
        autoFocus
        disabled={creating}
        onChange={(event) => setSearchTerm(event.target.value)}
        onKeyDown={(event) => {
          const moved = moveCandidateHighlight(event, highlight, filtered.length)
          if (moved !== null) {
            setHighlight(moved)
            return
          }
          if (event.key === 'Enter') {
            event.preventDefault()
            event.stopPropagation()
            // 票 02 顺序缺陷修复：搜索词非空时回车只匹配/勾选（编辑器保持打开、不提交旧集合）——
            // 此前 consumeEditorKey 先 commit 旧集合、addFromSearch 后执行，提交值永远落后一拍。
            if (search.trim() !== '') {
              void addFromSearch()
              return
            }
            // 搜索词为空且回车高亮项：勾选高亮项；高亮复位以便再次回车提交已选集合。
            const target =
              highlight >= 0 && highlight < filtered.length ? filtered[highlight] : undefined
            if (target !== undefined) {
              toggle(target.id)
              setHighlight(-1)
              return
            }
            commit(enterMove ?? 'down')
            return
          }
          consumeEditorKey(event, enterMove, onReject, commit)
        }}
      />
      <EditorError error={error} anchor={host} />
      <EditorPopup
        id={listboxId}
        anchor={host}
        className={styles.panel}
        role="listbox"
        ariaLabel="选项列表"
        minWidth={220}
      >
        {filtered.map((option) => (
          <button
            key={option.id}
            id={optionElementId(listboxId, option.id)}
            type="button"
            role="option"
            aria-selected={selected.includes(option.id)}
            className={styles.option}
            onPointerDown={preventOptionPointerDown}
            onClick={(event) => {
              // 票 02 / BUG-04：点击为勾选切换且编辑器保持打开（提交交给回车/失焦缝）；
              // stopPropagation 不冒泡到单元格（防抢焦失焦提交竞态吞掉勾选变更）。
              event.stopPropagation()
              toggle(option.id)
            }}
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
      </EditorPopup>
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
  const [host, setHost] = useEditorHostAnchor()
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
    <span className={styles.editor} ref={setHost}>
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
      <EditorError error={error} anchor={host} />
      <EditorPopup anchor={host} className={styles.calendar} role="dialog" ariaLabel="日期选择器">
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
      </EditorPopup>
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
  const [host, setHost] = useEditorHostAnchor()
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })
  // 票 02：方向键高亮导航（aria-activedescendant）；-1 = 未高亮，回车选中高亮候选。
  const listboxId = useId()
  const [highlight, setHighlight] = useState(-1)

  const needle = search.trim().toLocaleLowerCase()
  const candidates = Object.entries(users).filter(
    ([userId, info]) =>
      needle === '' ||
      info.name.toLocaleLowerCase().includes(needle) ||
      userId.toLocaleLowerCase().includes(needle),
  )
  // 候选统一为 {id,label} 形状以复用 matchOptionId（按名称精确匹配）。
  const candidateOptions = candidates.map(([userId, info]) => ({ id: userId, label: info.name }))

  // aria-activedescendant 指向当前高亮候选（搜索词变化会重置高亮，见 setSearchTerm）。
  const activeOptionId =
    highlight >= 0 && highlight < candidates.length
      ? optionElementId(listboxId, candidates[highlight][0])
      : undefined

  function setSearchTerm(next: string) {
    setSearch(next)
    setHighlight(-1)
  }

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

  /** 票 02：单选人员——点击候选/回车选中即提交并收起（与 select 点击语义一致）。 */
  function selectSingle(userId: string, move: EditorCommitMove) {
    setSelected([userId])
    onDraftChange?.(userId)
    onCommit(userId, move)
  }

  function commit(move: EditorCommitMove) {
    onCommit(
      allowMultiple ? (selected.length === 0 ? null : selected) : (selected[0] ?? null),
      move,
    )
  }

  /** 票 02：回车语义——搜索词非空先匹配（单选提交匹配项/多选勾选匹配项，不提交旧集合）；
   * 空词 + 高亮选中高亮项（多选勾选后复位高亮）；空词无高亮提交已选集合。 */
  function handleEnter() {
    const move = enterMove ?? 'down'
    const trimmed = search.trim()
    if (trimmed !== '') {
      const matched = matchOptionId(candidateOptions, trimmed)
      if (matched === null) return
      if (allowMultiple) {
        if (!selected.includes(matched)) toggle(matched)
        setSearchTerm('')
      } else {
        selectSingle(matched, move)
      }
      return
    }
    const highlightedId =
      highlight >= 0 && highlight < candidates.length ? candidates[highlight][0] : undefined
    if (highlightedId !== undefined) {
      if (allowMultiple) {
        toggle(highlightedId)
        setHighlight(-1)
      } else {
        selectSingle(highlightedId, move)
      }
      return
    }
    commit(move)
  }

  return (
    <span className={styles.editor} ref={setHost}>
      {selected.length === 0 ? null : (
        <span className={styles.tagList}>
          {selected.map((userId) => (
            <span key={userId} className={styles.tag}>
              {users[userId]?.name ?? userId}
              <button
                type="button"
                aria-label={`移除人员：${users[userId]?.name ?? userId}`}
                onClick={(event) => {
                  // 票 02：不冒泡到单元格（防抢焦失焦提交竞态吞掉勾选变更）。
                  event.stopPropagation()
                  toggle(userId)
                }}
              >
                ×
              </button>
            </span>
          ))}
        </span>
      )}
      <input
        type="text"
        role="combobox"
        aria-label="搜索人员"
        aria-expanded="true"
        aria-controls={listboxId}
        aria-activedescendant={activeOptionId}
        className={styles.input}
        value={search}
        autoFocus
        onChange={(event) => setSearchTerm(event.target.value)}
        onKeyDown={(event) => {
          const moved = moveCandidateHighlight(event, highlight, candidates.length)
          if (moved !== null) {
            setHighlight(moved)
            return
          }
          if (event.key === 'Enter') {
            event.preventDefault()
            event.stopPropagation()
            handleEnter()
            return
          }
          consumeEditorKey(event, enterMove, onReject, commit)
        }}
      />
      <EditorPopup
        id={listboxId}
        anchor={host}
        className={styles.panel}
        role="listbox"
        ariaLabel="人员候选"
        minWidth={220}
      >
        {candidates.map(([userId, info]) => (
          <button
            key={userId}
            id={optionElementId(listboxId, userId)}
            type="button"
            role="option"
            aria-selected={selected.includes(userId)}
            className={styles.option}
            onPointerDown={preventOptionPointerDown}
            onClick={(event) => {
              // 票 02：单选点击候选即提交收起；多选点击为勾选切换且编辑器保持打开；
              // stopPropagation 不冒泡到单元格（防抢焦失焦提交竞态）。
              event.stopPropagation()
              if (allowMultiple) toggle(userId)
              else selectSingle(userId, 'none')
            }}
          >
            {selected.includes(userId) ? '☑ ' : '☐ '}
            {info.name}
            {info.disabled === true ? '（已停用成员）' : ''}
          </button>
        ))}
      </EditorPopup>
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

/**
 * URL 编辑器配置（票 08 / BUG-02）：宿主可注入的紧凑标志，注入模式与 createOption/users 一致——
 * 网格窄列注入 compact 只渲染地址单输入（显示文本缺省取地址本身）；记录详情抽屉不注入，
 * 保留地址+显示文本双字段完整编辑。
 */
interface UrlEditorConfig {
  readonly compact?: boolean
}

function UrlCellEditor({
  value,
  config,
  initialInput,
  enterMove,
  onCommit,
  onReject,
  onDraftChange,
}: CellEditorProps) {
  const urlConfig = asConfig<UrlEditorConfig>(config)
  const compact = urlConfig.compact === true
  const parsed = parseUrlValue(value)
  const [host, setHost] = useEditorHostAnchor()
  const [url, setUrl] = useState(() =>
    initialInput !== undefined ? initialInput : parsed?.url ?? (typeof value === 'string' ? value : ''),
  )
  const [displayText, setDisplayText] = useState(parsed?.displayText ?? '')
  const [error, setError] = useState<string | null>(null)

  // 票 08：紧凑形态无显示文本输入，display_text 缺省取地址本身（normalizeUrlInput 空显示语义）；
  // 完整形态按显示文本输入框提交。草稿与提交值同为 {url, display_text} 对象（值编码字典），
  // 失焦提交缝（editorDraftRef）与键盘/点击提交通道取到一致形状。
  function draftValue(rawUrl: string): { url: string; display_text: string } | null {
    const normalized = normalizeUrlInput(rawUrl, compact ? '' : displayText)
    return normalized === null
      ? null
      : { url: normalized.url, display_text: normalized.displayText }
  }

  function commit(move: EditorCommitMove) {
    if (url.trim() === '') {
      onCommit(null, move)
      return
    }
    const normalized = draftValue(url)
    if (normalized === null) {
      setError('请输入合法的 http/https 链接')
      return
    }
    onCommit(normalized, move)
  }

  const urlInput = (
    <input
      type="text"
      aria-label="链接地址"
      className={styles.input}
      value={url}
      autoFocus
      aria-invalid={error !== null}
      onChange={(event) => {
        setUrl(event.target.value)
        const normalized = draftValue(event.target.value)
        if (normalized !== null) onDraftChange?.(normalized)
        if (error !== null) setError(null)
      }}
      onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
    />
  )

  return (
    <span className={styles.editor} ref={setHost}>
      {compact ? (
        urlInput
      ) : (
        <span className={styles.editorRow}>
          {urlInput}
          <input
            type="text"
            aria-label="显示文本"
            className={styles.input}
            value={displayText}
            onChange={(event) => setDisplayText(event.target.value)}
            onKeyDown={(event) => consumeEditorKey(event, enterMove, onReject, commit)}
          />
        </span>
      )}
      <EditorError error={error} anchor={host} />
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
  const [host, setHost] = useEditorHostAnchor()
  const [search, setSearch] = useState(initialInput ?? '')
  const [selected, setSelected] = useState<string[]>(() => {
    if (Array.isArray(value)) return value.map((item) => String(item))
    if (typeof value === 'string' && value !== '') return [value]
    return []
  })
  // 票 02：方向键高亮导航（aria-activedescendant）；-1 = 未高亮，回车选中高亮候选。
  const listboxId = useId()
  const [highlight, setHighlight] = useState(-1)

  const needle = search.trim().toLocaleLowerCase()
  const candidates = Object.entries(labels).filter(
    ([recordId, label]) =>
      needle === '' ||
      label.toLocaleLowerCase().includes(needle) ||
      recordId.toLocaleLowerCase().includes(needle),
  )
  // 候选统一为 {id,label} 形状以复用 matchOptionId（按名称精确匹配）。
  const candidateOptions = candidates.map(([recordId, label]) => ({ id: recordId, label }))

  // aria-activedescendant 指向当前高亮候选（搜索词变化会重置高亮，见 setSearchTerm）。
  const activeOptionId =
    highlight >= 0 && highlight < candidates.length
      ? optionElementId(listboxId, candidates[highlight][0])
      : undefined

  function setSearchTerm(next: string) {
    setSearch(next)
    setHighlight(-1)
  }

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

  /** 票 02：单选关联——点击候选/回车选中即提交并收起（与 select 点击语义一致）。 */
  function selectSingle(recordId: string, move: EditorCommitMove) {
    setSelected([recordId])
    onDraftChange?.(recordId)
    onCommit(recordId, move)
  }

  function commit(move: EditorCommitMove) {
    onCommit(
      allowMultiple ? (selected.length === 0 ? null : selected) : (selected[0] ?? null),
      move,
    )
  }

  /** 票 02：回车语义——搜索词非空先匹配（单选提交匹配项/多选勾选匹配项，不提交旧集合）；
   * 空词 + 高亮选中高亮项（多选勾选后复位高亮）；空词无高亮提交已选集合。 */
  function handleEnter() {
    const move = enterMove ?? 'down'
    const trimmed = search.trim()
    if (trimmed !== '') {
      const matched = matchOptionId(candidateOptions, trimmed)
      if (matched === null) return
      if (allowMultiple) {
        if (!selected.includes(matched)) toggle(matched)
        setSearchTerm('')
      } else {
        selectSingle(matched, move)
      }
      return
    }
    const highlightedId =
      highlight >= 0 && highlight < candidates.length ? candidates[highlight][0] : undefined
    if (highlightedId !== undefined) {
      if (allowMultiple) {
        toggle(highlightedId)
        setHighlight(-1)
      } else {
        selectSingle(highlightedId, move)
      }
      return
    }
    commit(move)
  }

  return (
    <span className={styles.editor} ref={setHost}>
      {selected.length === 0 ? null : (
        <span className={styles.tagList}>
          {selected.map((recordId) => (
            <span key={recordId} className={styles.tag}>
              {labels[recordId] ?? recordId}
              <button
                type="button"
                aria-label={`移除关联：${labels[recordId] ?? recordId}`}
                onClick={(event) => {
                  // 票 02：不冒泡到单元格（防抢焦失焦提交竞态吞掉勾选变更）。
                  event.stopPropagation()
                  toggle(recordId)
                }}
              >
                ×
              </button>
            </span>
          ))}
        </span>
      )}
      <input
        type="text"
        role="combobox"
        aria-label="搜索关联记录"
        aria-expanded="true"
        aria-controls={listboxId}
        aria-activedescendant={activeOptionId}
        className={styles.input}
        value={search}
        autoFocus
        onChange={(event) => setSearchTerm(event.target.value)}
        onKeyDown={(event) => {
          const moved = moveCandidateHighlight(event, highlight, candidates.length)
          if (moved !== null) {
            setHighlight(moved)
            return
          }
          if (event.key === 'Enter') {
            event.preventDefault()
            event.stopPropagation()
            handleEnter()
            return
          }
          consumeEditorKey(event, enterMove, onReject, commit)
        }}
      />
      <EditorPopup
        id={listboxId}
        anchor={host}
        className={styles.panel}
        role="listbox"
        ariaLabel="关联记录候选"
        minWidth={220}
      >
        {candidates.map(([recordId, label]) => (
          <button
            key={recordId}
            id={optionElementId(listboxId, recordId)}
            type="button"
            role="option"
            aria-selected={selected.includes(recordId)}
            className={styles.option}
            onPointerDown={preventOptionPointerDown}
            onClick={(event) => {
              // 票 02：单选点击候选即提交收起；多选点击为勾选切换且编辑器保持打开；
              // stopPropagation 不冒泡到单元格（防抢焦失焦提交竞态）。
              event.stopPropagation()
              if (allowMultiple) toggle(recordId)
              else selectSingle(recordId, 'none')
            }}
          >
            {selected.includes(recordId) ? '☑ ' : '☐ '}
            {label}
          </button>
        ))}
      </EditorPopup>
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
