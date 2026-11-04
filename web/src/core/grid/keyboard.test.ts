// 键位状态机表驱动测试（票 07）。
// 事实源：需求文档 3.8.1 键盘快捷键表（P0）、3.8.2 编辑触发三通道、4.4 记录编辑、AC-REC-02。
// 语义：输入 (state, key event, bounds) → 输出 (next state, 副作用信号)；信号仅为信号，
// M1 不实现新增行/剪贴板/撤销/查找的真实逻辑（消费方是票 09/M2）。
import { describe, expect, it } from 'vitest'

import type { GridBounds } from './common'
import {
  applyKey,
  initialKeyboardState,
  requestEdit,
  withSelection,
  type GridKeyEvent,
  type KeyboardState,
} from './keyboard'

const BOUNDS: GridBounds = { rowCount: 5, colCount: 3 }

/** 便捷构造：idle 态、焦点在 (row,col)、锚点缺省随焦点。 */
function idle(row: number, col: number, anchor?: { row: number; col: number }): KeyboardState {
  return { mode: 'idle', focus: { row, col }, anchor: anchor ?? { row, col } }
}

/** 便捷构造：编辑态、焦点在 (row,col)。 */
function editing(row: number, col: number): KeyboardState {
  return { mode: 'editing', focus: { row, col }, anchor: { row, col } }
}

function key(k: GridKeyEvent['key'], mods?: { ctrl?: boolean; shift?: boolean }): GridKeyEvent {
  return { key: k, ...(mods ?? {}) }
}

describe('初始态', () => {
  it('初始态：idle、无焦点', () => {
    expect(initialKeyboardState()).toEqual({ mode: 'idle', focus: null, anchor: null })
  })

  it('无焦点时任何键均 no-op（不抛错、不产信号）', () => {
    const state = initialKeyboardState()
    for (const event of [key('ArrowDown'), key('Enter'), key('F2'), key('Delete'), key('c', { ctrl: true })]) {
      const transition = applyKey(state, event, BOUNDS)
      expect(transition.state).toBe(state)
      expect(transition.effects).toEqual([])
    }
  })
})

describe('导航（方向键移动选中单元格）', () => {
  const cases: ReadonlyArray<{
    readonly name: string
    readonly state: KeyboardState
    readonly event: GridKeyEvent
    readonly expectedFocus: { row: number; col: number }
  }> = [
    { name: '右移', state: idle(1, 1), event: key('ArrowRight'), expectedFocus: { row: 1, col: 2 } },
    { name: '左移', state: idle(1, 1), event: key('ArrowLeft'), expectedFocus: { row: 1, col: 0 } },
    { name: '下移', state: idle(1, 1), event: key('ArrowDown'), expectedFocus: { row: 2, col: 1 } },
    { name: '上移', state: idle(1, 1), event: key('ArrowUp'), expectedFocus: { row: 0, col: 1 } },
    { name: '右移在末列钳制', state: idle(1, 2), event: key('ArrowRight'), expectedFocus: { row: 1, col: 2 } },
    { name: '左移在首列钳制', state: idle(1, 0), event: key('ArrowLeft'), expectedFocus: { row: 1, col: 0 } },
    { name: '上移在首行钳制', state: idle(0, 1), event: key('ArrowUp'), expectedFocus: { row: 0, col: 1 } },
  ]
  for (const c of cases) {
    it(`${c.name} → (${c.expectedFocus.row},${c.expectedFocus.col})`, () => {
      const transition = applyKey(c.state, c.event, BOUNDS)
      expect(transition.effects).toEqual([])
      expect(transition.state.mode).toBe('idle')
      expect(transition.state.focus).toEqual(c.expectedFocus)
      expect(transition.state.anchor).toEqual(c.expectedFocus) // 方向键移动选中单元格：锚点随焦点收拢
    })
  }

  it('末行按 ↓ 触发新增行信号（信号而已，M1 不实现新增；需求 4.4 末行方向下键）', () => {
    const transition = applyKey(idle(4, 1), key('ArrowDown'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'appendRow' }])
    expect(transition.state.focus).toEqual({ row: 4, col: 1 }) // 焦点不动
    expect(transition.state.mode).toBe('idle')
  })
})

