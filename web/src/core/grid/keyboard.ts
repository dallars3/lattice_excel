// 键位状态机（票 07，需求文档 3.8.1 键盘快捷键表（P0）为键位行为唯一事实源；3.8.2 编辑触发、4.4 记录编辑、AC-REC-02）。
// 纯函数：输入 (state, key event, bounds) → 输出 (next state, 副作用信号)。
// 信号仅为信号：appendRow（新增行）/ clipboard / undo / find / clearCells 的真实逻辑归消费层
// （票 09 接线、剪贴板 TSV 在技术方案 6.3、撤销在 4.4），M1 不实现。
//
// 语义对照（需求 3.8.1）：
// - 方向键：移动选中单元格（idle 态）；编辑态归编辑器持有（单元格内光标），grid 级 no-op。
// - Enter：编辑态提交并下移（AC-REC-02）；idle 态进入编辑（3.8.2）；末行数据区等价触发新增记录。
// - Tab / Shift+Tab：提交并右移 / 左移；idle 态纯跨列移动。需求文档未定义行尾回绕 → 钳制不回绕
//   （「提交并右移」的语义是同行业内跨列；越界时提交仍发生、焦点钳制在边界）。
// - F2 / 双击：进入编辑（双击经 requestEdit 同一迁移，3.8.2 三通道）。
// - Esc：取消本次编辑（AC-REC-02：取消不落盘）；idle 态键位表未定义 → no-op。
// - Delete：清空选中单元格（信号携带归一化选区矩形）。
// - Ctrl/Cmd + Z / C / V / X / F：撤销 / 复制（TSV）/ 粘贴 / 剪切 / 视图内搜索（信号）。
import { clampCell, type CellRef, type GridBounds } from './common'
import { selectionRect, type Selection, type SelectionRect } from './selection'

export const GRID_EDIT_MODES = ['idle', 'editing'] as const
export type GridEditMode = (typeof GRID_EDIT_MODES)[number]

/** 键清单（需求 3.8.1；Ctrl/Cmd 统一为 ctrl 修饰位，shift 为 Shift 位）。 */
export const GRID_KEYS = [
  'ArrowUp',
  'ArrowDown',
  'ArrowLeft',
  'ArrowRight',
  'Enter',
  'Tab',
  'F2',
  'Escape',
  'Delete',
  'c',
  'v',
  'x',
  'z',
  'f',
] as const
export type GridKey = (typeof GRID_KEYS)[number]

export interface GridKeyEvent {
  readonly key: GridKey
  /** Ctrl / Cmd 统一为 true（需求 3.8.1「Ctrl/Cmd」双写）。 */
  readonly ctrl?: boolean
  readonly shift?: boolean
}

export interface KeyboardState {
  readonly mode: GridEditMode
  /** 键盘焦点所在单元格；null = 无焦点（未初始化）。 */
  readonly focus: CellRef | null
  /** 选择锚点（拖选/扩展语义；null 视为随焦点收拢成单格）。 */
  readonly anchor: CellRef | null
}

/** 副作用信号（消费层据此执行真实动作；M1 全部不落地实现）。 */
export type GridKeyEffect =
  | { readonly kind: 'beginEdit'; readonly cell: CellRef }
  | { readonly kind: 'commit'; readonly cell: CellRef; readonly nextFocus: CellRef }
  | { readonly kind: 'cancel'; readonly cell: CellRef }
  /** Delete：清空选中（矩形内单元格置空由消费层执行）。 */
  | { readonly kind: 'clearCells'; readonly rect: SelectionRect }
  /** Ctrl+C / X / V：TSV 剪贴板（技术方案 6.3，M1 不实现）。 */
  | { readonly kind: 'clipboard'; readonly action: 'copy' | 'paste' | 'cut'; readonly rect: SelectionRect }
  /** Ctrl+Z：撤销本次会话内最近一步编辑（M1 不实现）。 */
  | { readonly kind: 'undo' }
  /** Ctrl+F：呼出当前视图内搜索（前端增强，M1 不实现）。 */
  | { readonly kind: 'find' }
  /** 末行 ↓ / 末行 Enter：新增行信号（M1 不实现新增逻辑；新增后焦点落主字段由消费层处理）。 */
  | { readonly kind: 'appendRow' }

export interface KeyTransition {
  readonly state: KeyboardState
  readonly effects: readonly GridKeyEffect[]
}

/** 初始态：idle、无焦点。 */
export function initialKeyboardState(): KeyboardState {
  return { mode: 'idle', focus: null, anchor: null }
}

/** 当前态的有效选区矩形（锚点缺省收拢到焦点）。 */
function currentRect(state: KeyboardState): SelectionRect {
  if (state.focus === null) {
    return { top: 0, left: 0, bottom: -1, right: -1 }
  }
  const anchor = state.anchor ?? state.focus
  return selectionRect({ kind: 'range', anchor, focus: state.focus })
}

/** 无变化迁移（原状态引用返回，便于断言与消费层跳过重渲染）。 */
const UNCHANGED = (state: KeyboardState): KeyTransition => ({ state, effects: [] })

/** 焦点移动（idle 态方向键/Tab）：焦点钳制、锚点收拢随焦点。末行 ↓ 的 appendRow 由调用方先行判断。 */
function moved(state: KeyboardState, bounds: GridBounds, dRow: number, dCol: number): KeyTransition {
  if (state.focus === null) {
    return UNCHANGED(state)
  }
  const nextFocus = clampCell({ row: state.focus.row + dRow, col: state.focus.col + dCol }, bounds)
  return { state: { mode: 'idle', focus: nextFocus, anchor: nextFocus }, effects: [] }
}

