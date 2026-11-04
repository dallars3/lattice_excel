// 兜底渲染器 / 兜底编辑器（票 08）：未知字段类型的缺省实现（技术方案 6.4「未知类型返回兜底」）。
// 渲染器安全显示原始值文本：null/undefined → 空串，其余 String() 化——永不抛错、不丢 0/false 等合法假值；
// 全量值经 title 可获取（与 text 演示渲染器同一「全量值可获取」通道）。
// 编辑器为通用字符串编辑：值 String() 化进入草稿、无类型校验（值形态未知），生命周期统一。
// 注意：本文件对 ../registry 只做类型导入（运行时零依赖）——registry.ts 运行时反向导入本文件
// 作为兜底默认值，若引入运行时回环会在模块初始化期炸出「defineEditor is not a function」。
import { useState } from 'react'

import {
  commitEditorDraft,
  resolveEditorKey,
  type CellEditorDef,
  type CellEditorLifecycle,
  type CellEditorProps,
  type CellRendererDef,
} from '../registry'

import styles from './fallback.module.css'

/** 兜底安全文本化：null/undefined → 空串，其余 String()。 */
export function fallbackCellText(value: unknown): string {
  return value === null || value === undefined ? '' : String(value)
}

function FallbackCell({ value }: { readonly value: unknown; readonly config: unknown }) {
  const text = fallbackCellText(value)
  return (
    <span className={styles.fallback} title={text === '' ? undefined : text}>
      {text}
    </span>
  )
}

/** 未知字段类型兜底渲染器：安全文本、左对齐、单行省略（原始值形态未知，裁剪显示最稳妥）。 */
export const FALLBACK_RENDERER: CellRendererDef = {
  align: 'left',
  clip: 'ellipsis',
  component: FallbackCell,
}

const FALLBACK_LIFECYCLE: CellEditorLifecycle<string, string, unknown, string> = {
  begin: ({ value }) => fallbackCellText(value),
  input: ({ input }) => input,
  // 兜底编辑器不做类型校验（值形态未知，校验语义归 M2 各字段类型实现）
  validate: () => null,
  commit: ({ draft }) => draft,
}

function FallbackCellEditor({ value, config, enterMove, onCommit, onReject }: CellEditorProps<string, unknown>) {
  const [draft, setDraft] = useState(() => FALLBACK_LIFECYCLE.begin({ value, config }))
  return (
    <input
      type="text"
      className={styles.editor}
      value={draft}
      onChange={(event) => setDraft(FALLBACK_LIFECYCLE.input({ draft, input: event.target.value, config }))}
      onKeyDown={(event) => {
        const action = resolveEditorKey(event.key, { enterMove, shiftKey: event.shiftKey })
        if (action.kind === 'pass') {
          return
        }
        event.preventDefault()
        if (action.kind === 'reject') {
          onReject()
          return
        }
        const outcome = commitEditorDraft(FALLBACK_LIFECYCLE, { draft, config }, action.move)
        if (outcome.ok) {
          onCommit(outcome.value, action.move)
        }
      }}
    />
  )
}

/** 未知字段类型兜底编辑器：通用字符串编辑（统一生命周期 + 三键一致）。
 * 以字面量 + 局部型变收窄定义（不用 defineEditor 助手，避免对 registry 的运行时依赖，见文件头）。 */
export const FALLBACK_EDITOR: CellEditorDef = {
  lifecycle: FALLBACK_LIFECYCLE as unknown as CellEditorLifecycle<unknown, unknown, unknown>,
  component: FallbackCellEditor as unknown as CellEditorDef['component'],
}
