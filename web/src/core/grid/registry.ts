// CellRenderer / CellEditor 注册表框架（票 08；技术方案 6.4 为接口定型唯一事实源）。
// 两注册表 renderers[fieldType] / editors[fieldType]：M1 交付框架 + text/number/select 演示实现，
// M2 起按需求 4.3 的 11 种字段类型矩阵注册扩展（类型键为字符串，M2 以接口文档字段类型枚举为准）。
// 框架逻辑（注册/查询/去重/兜底/生命周期流转）DOM 无关，直接单测；渲染器/编辑器本体是 React 实现（renderers/）。
//
// 去重策略（票面二选一，此处选「覆盖」）：重复注册同类型 = 后注册覆盖（last-wins），
// register 返回被替换的旧定义（undefined = 首次注册）。理由：M2 允许「默认实现 + 场景覆写」
// （按视图/主题替换渲染器），报错会阻断覆写路径；返回旧定义让装配层能对意外覆盖断言/告警，
// 任何覆盖都必须显式经过 register，不静默。
//
// 类型键：精确匹配字符串，不归一化（'text' 与 'Text' 视为两个类型）；空串合法但只会命中兜底。
import type { ComponentType } from 'react'

import { FALLBACK_EDITOR, FALLBACK_RENDERER } from './renderers/fallback'

/** 单元格水平对齐语义：网格渲染流（票 09 / M2）据此布局；演示组件以同语义 CSS 实现。 */
export type CellAlign = 'left' | 'right' | 'center'

/** 单元格裁剪语义：ellipsis = 单行省略（全量值经渲染器自身通道如 title/aria 可获取）。 */
export type CellClip = 'ellipsis' | 'visible'

/** 渲染器组件入参：value = 单元格值（null = 空单元格），config = 字段类型配置（字段 = 列，见 GLOSSARY）。 */
export interface CellRendererProps {
  readonly value: unknown
  readonly config: unknown
  /** 关联/引用单元格的跳转回调（票 05）：recordId → 目标记录。 */
  readonly onOpenRecord?: (recordId: string) => void
}

/** 渲染器定义：React 组件 + 对齐/裁剪语义元数据。 */
export interface CellRendererDef {
  readonly align: CellAlign
  readonly clip: CellClip
  readonly component: ComponentType<CellRendererProps>
}

/** 类型化注册入口（渲染器）：注册表以 unknown 存储全部字段类型的定义（存在类型去泛型），
 * 类型安全在各字段类型实现内部闭环；此处是唯一型变收窄点（集中一处、可审计）。 */
export function defineRenderer<Value, Config>(def: {
  readonly align: CellAlign
  readonly clip: CellClip
  readonly component: ComponentType<{ readonly value: Value | null; readonly config: Config }>
}): CellRendererDef {
  return def as unknown as CellRendererDef
}

/**
 * 编辑器提交移动语义（需求 4.4：Enter 提交下移、Tab 提交右移）。
 * 仅为语义信号：网格消费层（票 07 键位状态机 commit 信号 → nextFocus 换算）据此移动焦点，M1 不实现移动本身。
 */
export const EDITOR_COMMIT_MOVES = ['down', 'left', 'right', 'none'] as const
export type EditorCommitMove = (typeof EDITOR_COMMIT_MOVES)[number]

/**
 * 编辑器统一生命周期（技术方案 6.4：begin → input → validate → commit(reject)）。
 * 与具体字段类型解耦：Draft = 编辑草稿、Input = 一次输入事件载荷、Config = 字段类型配置、
 * Value = 提交值。reject 是生命周期的终止分支之一（取消编辑、丢弃草稿、不产出值），
 * 由会话持有方（编辑器组件/网格）直接触发，无钩子。
 */
export interface CellEditorLifecycle<Draft, Input, Config, Value = unknown> {
  /** begin：进入编辑——当前单元格值 + 字段配置 → 初始草稿。 */
  begin(args: { readonly value: unknown; readonly config: Config }): Draft
  /** input：一次输入 → 更新后的草稿（受控）。 */
  input(args: { readonly draft: Draft; readonly input: Input; readonly config: Config }): Draft
  /** validate：草稿校验——null = 通过；否则可访问错误文案（阻止提交，需求 4.4 校验异常行内错误态）。 */
  validate(args: { readonly draft: Draft; readonly config: Config }): string | null
  /** commit：草稿 → 提交值（仅在 validate 通过后被统一提交流调用）。 */
  commit(args: { readonly draft: Draft; readonly config: Config }): Value
}