/**
 * 键位状态机主迁移：state + key event → next state + 副作用信号。
 * 未知键 / 无焦点 / 键位表未定义的组合一律 no-op（原状态原样返回、零信号，不抛错）。
 */
export function applyKey(state: KeyboardState, event: GridKeyEvent, bounds: GridBounds): KeyTransition {
  const focus = state.focus
  if (focus === null) {
    return UNCHANGED(state)
  }
  const atLastRow = focus.row >= bounds.rowCount - 1

  if (state.mode === 'editing') {
    // 编辑态：Enter/Tab/Esc 三键归状态机（AC-REC-02），其余归编辑器持有 → no-op
    switch (event.key) {
      case 'Enter': {
        // 提交并下移（AC-REC-02）；末行钳制不动（新增触发在非编辑态末行 Enter/↓）
        const nextFocus = clampCell({ row: focus.row + 1, col: focus.col }, bounds)
        return {
          state: { mode: 'idle', focus: nextFocus, anchor: nextFocus },
          effects: [{ kind: 'commit', cell: focus, nextFocus }],
        }
      }
      case 'Tab': {
        // 提交并右移 / 左移（AC-REC-02）；需求未定义回绕 → 行尾/行首钳制
        const dCol = event.shift === true ? -1 : 1
        const nextFocus = clampCell({ row: focus.row, col: focus.col + dCol }, bounds)
        return {
          state: { mode: 'idle', focus: nextFocus, anchor: nextFocus },
          effects: [{ kind: 'commit', cell: focus, nextFocus }],
        }
      }
      case 'Escape':
        // 取消本次编辑，不落盘（AC-REC-02）；焦点不变
        return { state: { mode: 'idle', focus, anchor: state.anchor }, effects: [{ kind: 'cancel', cell: focus }] }
      default:
        return UNCHANGED(state)
    }
  }

  // idle 态
  switch (event.key) {
    case 'ArrowUp':
      return moved(state, bounds, -1, 0)
    case 'ArrowDown':
      // 末行 ↓ 触发新增行信号（需求 4.4「末行方向下键」），焦点不动
      if (atLastRow) {
        return { state, effects: [{ kind: 'appendRow' }] }
      }
      return moved(state, bounds, 1, 0)
    case 'ArrowLeft':
      return moved(state, bounds, 0, -1)
    case 'ArrowRight':
      return moved(state, bounds, 0, 1)
    case 'Enter':
      // 末行数据区等价触发新增记录（需求 3.8.1）；否则进入编辑（3.8.2）
      if (atLastRow) {
        return { state, effects: [{ kind: 'appendRow' }] }
      }
      return { state: { mode: 'editing', focus, anchor: state.anchor }, effects: [{ kind: 'beginEdit', cell: focus }] }
    case 'Tab':
      return moved(state, bounds, 0, event.shift === true ? -1 : 1)
    case 'F2':
      return { state: { mode: 'editing', focus, anchor: state.anchor }, effects: [{ kind: 'beginEdit', cell: focus }] }
    case 'Escape':
      return UNCHANGED(state) // 键位表仅定义取消编辑；idle 态未定义
    case 'Delete':
      return { state, effects: [{ kind: 'clearCells', rect: currentRect(state) }] }
    case 'c':
      if (event.ctrl === true) {
        return { state, effects: [{ kind: 'clipboard', action: 'copy', rect: currentRect(state) }] }
      }
      return UNCHANGED(state)
    case 'x':
      if (event.ctrl === true) {
        return { state, effects: [{ kind: 'clipboard', action: 'cut', rect: currentRect(state) }] }
      }
      return UNCHANGED(state)
    case 'v':
      if (event.ctrl === true) {
        return { state, effects: [{ kind: 'clipboard', action: 'paste', rect: currentRect(state) }] }
      }
      return UNCHANGED(state)
    case 'z':
      if (event.ctrl === true) {
        return { state, effects: [{ kind: 'undo' }] }
      }
      return UNCHANGED(state)
    case 'f':
      if (event.ctrl === true) {
        return { state, effects: [{ kind: 'find' }] }
      }
      return UNCHANGED(state)
    default:
      return UNCHANGED(state)
  }
}

/**
 * 双击进入编辑（需求 3.8.2 三通道之一）：与 F2 同一 beginEdit 迁移。
 * cell 为指针命中的单元格（钳制到边界）；双击即选中该格并进入编辑，原选择态不保留。
 * 入参 state 保持状态机接缝形状（state + 输入 → 迁移），迁移本身只依赖命中格。
 */
export function requestEdit(_state: KeyboardState, cell: CellRef, bounds: GridBounds): KeyTransition {
  const clamped = clampCell(cell, bounds)
  return {
    state: { mode: 'editing', focus: clamped, anchor: clamped },
    effects: [{ kind: 'beginEdit', cell: clamped }],
  }
}

/**
 * 把 SelectionModel 的选择结果并入键盘态（票 09 接线缝：拖选/整行/全选后键盘态与选择态同步）。
 * focus 取 selection.focus、anchor 取 selection.anchor；selection 为 null 时锚点收拢回焦点。
 */
export function withSelection(state: KeyboardState, selection: Selection | null): KeyboardState {
  if (selection === null) {
    return { ...state, anchor: state.focus }
  }
  return { mode: state.mode, focus: selection.focus, anchor: selection.anchor }
}