describe('选中与编辑进入（3.8.2 三通道中的键通道）', () => {
  it('F2 进入编辑（焦点格）', () => {
    const transition = applyKey(idle(2, 1), key('F2'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'beginEdit', cell: { row: 2, col: 1 } }])
    expect(transition.state.mode).toBe('editing')
    expect(transition.state.focus).toEqual({ row: 2, col: 1 })
  })

  it('idle 态 Enter 进入编辑（3.8.2：Enter 进入编辑）', () => {
    const transition = applyKey(idle(1, 0), key('Enter'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'beginEdit', cell: { row: 1, col: 0 } }])
    expect(transition.state.mode).toBe('editing')
  })

  it('idle 态末行 Enter 等价触发新增记录（3.8.1 键位表），不发 beginEdit', () => {
    const transition = applyKey(idle(4, 2), key('Enter'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'appendRow' }])
    expect(transition.state.mode).toBe('idle')
  })

  it('双击通道 requestEdit：等价 F2 的 beginEdit 语义（3.8.2）', () => {
    const transition = requestEdit(idle(3, 2), { row: 3, col: 2 }, BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'beginEdit', cell: { row: 3, col: 2 } }])
    expect(transition.state.mode).toBe('editing')
    expect(transition.state.focus).toEqual({ row: 3, col: 2 })
  })
})

describe('Tab / Shift+Tab 跨列', () => {
  it('idle 态 Tab 右移、行尾钳制；Shift+Tab 左移、行首钳制（需求未定义回绕 → 钳制不回绕）', () => {
    const right = applyKey(idle(1, 0), key('Tab'), BOUNDS)
    expect(right.state.focus).toEqual({ row: 1, col: 1 })
    const rightEnd = applyKey(idle(1, 2), key('Tab'), BOUNDS)
    expect(rightEnd.state.focus).toEqual({ row: 1, col: 2 })
    const left = applyKey(idle(1, 1), key('Tab', { shift: true }), BOUNDS)
    expect(left.state.focus).toEqual({ row: 1, col: 0 })
    const leftStart = applyKey(idle(1, 0), key('Tab', { shift: true }), BOUNDS)
    expect(leftStart.state.focus).toEqual({ row: 1, col: 0 })
  })

  it('编辑态 Tab 提交并右移（AC-REC-02），mode 回 idle', () => {
    const transition = applyKey(editing(1, 0), key('Tab'), BOUNDS)
    expect(transition.effects).toEqual([
      { kind: 'commit', cell: { row: 1, col: 0 }, nextFocus: { row: 1, col: 1 } },
    ])
    expect(transition.state.mode).toBe('idle')
    expect(transition.state.focus).toEqual({ row: 1, col: 1 })
  })

  it('编辑态 Shift+Tab 提交并左移', () => {
    const transition = applyKey(editing(1, 2), key('Tab', { shift: true }), BOUNDS)
    expect(transition.effects).toEqual([
      { kind: 'commit', cell: { row: 1, col: 2 }, nextFocus: { row: 1, col: 1 } },
    ])
  })

  it('编辑态末列 Tab 提交后钳制不动（不回绕换行）', () => {
    const transition = applyKey(editing(1, 2), key('Tab'), BOUNDS)
    expect(transition.effects).toEqual([
      { kind: 'commit', cell: { row: 1, col: 2 }, nextFocus: { row: 1, col: 2 } },
    ])
  })
})

