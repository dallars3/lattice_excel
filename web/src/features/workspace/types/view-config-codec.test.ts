// 视图配置编解码与校验（票 07，纯逻辑接缝）：protojson 真实形状 ↔ 内部模型。
import { describe, expect, it } from 'vitest'

import {
  decodeViewConfig,
  encodeViewConfig,
  viewDataSignature,
} from './view-config-codec'
import {
  completeFilters,
  isFilterComplete,
  MAX_FILTERS,
  MAX_SORTS,
  operatorOptionsFor,
  validateViewConfig,
} from './view-config-validation'
import type { ViewConfig, ViewFilterOp } from './view-config'

const RAW_CONFIG = {
  hidden_field_list: ['fld_1', 'fld_2'],
  frozen_field_list: ['fld_1'],
  filter_list: [
    {
      field_id: 'fld_1',
      op: 'VIEW_FILTER_OP_IN',
      value: {
        list_value: { value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_2' }] },
      },
    },
    { field_id: 'fld_2', op: 'VIEW_FILTER_OP_IS_EMPTY' },
    {
      field_id: 'fld_3',
      op: 'VIEW_FILTER_OP_BETWEEN',
      value: {
        list_value: {
          value_list: [{ int_value: '1' }, { int_value: '5' }],
        },
      },
    },
  ],
  sort_list: [
    { field_id: 'fld_1', dir: 'SORT_DIR_ASC' },
    { field_id: 'fld_2', dir: 'SORT_DIR_DESC' },
  ],
  group_by_list: [{ field_id: 'fld_3', granularity: 'GRANULARITY_MONTH' }],
  row_height: 'ROW_HEIGHT_TALL',
  field_width_map: { fld_1: 200, fld_2: 5000, fld_3: 20 },
}

const TYPED_CONFIG: ViewConfig = {
  hiddenFieldIds: ['fld_2'],
  frozenFieldIds: ['fld_1'],
  filters: [
    { fieldId: 'fld_1', op: 'contains', value: '标题' },
    { fieldId: 'fld_2', op: 'is_empty', value: null },
    {
      fieldId: 'fld_3',
      op: 'in',
      value: ['opt_1', 'opt_2'],
    },
    {
      fieldId: 'fld_4',
      op: 'between',
      value: [1, 1.5],
    },
  ],
  sorts: [
    { fieldId: 'fld_1', dir: 'asc' },
    { fieldId: 'fld_2', dir: 'desc' },
  ],
  group: { fieldId: 'fld_3', granularity: 'week' },
  rowHeight: 'medium',
  fieldWidthMap: { fld_1: 200, fld_2: 100 },
}

describe('decodeViewConfig：protojson → 内部模型', () => {
  it('枚举名归一、common.Value 解码、宽度 clamp 40–2000', () => {
    expect(decodeViewConfig(RAW_CONFIG)).toEqual({
      hiddenFieldIds: ['fld_1', 'fld_2'],
      frozenFieldIds: ['fld_1'],
      filters: [
        { fieldId: 'fld_1', op: 'in', value: ['opt_1', 'opt_2'] },
        { fieldId: 'fld_2', op: 'is_empty', value: null },
        { fieldId: 'fld_3', op: 'between', value: [1, 5] },
      ],
      sorts: [
        { fieldId: 'fld_1', dir: 'asc' },
        { fieldId: 'fld_2', dir: 'desc' },
      ],
      group: { fieldId: 'fld_3', granularity: 'month' },
      rowHeight: 'tall',
      fieldWidthMap: { fld_1: 200, fld_2: 2000, fld_3: 40 },
    })
  })

  it('缺省/空配置按 UI 默认：无分组、中等行高、空条件', () => {
    expect(decodeViewConfig({})).toEqual({
      hiddenFieldIds: [],
      frozenFieldIds: [],
      filters: [],
      sorts: [],
      group: null,
      rowHeight: 'medium',
      fieldWidthMap: {},
    })
  })

  it('日期值 date_value_ms 与 int_value 均解码为毫秒数字', () => {
    const decoded = decodeViewConfig({
      filter_list: [
        { field_id: 'fld_date', op: 'VIEW_FILTER_OP_EQ', value: { date_value_ms: '1730000000000' } },
        {
          field_id: 'fld_date',
          op: 'VIEW_FILTER_OP_BETWEEN',
          value: {
            list_value: {
              value_list: [{ date_value_ms: '1730000000000' }, { int_value: '1735000000000' }],
            },
          },
        },
      ],
    })
    expect(decoded.filters.map((filter) => filter.value)).toEqual([
      1730000000000,
      [1730000000000, 1735000000000],
    ])
  })
})

describe('encodeViewConfig：内部模型 → protojson', () => {
  it('反向枚举名、common.Value 包装（字符串/整数/小数/数组）、空操作省略 value', () => {
    expect(encodeViewConfig(TYPED_CONFIG)).toEqual({
      hidden_field_list: ['fld_2'],
      frozen_field_list: ['fld_1'],
      filter_list: [
        { field_id: 'fld_1', op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: '标题' } },
        { field_id: 'fld_2', op: 'VIEW_FILTER_OP_IS_EMPTY' },
        {
          field_id: 'fld_3',
          op: 'VIEW_FILTER_OP_IN',
          value: {
            list_value: {
              value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_2' }],
            },
          },
        },
        {
          field_id: 'fld_4',
          op: 'VIEW_FILTER_OP_BETWEEN',
          value: {
            list_value: {
              value_list: [{ int_value: '1' }, { double_value: 1.5 }],
            },
          },
        },
      ],
      sort_list: [
        { field_id: 'fld_1', dir: 'SORT_DIR_ASC' },
        { field_id: 'fld_2', dir: 'SORT_DIR_DESC' },
      ],
      group_by_list: [{ field_id: 'fld_3', granularity: 'GRANULARITY_WEEK' }],
      row_height: 'ROW_HEIGHT_MEDIUM',
      field_width_map: { fld_1: 200, fld_2: 100 },
    })
  })

  it('无分组时 group_by_list 为空数组；无粒度时省略 granularity', () => {
    const encoded = encodeViewConfig({
      ...TYPED_CONFIG,
      group: { fieldId: 'fld_3' },
    })
    expect(encoded.group_by_list).toEqual([{ field_id: 'fld_3' }])

    const noGroup = encodeViewConfig({ ...TYPED_CONFIG, group: null })
    expect(noGroup.group_by_list).toEqual([])
  })

  it('范围外宽度在编码时也 clamp 到 40–2000', () => {
    const encoded = encodeViewConfig({
      ...TYPED_CONFIG,
      fieldWidthMap: { fld_1: 0, fld_2: 4000 },
    })
    expect(encoded.field_width_map).toEqual({ fld_1: 40, fld_2: 2000 })
  })

  it('丢弃未填完的筛选条件：不把 value=null 下发为后端必判 false 的条件', () => {
    const encoded = encodeViewConfig({
      ...TYPED_CONFIG,
      filters: [
        { fieldId: 'fld_1', op: 'contains', value: '标题' },
        { fieldId: 'fld_2', op: 'contains', value: null },
        { fieldId: 'fld_3', op: 'between', value: [1, null] },
        { fieldId: 'fld_4', op: 'in', value: [] },
        { fieldId: 'fld_5', op: 'is_empty', value: null },
      ],
    })
    expect(encoded.filter_list).toEqual([
      { field_id: 'fld_1', op: 'VIEW_FILTER_OP_CONTAINS', value: { string_value: '标题' } },
      { field_id: 'fld_5', op: 'VIEW_FILTER_OP_IS_EMPTY' },
    ])
  })
})

