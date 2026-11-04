// text 演示（票 08）：渲染器（长文本单行省略，全量值经 title 可获取——需求 4.3 多行文本行
// 「单行省略，hover/抽屉看全文」的 M1 网格态）+ 编辑器（统一生命周期演示实现，技术方案 6.4）。
// 编辑器与字段类型解耦：生命周期钩子（TEXT_LIFECYCLE）为纯函数，组件只驱动会话与三键分发。
import { useState } from 'react'

import {
  commitEditorDraft,
  defineEditor,
  defineRenderer,
  resolveEditorKey,
  type CellEditorLifecycle,
  type CellEditorProps,
} from '../registry'

import styles from './text.module.css'

/** text（多行文本）字段类型配置：编辑字符上限（需求 4.3 ≤10,000 字符）。 */
export interface TextCellConfig {
  readonly maxLength?: number
}

const DEFAULT_MAX_LENGTH = 10000

function TextCell({ value }: { readonly value: unknown; readonly config: unknown }) {
  // 内部窄化点：非字符串值 String() 化安全显示（不抛错、不丢 0/false）
  const text = value === null || value === undefined ? '' : String(value)
  return (
    <span className={styles.text} title={text === '' ? undefined : text}>
      {text}
    </span>
  )
}

/** text 字段演示渲染器：省略在 CSS 层实现，全量值始终渲染在 DOM 中（title + 文本节点双通道）。 */
export const TEXT_RENDERER = defineRenderer<string, TextCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: TextCell,
})

/** text 编辑器统一生命周期钩子（纯函数，M2 其他字段类型按同契约实现各自钩子）。
 * Config 容忍缺省（未配置字段进入编辑仍须可用，注册表边界 config 为 unknown）。 */
export const TEXT_LIFECYCLE: CellEditorLifecycle<string, string, TextCellConfig | undefined, string> = {
  begin: ({ value }) => (value === null || value === undefined ? '' : String(value)),
  // 受控输入：草稿即输入串（M2 多行文本域的换行处理在此钩子内扩展，组件不动）
  input: ({ input }) => input,
  validate: ({ draft, config }) => {
    // config 可能缺省（注册表边界为 unknown，未配置字段时进入编辑仍须可用）
    const maxLength = config?.maxLength ?? DEFAULT_MAX_LENGTH
    return draft.length > maxLength ? `内容超出上限 ${maxLength} 字符` : null
  },
  commit: ({ draft }) => draft,
}

const EDITOR_ERROR_ID = 'grid-cell-editor-error'

function TextCellEditor({ value, config, enterMove, onCommit, onReject }: CellEditorProps<string, TextCellConfig | undefined>) {
  const [draft, setDraft] = useState(() => TEXT_LIFECYCLE.begin({ value, config }))
  const [error, setError] = useState<string | null>(null)

  const handleInput = (raw: string) => {
    const next = TEXT_LIFECYCLE.input({ draft, input: raw, config })
    setDraft(next)
    // 已有错误时随输入重校验：修正后错误即时消除（需求 4.4 校验异常行内错误态）
    if (error !== null) {
      setError(TEXT_LIFECYCLE.validate({ draft: next, config }))
    }
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    const action = resolveEditorKey(event.key, { enterMove, shiftKey: event.shiftKey })
    if (action.kind === 'pass') {
      return
    }
    event.preventDefault()
    if (action.kind === 'reject') {
      onReject() // 生命周期 reject 分支：取消编辑、丢弃草稿、不落盘
      return
    }
    const outcome = commitEditorDraft(TEXT_LIFECYCLE, { draft, config }, action.move)
    if (outcome.ok) {
      onCommit(outcome.value, action.move)
    } else {
      setError(outcome.error) // validate 失败：阻止提交，行内可访问错误提示
    }
  }

  return (
    <span className={styles.editor}>
      <input
        type="text"
        className={styles.editor__input}
        value={draft}
        onChange={(event) => handleInput(event.target.value)}
        onKeyDown={handleKeyDown}
        aria-invalid={error !== null}
        aria-describedby={error !== null ? EDITOR_ERROR_ID : undefined}
      />
      {error !== null ? (
        <span id={EDITOR_ERROR_ID} role="alert" className={styles.editor__error}>
          {error}
        </span>
      ) : null}
    </span>
  )
}

/** text 字段演示编辑器：统一生命周期 + 三键一致（Enter 提交下移/Tab 提交右移/Esc 取消）。 */
export const TEXT_EDITOR = defineEditor<string, TextCellConfig | undefined, string, string>({
  lifecycle: TEXT_LIFECYCLE,
  component: TextCellEditor,
})