/** 编辑器组件入参：会话持有方（网格）注入当前值/配置与两个生命周期终止分支的回调。 */
export interface CellEditorProps<Value = unknown, Config = unknown> {
  readonly value: Value | null
  readonly config: Config
  /** Enter 提交的移动语义（默认 'down'，可配；对接票 07 键位信号，网格消费层换算 nextFocus）。 */
  readonly enterMove?: EditorCommitMove
  /** 生命周期 commit 分支：validate 通过后的提交值 + 本次提交移动语义。 */
  readonly onCommit: (value: Value, move: EditorCommitMove) => void
  /** 生命周期 reject 分支：取消编辑、丢弃草稿、不落盘。 */
  readonly onReject: () => void
  /** 选中态直接打字进入编辑时的首字符（票 06 四通道之一）；缺省按当前值初始化草稿。 */
  readonly initialInput?: string
  /** 草稿变化通知（网格层用于失败重试 / 冲突「复制我的内容」取最新草稿）。 */
  readonly onDraftChange?: (value: unknown) => void
}

/** 编辑器定义：统一生命周期钩子 + React 组件（组件驱动钩子，键位经 resolveEditorKey 保证三键一致）。 */
export interface CellEditorDef {
  readonly lifecycle: CellEditorLifecycle<unknown, unknown, unknown>
  readonly component: ComponentType<CellEditorProps>
}

/** 类型化注册入口（编辑器）：型变收窄点，理由同 defineRenderer。 */
export function defineEditor<Value, Config, Draft, Input>(def: {
  readonly lifecycle: CellEditorLifecycle<Draft, Input, Config, Value>
  readonly component: ComponentType<CellEditorProps<Value, Config>>
}): CellEditorDef {
  return def as unknown as CellEditorDef
}

/** 三键行为（需求 4.4 / AC-REC-02）：Enter 提交（移动语义可配，默认下移）、Tab 提交右移、
 * Shift+Tab 提交左移、Esc 取消。全部编辑器共用本解析，保证三键行为跨字段类型一致；
 * 其余键交还编辑控件原生行为。 */
export type EditorKeyAction =
  | { readonly kind: 'commit'; readonly move: EditorCommitMove }
  | { readonly kind: 'reject' }
  | { readonly kind: 'pass' }

export interface EditorKeyOptions {
  readonly enterMove?: EditorCommitMove
  readonly shiftKey?: boolean
}

export function resolveEditorKey(key: string, options?: EditorKeyOptions): EditorKeyAction {
  switch (key) {
    case 'Enter':
      return { kind: 'commit', move: options?.enterMove ?? 'down' }
    case 'Tab':
      return { kind: 'commit', move: options?.shiftKey === true ? 'left' : 'right' }
    case 'Escape':
      return { kind: 'reject' }
    default:
      return { kind: 'pass' }
  }
}

/** 统一提交流结果：ok = 生命周期 commit 分支（提交值 + 移动语义）；失败 = validate 文案（阻止提交）。 */
export type EditorCommitOutcome<Value> =
  | { readonly ok: true; readonly value: Value; readonly move: EditorCommitMove }
  | { readonly ok: false; readonly error: string; readonly move: EditorCommitMove }

/** 统一提交流：validate 失败 → 阻止提交（返回错误文案，commit 钩子不执行）；通过 → commit 产出值。 */
export function commitEditorDraft<Draft, Input, Config, Value>(
  lifecycle: CellEditorLifecycle<Draft, Input, Config, Value>,
  args: { readonly draft: Draft; readonly config: Config },
  move: EditorCommitMove,
): EditorCommitOutcome<Value> {
  const error = lifecycle.validate({ draft: args.draft, config: args.config })
  if (error !== null) {
    return { ok: false, error, move }
  }
  return { ok: true, value: lifecycle.commit({ draft: args.draft, config: args.config }), move }
}

export interface CellRegistry {
  /** 注册渲染器；返回被替换的旧定义（undefined = 首次注册）。重复注册同类型 = 覆盖（见文件头去重策略）。 */
  registerRenderer(fieldType: string, def: CellRendererDef): CellRendererDef | undefined
  /** 按类型键取渲染器；未知类型返回兜底渲染器（安全显示原始值文本）。 */
  queryRenderer(fieldType: string): CellRendererDef
  /** 注册编辑器；返回被替换的旧定义（undefined = 首次注册）。重复注册同类型 = 覆盖（见文件头去重策略）。 */
  registerEditor(fieldType: string, def: CellEditorDef): CellEditorDef | undefined
  /** 按类型键取编辑器；未知类型返回兜底编辑器（通用字符串编辑，安全形态）。 */
  queryEditor(fieldType: string): CellEditorDef
}

export function createCellRegistry(): CellRegistry {
  const renderers = new Map<string, CellRendererDef>()
  const editors = new Map<string, CellEditorDef>()

  return {
    registerRenderer(fieldType, def) {
      const previous = renderers.get(fieldType)
      renderers.set(fieldType, def)
      return previous
    },
    queryRenderer(fieldType) {
      return renderers.get(fieldType) ?? FALLBACK_RENDERER
    },
    registerEditor(fieldType, def) {
      const previous = editors.get(fieldType)
      editors.set(fieldType, def)
      return previous
    },
    queryEditor(fieldType) {
      return editors.get(fieldType) ?? FALLBACK_EDITOR
    },
  }
}