describe('数据条件签名', () => {
  it('无筛选/排序/分组与配置未加载共用空签名', () => {
    expect(viewDataSignature(null)).toBe('')
    expect(viewDataSignature(decodeViewConfig({}))).toBe('')
  })

  it('筛选/排序/分组变更改变签名，宽度/行高/显隐不改变', () => {
    const base = viewDataSignature(TYPED_CONFIG)
    expect(viewDataSignature({ ...TYPED_CONFIG, fieldWidthMap: { fld_1: 300 } })).toBe(base)
    expect(viewDataSignature({ ...TYPED_CONFIG, rowHeight: 'compact' })).toBe(base)
    expect(viewDataSignature({ ...TYPED_CONFIG, hiddenFieldIds: [] })).toBe(base)

    expect(
      viewDataSignature({
        ...TYPED_CONFIG,
        filters: [{ fieldId: 'fld_1', op: 'contains', value: '新标题' }],
      }),
    ).not.toBe(base)
    expect(
      viewDataSignature({
        ...TYPED_CONFIG,
        sorts: [{ fieldId: 'fld_1', dir: 'desc' }],
      }),
    ).not.toBe(base)
    expect(viewDataSignature({ ...TYPED_CONFIG, group: null })).not.toBe(base)
  })

  it('未填完的筛选条件不影响签名（仅完整条件参与数据查询 key）', () => {
    const base = viewDataSignature(TYPED_CONFIG)
    expect(
      viewDataSignature({
        ...TYPED_CONFIG,
        filters: [...TYPED_CONFIG.filters, { fieldId: 'fld_x', op: 'contains', value: null }],
      }),
    ).toBe(base)
    expect(
      viewDataSignature({
        ...TYPED_CONFIG,
        filters: [...TYPED_CONFIG.filters, { fieldId: 'fld_x', op: 'in', value: [] }],
      }),
    ).toBe(base)
    // 只有未完成条件时与空配置共用空签名。
    expect(
      viewDataSignature({
        ...TYPED_CONFIG,
        filters: [{ fieldId: 'fld_x', op: 'contains', value: '   ' }],
        sorts: [],
        group: null,
      }),
    ).toBe('')
  })
})

