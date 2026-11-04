// 演示注册缝测试（票 08）：registerDemoCells 把演示实现接进注册表，未知类型仍命中兜底。
import { describe, expect, it } from 'vitest'

import { createCellRegistry, type CellRegistry } from '../registry'
import { FALLBACK_EDITOR, FALLBACK_RENDERER } from './fallback'
import {
  DEMO_CELL_DEFINITIONS,
  NUMBER_RENDERER,
  SELECT_RENDERER,
  TEXT_EDITOR,
  TEXT_RENDERER,
  registerDemoCells,
} from './index'

function demoRegistry(): CellRegistry {
  const registry = createCellRegistry()
  registerDemoCells(registry)
  return registry
}

describe('registerDemoCells（票 08 演示注册缝）', () => {
  it('注册三个演示渲染器与 text 演示编辑器，按类型键可查回', () => {
    const registry = demoRegistry()

    expect(registry.queryRenderer('text')).toBe(TEXT_RENDERER)
    expect(registry.queryRenderer('number')).toBe(NUMBER_RENDERER)
    expect(registry.queryRenderer('select')).toBe(SELECT_RENDERER)
    expect(registry.queryEditor('text')).toBe(TEXT_EDITOR)
  })

  it('未注册演示的类型仍命中兜底（M1 只交付 3 演示 + 兜底，11 种完整实现为 M2）', () => {
    const registry = demoRegistry()

    expect(registry.queryRenderer('date')).toBe(FALLBACK_RENDERER)
    expect(registry.queryEditor('date')).toBe(FALLBACK_EDITOR)
  })

  it('演示清单范围：3 渲染器 + 1 编辑器', () => {
    expect(Object.keys(DEMO_CELL_DEFINITIONS.renderers).sort()).toEqual(['number', 'select', 'text'])
    expect(Object.keys(DEMO_CELL_DEFINITIONS.editors)).toEqual(['text'])
  })
})
