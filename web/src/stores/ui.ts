import { create } from 'zustand'

/** uiStore（技术方案第 8 章）：横幅/Toast/Modal 栈、删除撤销 7s 计时器。 */
export interface UiState {
  /** 全局横幅文案（非空时渲染） */
  readonly bannerMessage: string | null
  /** Modal 栈（栈顶最后入栈，后到先关） */
  readonly modalStack: readonly string[]
  /** 删除撤销窗口截止时间戳（ms；null 表示无撤销窗口） */
  readonly undoDeadlineAt: number | null
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；Toast/Modal 入栈与 7s 撤销计时随 M2 全局弹层落地。 */
export const uiStore = create<UiState>()(() => ({
  bannerMessage: null,
  modalStack: [],
  undoDeadlineAt: null,
}))