describe('筛选完整性（isFilterComplete / completeFilters）', () => {
  it.each([
    { op: 'is_empty', value: null, want: true },
    { op: 'not_empty', value: null, want: true },
    { op: 'today', value: null, want: true },
    { op: 'has', value: null, want: true },
    { op: 'none', value: null, want: true },
    { op: 'contains', value: 'abc', want: true },
    { op: 'contains', value: '   ', want: false },
    { op: 'contains', value: null, want: false },
    { op: 'eq', value: 0, want: true },
    { op: 'between', value: [1, 2], want: true },
    { op: 'between', value: [1, null], want: false },
    { op: 'between', value: [1], want: false },
    { op: 'in', value: ['opt_1'], want: true },
    { op: 'in', value: [], want: false },
    { op: 'in', value: [''], want: false },
    { op: 'contains_any', value: [' '], want: false },
  ] as const)('$op 值 $value → $want', ({ op, value, want }) => {
    expect(
      isFilterComplete({ fieldId: 'fld_1', op: op as ViewFilterOp, value }),
    ).toBe(want)
  })

  it('completeFilters 只保留完整条件且保持原顺序', () => {
    const complete = completeFilters([
      { fieldId: 'fld_1', op: 'contains', value: 'a' },
      { fieldId: 'fld_2', op: 'contains', value: null },
      { fieldId: 'fld_3', op: 'is_empty', value: null },
    ])
    expect(complete.map((filter) => filter.fieldId)).toEqual(['fld_1', 'fld_3'])
  })
})

describe('操作符白名单（接口文档 8.0.5）', () => {
    it('按字段类型给出白名单，lookup 不提供筛选', () => {
    expect(operatorOptionsFor('text')).toEqual(['contains', 'not_contains', 'is_empty', 'not_empty'])
    expect(operatorOptionsFor('number')).toEqual(['eq', 'ne', 'gt', 'lt', 'between', 'is_empty'])
    expect(operatorOptionsFor('select')).toEqual(['in', 'not_in', 'is_empty'])
    expect(operatorOptionsFor('multiselect')).toEqual(['contains_any', 'contains_all', 'is_empty'])
    expect(operatorOptionsFor('date')).toEqual([
      'eq',
      'before',
      'after',
      'between',
      'today',
      'is_empty',
    ])
    expect(operatorOptionsFor('person')).toEqual(['contains', 'is_empty'])
    expect(operatorOptionsFor('attachment')).toEqual(['has', 'none'])
    expect(operatorOptionsFor('relation')).toEqual(['has', 'none'])
    expect(operatorOptionsFor('formula')).toEqual(['eq', 'ne', 'gt', 'lt', 'is_empty'])
    expect(operatorOptionsFor('lookup')).toEqual([])
  })
})

describe('validateViewConfig：上限校验', () => {
  it(`筛选超过 ${MAX_FILTERS} 条、排序超过 ${MAX_SORTS} 级返回错误`, () => {
    const tooManyFilters: ViewConfig = {
      ...TYPED_CONFIG,
      filters: Array.from({ length: MAX_FILTERS + 1 }, (_, index) => ({
        fieldId: `fld_${index}`,
        op: 'contains' as const,
        value: 'x',
      })),
      sorts: [],
    }
    expect(validateViewConfig(tooManyFilters)).toContain('筛选条件最多 5 条')

    const tooManySorts: ViewConfig = {
      ...TYPED_CONFIG,
      filters: [],
      sorts: Array.from({ length: MAX_SORTS + 1 }, (_, index) => ({
        fieldId: `fld_${index}`,
        dir: 'asc' as const,
      })),
    }
    expect(validateViewConfig(tooManySorts)).toContain('排序最多 3 级')
  })

  it('合法配置无错误', () => {
    expect(validateViewConfig({ ...TYPED_CONFIG, filters: TYPED_CONFIG.filters.slice(0, 5), sorts: TYPED_CONFIG.sorts.slice(0, 3) })).toEqual([])
  })
})