describe('编辑态：Enter 提交下移 / Esc 取消不落盘（AC-REC-02）', () => {
  it('Enter 提交并下移', () => {
    const transition = applyKey(editing(1, 1), key('Enter'), BOUNDS)
    expect(transition.effects).toEqual([
      { kind: 'commit', cell: { row: 1, col: 1 }, nextFocus: { row: 2, col: 1 } },
    ])
    expect(transition.state.mode).toBe('idle')
    expect(transition.state.focus).toEqual({ row: 2, col: 1 })
  })

  it('末行编辑态 Enter 提交、焦点钳制在末行（新增触发在非编辑态末行 Enter/↓）', () => {
    const transition = applyKey(editing(4, 1), key('Enter'), BOUNDS)
    expect(transition.effects).toEqual([
      { kind: 'commit', cell: { row: 4, col: 1 }, nextFocus: { row: 4, col: 1 } },
    ])
    expect(transition.state.mode).toBe('idle')
    expect(transition.state.focus).toEqual({ row: 4, col: 1 })
  })

  it('Esc 取消本次编辑：mode 回 idle、焦点不变、取消信号（不落盘）', () => {
    const transition = applyKey(editing(2, 1), key('Escape'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'cancel', cell: { row: 2, col: 1 } }])
    expect(transition.state.mode).toBe('idle')
    expect(transition.state.focus).toEqual({ row: 2, col: 1 })
  })

  it('编辑态下方向键/F2/Delete/Ctrl 组合归编辑器持有：grid 级 no-op', () => {
    const state = editing(1, 1)
    for (const event of [
      key('ArrowDown'),
      key('ArrowUp'),
      key('ArrowLeft'),
      key('ArrowRight'),
      key('F2'),
      key('Delete'),
      key('c', { ctrl: true }),
      key('v', { ctrl: true }),
      key('x', { ctrl: true }),
      key('z', { ctrl: true }),
      key('f', { ctrl: true }),
    ]) {
      const transition = applyKey(state, event, BOUNDS)
      expect(transition.effects).toEqual([])
      expect(transition.state).toBe(state) // 原状态原样返回（引用不变）
    }
  })
})

describe('Delete 清空选中（3.8.1）', () => {
  it('单格：清空信号覆盖焦点格', () => {
    const transition = applyKey(idle(2, 1), key('Delete'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'clearCells', rect: { top: 2, left: 1, bottom: 2, right: 1 } }])
  })

  it('区域：清空信号覆盖 anchor..focus 归一化矩形（反向拖选）', () => {
    const state = idle(1, 2, { row: 3, col: 0 }) // focus (1,2)、anchor (3,0)（拖选后态）
    const transition = applyKey(state, key('Delete'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'clearCells', rect: { top: 1, left: 0, bottom: 3, right: 2 } }])
  })

  it('withSelection：把 SelectionModel 的拖选结果并入键盘态（票 09 接线缝）', () => {
    const state = withSelection(idle(2, 1), { kind: 'range', anchor: { row: 1, col: 0 }, focus: { row: 3, col: 2 } })
    expect(state.focus).toEqual({ row: 3, col: 2 })
    expect(state.anchor).toEqual({ row: 1, col: 0 })
    const transition = applyKey(state, key('Delete'), BOUNDS)
    expect(transition.effects).toEqual([{ kind: 'clearCells', rect: { top: 1, left: 0, bottom: 3, right: 2 } }])
  })
})

describe('Ctrl/Cmd 组合键（3.8.1）', () => {
  it('Ctrl+C 复制（TSV 语义由消费层落实）/ Ctrl+X 剪切 / Ctrl+V 粘贴，信号携带选区矩形', () => {
    const state = idle(1, 1, { row: 0, col: 0 })
    expect(applyKey(state, key('c', { ctrl: true }), BOUNDS).effects).toEqual([
      { kind: 'clipboard', action: 'copy', rect: { top: 0, left: 0, bottom: 1, right: 1 } },
    ])
    expect(applyKey(state, key('x', { ctrl: true }), BOUNDS).effects).toEqual([
      { kind: 'clipboard', action: 'cut', rect: { top: 0, left: 0, bottom: 1, right: 1 } },
    ])
    expect(applyKey(state, key('v', { ctrl: true }), BOUNDS).effects).toEqual([
      { kind: 'clipboard', action: 'paste', rect: { top: 0, left: 0, bottom: 1, right: 1 } },
    ])
  })

  it('Ctrl+Z 撤销信号 / Ctrl+F 呼出视图内搜索信号', () => {
    expect(applyKey(idle(0, 0), key('z', { ctrl: true }), BOUNDS).effects).toEqual([{ kind: 'undo' }])
    expect(applyKey(idle(0, 0), key('f', { ctrl: true }), BOUNDS).effects).toEqual([{ kind: 'find' }])
  })

  it('无修饰键的字母键不触发剪贴板/撤销/查找（no-op）', () => {
    const state = idle(1, 1)
    for (const event of [key('c'), key('v'), key('x'), key('z'), key('f')]) {
      const transition = applyKey(state, event, BOUNDS)
      expect(transition.effects).toEqual([])
      expect(transition.state).toBe(state)
    }
  })
})

