// grid 核心门面（票 07 转正；技术方案第 4 章 core/grid 划分）。
// 窗口计算/行高校准/冻结双通道/loadedRanges/SelectionModel/键位状态机，全部 DOM 无关纯逻辑；
// CellRenderer/CellEditor 注册表随票 08 在本目录落地。
export type { CellRef, GridBounds } from './common'
export { clampCell } from './common'
export {
  DEFAULT_OVERSCAN,
  FRAME_BUDGET_MS,
  buildPrefixOffsets,
  computeWindow,
  type ComputeWindowInput,
  type WindowResult,
  type WindowSection,
} from './windowing'
export {
  ROW_HEIGHT_ESTIMATE_PX,
  ROW_HEIGHT_TIERS,
  RowHeightModel,
  type RowHeightModelOptions,
  type RowHeightTier,
} from './row-height'
export {
  computeColumnLayout,
  type ColumnLayout,
  type ComputeColumnLayoutInput,
  type PositionedColumn,
} from './frozen-columns'
export {
  DEFAULT_LOADED_RANGES_CAPACITY,
  LoadedRangeCache,
  type GridPage,
  type LoadedRangeCacheOptions,
} from './loaded-ranges'
export {
  SELECTION_KINDS,
  beginRange,
  cellCount,
  containsCell,
  extendRange,
  selectAll,
  selectCell,
  selectRow,
  selectionRect,
  type Selection,
  type SelectionKind,
  type SelectionOrNull,
  type SelectionRect,
} from './selection'
export {
  GRID_EDIT_MODES,
  GRID_KEYS,
  applyKey,
  initialKeyboardState,
  requestEdit,
  withSelection,
  type GridEditMode,
  type GridKey,
  type GridKeyEffect,
  type GridKeyEvent,
  type KeyboardState,
  type KeyTransition,
} from './keyboard'
