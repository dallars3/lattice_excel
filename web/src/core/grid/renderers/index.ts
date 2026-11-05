// 渲染器/编辑器实现门面（票 08）。M2 按 11 种字段类型矩阵（需求 4.3）扩展真实实现时，
// 以本目录为注册实现落点：各字段类型一个文件，导出 RENDERER/EDITOR 定义对并加入清单。
import type { CellEditorDef, CellRegistry, CellRendererDef } from '../registry'
import { ATTACHMENT_RENDERER } from './attachment'
import { DATE_RENDERER } from './date'
import { FORMULA_RENDERER } from './formula'
import { LOOKUP_RENDERER } from './lookup'
import { MULTISELECT_RENDERER } from './multiselect'
import { NUMBER_RENDERER } from './number'
import { PERSON_RENDERER } from './person'
import { RELATION_RENDERER } from './relation'
import { SELECT_RENDERER } from './select'
import { TEXT_EDITOR, TEXT_RENDERER } from './text'
import { URL_RENDERER } from './url'

export {
  OPTION_COLOR_VALUES,
  SELECT_RENDERER,
  optionColorValue,
  resolveSelectOptions,
  type RawSelectOption,
  type SelectCellConfig,
  type SelectOption,
} from './select'
export { MULTISELECT_RENDERER } from './multiselect'
export { DATE_RENDERER } from './date'
export { formatDate, type DateCellConfig, type DateFormat } from './format-date'
export { PERSON_RENDERER, type PersonCellConfig, type PersonInfo } from './person'
export {
  ATTACHMENT_RENDERER,
  type AttachmentCellConfig,
  type AttachmentFileInfo,
} from './attachment'
export { URL_RENDERER, parseUrlValue, type UrlCellValue } from './url'
export {
  FORMULA_ERROR_SENTINEL,
  FORMULA_PLACEHOLDER,
  FORMULA_RENDERER,
  parseFormulaValue,
  type FormulaDisplay,
} from './formula'
export { RELATION_RENDERER, type RelationCellConfig } from './relation'
export { LOOKUP_RENDERER, type LookupCellConfig } from './lookup'
export {
  formatNumber,
  type NumberCellConfig,
  type NumberFormat,
} from './format-number'
export { NUMBER_RENDERER } from './number'
export { FALLBACK_EDITOR, FALLBACK_RENDERER, fallbackCellText } from './fallback'
export { TEXT_EDITOR, TEXT_LIFECYCLE, TEXT_RENDERER, type TextCellConfig } from './text'
export {
  DATE_EDITOR,
  M2_EDITORS,
  MULTISELECT_EDITOR,
  NUMBER_EDITOR,
  PERSON_EDITOR,
  RELATION_EDITOR,
  SELECT_EDITOR,
  TEXT_EDITOR_M2,
  URL_EDITOR,
  registerM2Editors,
  type CreateOptionHandler,
} from './editors'
export {
  MIN_DATE_MS,
  matchOptionId,
  normalizeUrlInput,
  parseDateInput,
  parseNumberDraft,
} from './editor-values'

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

/** M2 11 类单元格渲染器清单（编辑器留票 06；键为接口文档字段类型小写枚举名）。 */
export interface M2CellDefinitions {
  readonly renderers: Readonly<Record<string, CellRendererDef>>
  readonly editors: Readonly<Record<string, CellEditorDef>>
}

export const M2_CELL_DEFINITIONS: M2CellDefinitions = {
  renderers: {
    text: TEXT_RENDERER,
    number: NUMBER_RENDERER,
    select: SELECT_RENDERER,
    multiselect: MULTISELECT_RENDERER,
    date: DATE_RENDERER,
    person: PERSON_RENDERER,
    attachment: ATTACHMENT_RENDERER,
    url: URL_RENDERER,
    formula: FORMULA_RENDERER,
    relation: RELATION_RENDERER,
    lookup: LOOKUP_RENDERER,
  },
  editors: {},
}

/** 注册 M2 11 类渲染器（不改变注册表语义：同类型覆盖、未知类型仍走兜底）。 */
export function registerM2Cells(registry: CellRegistry): void {
  for (const [fieldType, def] of Object.entries(M2_CELL_DEFINITIONS.renderers)) {
    registry.registerRenderer(fieldType, def)
  }
  for (const [fieldType, def] of Object.entries(M2_CELL_DEFINITIONS.editors)) {
    registry.registerEditor(fieldType, def)
  }
}