describe('未知键与不变量', () => {
  it('未知键名 no-op（不抛错）', () => {
    const state = idle(1, 1)
    const transition = applyKey(state, { key: 'PageDown' as never }, BOUNDS)
    expect(transition.effects).toEqual([])
    expect(transition.state).toBe(state)
  })

  it('主路径全表：state+key → effects/state 的键位表逐行断言', () => {
    const rows: ReadonlyArray<{
      readonly name: string
      readonly state: KeyboardState
      readonly event: GridKeyEvent
      readonly expectedEffects: ReadonlyArray<Record<string, unknown>>
      readonly expectedMode: 'idle' | 'editing'
    }> = [
      { name: 'idle+F2', state: idle(0, 0), event: key('F2'), expectedEffects: [{ kind: 'beginEdit', cell: { row: 0, col: 0 } }], expectedMode: 'editing' },
      { name: 'idle+Enter(非末行)', state: idle(0, 0), event: key('Enter'), expectedEffects: [{ kind: 'beginEdit', cell: { row: 0, col: 0 } }], expectedMode: 'editing' },
      { name: 'idle+Enter(末行)', state: idle(4, 0), event: key('Enter'), expectedEffects: [{ kind: 'appendRow' }], expectedMode: 'idle' },
      { name: 'idle+↓(末行)', state: idle(4, 0), event: key('ArrowDown'), expectedEffects: [{ kind: 'appendRow' }], expectedMode: 'idle' },
      { name: 'editing+Enter', state: editing(0, 0), event: key('Enter'), expectedEffects: [{ kind: 'commit', cell: { row: 0, col: 0 }, nextFocus: { row: 1, col: 0 } }], expectedMode: 'idle' },
      { name: 'editing+Esc', state: editing(0, 0), event: key('Escape'), expectedEffects: [{ kind: 'cancel', cell: { row: 0, col: 0 } }], expectedMode: 'idle' },
      { name: 'editing+Tab', state: editing(0, 0), event: key('Tab'), expectedEffects: [{ kind: 'commit', cell: { row: 0, col: 0 }, nextFocus: { row: 0, col: 1 } }], expectedMode: 'idle' },
      { name: 'editing+Shift+Tab(首列)', state: editing(0, 0), event: key('Tab', { shift: true }), expectedEffects: [{ kind: 'commit', cell: { row: 0, col: 0 }, nextFocus: { row: 0, col: 0 } }], expectedMode: 'idle' },
      { name: 'idle+Delete', state: idle(1, 1), event: key('Delete'), expectedEffects: [{ kind: 'clearCells', rect: { top: 1, left: 1, bottom: 1, right: 1 } }], expectedMode: 'idle' },
      { name: 'idle+Ctrl+C', state: idle(1, 1), event: key('c', { ctrl: true }), expectedEffects: [{ kind: 'clipboard', action: 'copy', rect: { top: 1, left: 1, bottom: 1, right: 1 } }], expectedMode: 'idle' },
      { name: 'idle+Ctrl+X', state: idle(1, 1), event: key('x', { ctrl: true }), expectedEffects: [{ kind: 'clipboard', action: 'cut', rect: { top: 1, left: 1, bottom: 1, right: 1 } }], expectedMode: 'idle' },
      { name: 'idle+Ctrl+V', state: idle(1, 1), event: key('v', { ctrl: true }), expectedEffects: [{ kind: 'clipboard', action: 'paste', rect: { top: 1, left: 1, bottom: 1, right: 1 } }], expectedMode: 'idle' },
      { name: 'idle+Ctrl+Z', state: idle(1, 1), event: key('z', { ctrl: true }), expectedEffects: [{ kind: 'undo' }], expectedMode: 'idle' },
      { name: 'idle+Ctrl+F', state: idle(1, 1), event: key('f', { ctrl: true }), expectedEffects: [{ kind: 'find' }], expectedMode: 'idle' },
      { name: 'idle+Esc', state: idle(1, 1), event: key('Escape'), expectedEffects: [], expectedMode: 'idle' },
    ]
    for (const row of rows) {
      const transition = applyKey(row.state, row.event, BOUNDS)
      expect(transition.effects, row.name).toEqual(row.expectedEffects)
      expect(transition.state.mode, row.name).toBe(row.expectedMode)
    }
  })
})
