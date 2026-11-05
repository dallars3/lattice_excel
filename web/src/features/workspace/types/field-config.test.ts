import { describe, expect, it } from 'vitest'

import {
  ALL_FIELD_TYPES,
  FIELD_TYPE_OPTIONS,
  OPTION_COLOR_PALETTE,
  PRIMARY_FIELD_TYPES,
  allowedConvertTargets,
  defaultFieldConfig,
  isConvertExcludedType,
  isOptionFieldType,
  nextOptionColor,
  resolveOptionEntry,
} from './field-config'

describe('字段类型与配置（接口文档 §8.0.4 / §8.5.5）', () => {
  it('11 种字段类型按固定顺序展示，标签唯一', () => {
    expect(ALL_FIELD_TYPES).toEqual([
      'text',
      'number',
      'select',
      'multiselect',
      'date',
      'person',
      'attachment',
      'url',
      'formula',
      'relation',
      'lookup',
    ])
    expect(FIELD_TYPE_OPTIONS.map((option) => option.type)).toEqual(ALL_FIELD_TYPES)
    expect(new Set(FIELD_TYPE_OPTIONS.map((option) => option.label)).size).toBe(11)
  })

  it('各类型默认配置符合 §8.0.4', () => {
    expect(defaultFieldConfig('text')).toEqual({})
    expect(defaultFieldConfig('attachment')).toEqual({})
    expect(defaultFieldConfig('url')).toEqual({})
    expect(defaultFieldConfig('number')).toEqual({
      format: 'int',
      decimals: 0,
      currency: '¥',
      thousands: false,
    })
    expect(defaultFieldConfig('select')).toEqual({ option_list: [], allow_create: false })
    expect(defaultFieldConfig('multiselect')).toEqual({ option_list: [], allow_create: false })
    expect(defaultFieldConfig('date')).toEqual({ include_time: false, format: 'YYYY-MM-DD' })
    expect(defaultFieldConfig('person')).toEqual({ allow_multiple: false })
    expect(defaultFieldConfig('formula')).toEqual({ expression: '' })
    expect(defaultFieldConfig('relation')).toEqual({ target_table_id: '', allow_multiple: false })
    expect(defaultFieldConfig('lookup')).toEqual({ relation_field_id: '', target_field_id: '' })
  })

  it('主字段仅允许 多行文本/数字/单选/公式', () => {
    expect(PRIMARY_FIELD_TYPES).toEqual(['text', 'number', 'select', 'formula'])
  })

  it('公式/关联/引用三类不参与任何方向的类型转换', () => {
    expect(isConvertExcludedType('formula')).toBe(true)
    expect(isConvertExcludedType('relation')).toBe(true)
    expect(isConvertExcludedType('lookup')).toBe(true)
    expect(isConvertExcludedType('text')).toBe(false)

    for (const type of ALL_FIELD_TYPES) {
      const targets = allowedConvertTargets(type)
      expect(targets).not.toContain('formula')
      expect(targets).not.toContain('relation')
      expect(targets).not.toContain('lookup')
      expect(targets).not.toContain(type)
    }
  })

  it('转换目标按 §8.5.5 规则给出', () => {
    expect(allowedConvertTargets('text')).toEqual(['number', 'select', 'multiselect', 'date'])
    expect(allowedConvertTargets('number')).toEqual(['text'])
    expect(allowedConvertTargets('select')).toEqual(['multiselect', 'text'])
    expect(allowedConvertTargets('multiselect')).toEqual(['select', 'text'])
    expect(allowedConvertTargets('date')).toEqual(['text'])
    expect(allowedConvertTargets('person')).toEqual(['text'])
    expect(allowedConvertTargets('attachment')).toEqual(['text'])
    expect(allowedConvertTargets('url')).toEqual(['text'])
    expect(allowedConvertTargets('formula')).toEqual([])
    expect(allowedConvertTargets('relation')).toEqual([])
    expect(allowedConvertTargets('lookup')).toEqual([])
  })

  it('仅单选/多选支持选项维护', () => {
    expect(isOptionFieldType('select')).toBe(true)
    expect(isOptionFieldType('multiselect')).toBe(true)
    expect(isOptionFieldType('text')).toBe(false)
  })
})

describe('选项录入裁决 resolveOptionEntry（§4.3 / D14）', () => {
  const options = [
    { id: 'opt_1', name: '进行中', color: 'blue' },
    { id: 'opt_2', name: '已完成', color: 'green' },
  ]

  it('匹配已有选项：去首尾空格并忽略大小写', () => {
    expect(resolveOptionEntry(false, options, '  进行中 ')).toMatchObject({
      matched: { id: 'opt_1', name: '进行中' },
      created: null,
      message: null,
    })
  })

  it('allow_create 关闭且无匹配：返回提示且不可新建', () => {
    expect(resolveOptionEntry(false, options, '已归档')).toEqual({
      matched: null,
      created: null,
      message: '无匹配选项，请联系表格管理员添加',
    })
  })

  it('allow_create 开启且无匹配：按调色板自动分配颜色新建', () => {
    const result = resolveOptionEntry(true, options, '已归档')
    expect(result.matched).toBeNull()
    expect(result.message).toBeNull()
    expect(result.created).toMatchObject({ name: '已归档', id: '' })
    expect(result.created?.color).toBe('orange')
  })

  it('空输入不产生匹配/新建/提示', () => {
    expect(resolveOptionEntry(true, options, '   ')).toEqual({
      matched: null,
      created: null,
      message: null,
    })
  })

  it('调色板按顺序分配并循环复用', () => {
    expect(nextOptionColor([])).toBe('blue')
    expect(nextOptionColor(['blue'])).toBe('green')
    expect(nextOptionColor(OPTION_COLOR_PALETTE.slice(0, 9))).toBe('brown')
    expect(nextOptionColor(OPTION_COLOR_PALETTE)).toBe('blue')
  })
})
