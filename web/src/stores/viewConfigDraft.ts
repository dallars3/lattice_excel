import { create } from 'zustand'

/** viewConfigDraftStore（技术方案第 8 章）：视图配置面板草稿（提交前）。 */
export interface ViewConfigDraftState {
  /** 草稿相对已保存配置是否被改动 */
  readonly isDirty: boolean
  /** 草稿本体（ViewConfig 局部形状随 M2 工具栏落地） */
  readonly draft: Record<string, unknown> | null
}

/** 空骨架（票 02）：仅初始状态供任意组件消费；草稿编辑与提交随 M2 视图配置面板落地。 */
export const viewConfigDraftStore = create<ViewConfigDraftState>()(() => ({
  isDirty: false,
  draft: null,
}))
