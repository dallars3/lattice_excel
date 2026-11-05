// cell-codec 表驱动测试（票 05）：H-48 响应的 common.Value protojson 包装解码、
// 原始值/数组/对象兼容、空值 null 与数字 0 的严格区分；请求编码输出普通 JSON。
import { describe, expect, it } from 'vitest'

import { decodeCellValue, decodeCellsMap, encodeCellValue } from './cell-codec'

describe('decodeCellValue（common.Value protojson → 普通值）', () => {
  it.each([
    { raw: null, expected: null, note: '空值统一 null' },
    { raw: undefined, expected: null },
    { raw: '', expected: '' },
    { raw: '文本', expected: '文本' },
    { raw: 0, expected: 0, note: '数字 0 与 null 区分' },
    { raw: 12.5, expected: 12.5 },
    { raw: false, expected: false },
    { raw: { string_value: '任务说明' }, expected: '任务说明' },
    { raw: { string_value: '' }, expected: '' },
    { raw: { int_value: '0' }, expected: 0, note: 'int64 字符串 → number，0 保持 0' },
    { raw: { int_value: '-12' }, expected: -12 },
    { raw: { int_value: 7 }, expected: 7 },
    { raw: { double_value: 1.5 }, expected: 1.5 },
    { raw: { double_value: '2.25' }, expected: 2.25 },
    { raw: { bool_value: true }, expected: true },
    { raw: { date_value_ms: '1730000000000' }, expected: 1730000000000 },
    { raw: { date_value_ms: 1730000000000 }, expected: 1730000000000 },
    { raw: { list_value: { value_list: [] } }, expected: [] },
    {
      raw: { list_value: { value_list: [{ string_value: 'opt_1' }, { string_value: 'opt_3' }] } },
      expected: ['opt_1', 'opt_3'],
    },
    {
      raw: { list_value: { value_list: [{ int_value: '1' }, { double_value: 2.5 }] } },
      expected: [1, 2.5],
    },
    { raw: ['opt_1', 'opt_3'], expected: ['opt_1', 'opt_3'], note: '原始数组递归解码' },
    {
      raw: [{ int_value: '9' }, { string_value: 's' }],
      expected: [9, 's'],
    },
    {
      raw: { url: 'https://a.com', display_text: '官网' },
      expected: { url: 'https://a.com', display_text: '官网' },
      note: 'URL 对象不是 Value 包装，原样保留',
    },
    { raw: {}, expected: null, note: '空 Value（oneof 未设置）按空值处理' },
  ])('raw=$raw → $expected', ({ raw, expected }) => {
    expect(decodeCellValue(raw)).toEqual(expected)
  })
})

describe('decodeCellsMap（field_id → common.Value）', () => {
  it('逐字段解码，空值字段保留 null；缺省整体返回空对象', () => {
    const cells = decodeCellsMap({
      fld_text: { string_value: '标题' },
      fld_number: { int_value: '0' },
      fld_empty: null,
      fld_list: { list_value: { value_list: [{ string_value: 'a' }, { string_value: 'b' }] } },
    })

    expect(cells).toEqual({
      fld_text: '标题',
      fld_number: 0,
      fld_empty: null,
      fld_list: ['a', 'b'],
    })
    expect(decodeCellsMap(undefined)).toEqual({})
    expect(decodeCellsMap(null)).toEqual({})
  })
})

describe('encodeCellValue（普通值 → 请求 JSON）', () => {
  it.each([
    { plain: null, expected: null },
    { plain: '', expected: '' },
    { plain: '文本', expected: '文本' },
    { plain: 0, expected: 0 },
    { plain: 1730000000000, expected: 1730000000000, note: '日期 ms 数字直传' },
    { plain: false, expected: false },
    { plain: ['opt_1', 'opt_3'], expected: ['opt_1', 'opt_3'] },
    {
      plain: { url: 'https://a.com', display_text: '官网' },
      expected: { url: 'https://a.com', display_text: '官网' },
    },
  ])('plain=$plain → $expected', ({ plain, expected }) => {
    expect(encodeCellValue(plain)).toEqual(expected)
  })
})
