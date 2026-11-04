// 渲染器/编辑器实现门面（票 08）。M2 按 11 种字段类型矩阵（需求 4.3）扩展真实实现时，
// 以本目录为注册实现落点：各字段类型一个文件，导出 RENDERER/EDITOR 定义对并加入清单。
import type { CellEditorDef, CellRegistry, CellRendererDef } from '../registry'
import { NUMBER_RENDERER } from './number'
import { SELECT_RENDERER } from './select'
import { TEXT_EDITOR, TEXT_RENDERER } from './text'

export { FALLBACK_EDITOR, FALLBACK_RENDERER, fallbackCellText } from './fallback'
export { formatNumber, type NumberCellConfig } from './format-number'
export { NUMBER_RENDERER } from './number'
export { SELECT_RENDERER, type SelectCellConfig, type SelectOption } from './select'
export { TEXT_EDITOR, TEXT_LIFECYCLE, TEXT_RENDERER, type TextCellConfig } from './text'

/** M1 演示定义清单（3 渲染器 + 1 编辑器；11 种字段类型完整实现为 M2，spec Out of Scope）。 */
export interface DemoCellDefinitions {
  readonly renderers: Readonly<Record<string, CellRendererDef>>
  readonly editors: Readonly<Record<string, CellEditorDef>>
}

export const DEMO_CELL_DEFINITIONS: DemoCellDefinitions = {
  renderers: { text: TEXT_RENDERER, number: NUMBER_RENDERER, select: SELECT_RENDERER },
  editors: { text: TEXT_EDITOR },
}

/** 把 M1 演示实现注册进注册表（票 09 演示页消费；M2 换真实实现时按同类型键覆盖注册即可）。 */
export function registerDemoCells(registry: CellRegistry): void {
  for (const [fieldType, def] of Object.entries(DEMO_CELL_DEFINITIONS.renderers)) {
    registry.registerRenderer(fieldType, def)
  }
  for (const [fieldType, def] of Object.entries(DEMO_CELL_DEFINITIONS.editors)) {
    registry.registerEditor(fieldType, def)
  }
}
