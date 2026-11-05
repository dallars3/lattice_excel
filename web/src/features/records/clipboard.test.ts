// TSV 剪贴板纯逻辑（票 06 / AC-REC-04/05）：选区序列化 + 单值/单行粘贴计划与计数。
import { describe, expect, it } from 'vitest'

import { RECORD_ROW_LIMIT, isRecordCapacityReached } from './capacity'
import {
  buildPastePlan,
  buildPasteSummary,
  parseTsv,
  serializeCellValue,
  serializeSelection,
  type ClipboardField,
} from './clipboard'
import { editHistoryStore } from './edit-history'

const DATE_MS = new Date(2024, 0, 5).getTime()

function field(
  id: string,
  type: string,
  configMap: Record<string, unknown> = {},
): ClipboardField {
  return { id, name: id, type, configMap }
}

describe('serializeCellValue', () => {
  it.each([
    { field: field('f', 'text'), value: '原文', expected: '原文' },
    { field: field('f', 'number'), value: 0, expected: '0' },
    { field: field('f', 'number'), value: 12.5, expected: '12.5' },
    {
      field: field('f', 'select', {
        option_list: [{ id: 'opt_1', name: '进行中', color: 'blue' }],
      }),
      value: 'opt_1',
      expected: '进行中',
    },
    {
      field: field('f', 'multiselect', {
        option_list: [
          { id: 'opt_1', name: '进行中' },
          { id: 'opt_2', name: '完成' },
        ],
      }),
      value: ['opt_1', 'opt_2'],
      expected: '进行中,完成',
    },
    {
      field: field('f', 'date', { format: 'YYYY-MM-DD' }),
      value: DATE_MS,
      expected: '2024-01-05',
    },
    {
      field: field('f', 'person'),
      value: 'usr_1',
      expected: '张三',
    },
    { field: field('f', 'attachment'), value: ['fil_1'], expected: 'fil_1' },
    {
      field: field('f', 'url'),
      value: { url: 'https://a.com', display_text: '官网' },
      expected: '官网',
    },
    { field: field('f', 'formula'), value: 126, expected: '126' },
    {
      field: field('f', 'relation'),
      value: ['rec_9'],
      expected: '客户甲',
    },
    { field: field('f', 'lookup'), value: '引用值', expected: '引用值' },
    { field: field('f', 'text'), value: null, expected: '' },
  ])('$field.type 值 $value → "$expected"', ({ field: f, value, expected }) => {
    const extras = {
      personDirectory: { usr_1: { name: '张三' } },
      relationLabels: { rec_9: '客户甲' },
    }
    expect(serializeCellValue(f, value, extras)).toBe(expected)
  })
})

describe('serializeSelection / parseTsv', () => {
  it('选区按制表符与换行输出（单元格/区域/整行同口径）', () => {
    const fields = [field('fld_a', 'text'), field('fld_b', 'number'), field('fld_c', 'text')]
    const rows = [
      { cellsMap: { fld_a: 'A1', fld_b: 1, fld_c: 'C1' } },
      { cellsMap: { fld_a: 'A2', fld_b: 2, fld_c: 'C2' } },
    ]
    expect(
      serializeSelection({
        rows,
        fields,
        rect: { top: 0, left: 0, bottom: 1, right: 2 },
      }),
    ).toBe('A1\t1\tC1\nA2\t2\tC2')

    expect(
      serializeSelection({ rows, fields, rect: { top: 0, left: 1, bottom: 0, right: 1 } }),
    ).toBe('1')
  })

  it('解析 TSV：兼容 CRLF、保留行内制表符拆分、去尾部空行', () => {
    expect(parseTsv('a\tb\r\nc\td\r\n')).toEqual([
      ['a', 'b'],
      ['c', 'd'],
    ])
    expect(parseTsv('单值')).toEqual([['单值']])
    expect(parseTsv('')).toEqual([])
  })
})

describe('buildPastePlan', () => {
  const fields = [
    field('fld_text', 'text'),
    field('fld_formula', 'formula'),
    field('fld_select', 'select', {
      option_list: [{ id: 'opt_1', name: '进行中' }],
      allow_create: false,
    }),
    field('fld_num', 'number'),
    field('fld_date', 'date', { include_time: false }),
    field('fld_person', 'person', { allow_multiple: true }),
  ]

  it('单值粘贴：从起始列映射；只读列跳过、选项不匹配置空分别计数', () => {
    const plan = buildPastePlan({
      parsed: [['你好', '=SUM()', '暂停']],
      fields,
      startCol: 0,
      extras: { personDirectory: { usr_1: { name: '张三' } } },
    })

    expect(plan.unsupported).toBe(false)
    expect(plan.skippedReadonly).toBe(1)
    expect(plan.optionUnmatched).toBe(1)
    expect(plan.operations).toEqual([
      { fieldId: 'fld_text', value: '你好', expectVersion: 0 },
      { fieldId: 'fld_select', value: null, expectVersion: 0 },
    ])
  })

  it('单行粘贴跨多个字段：数字/日期/人员按类型换算', () => {
    const plan = buildPastePlan({
      parsed: [['7', '2024-01-05', '张三,李四']],
      fields: [fields[3]!, fields[4]!, fields[5]!],
      startCol: 0,
      extras: {
        personDirectory: { usr_1: { name: '张三' }, usr_2: { name: '李四' } },
      },
    })

    expect(plan.operations).toEqual([
      { fieldId: 'fld_num', value: 7, expectVersion: 0 },
      { fieldId: 'fld_date', value: new Date(2024, 0, 5).getTime(), expectVersion: 0 },
      { fieldId: 'fld_person', value: ['usr_1', 'usr_2'], expectVersion: 0 },
    ])
    expect(plan.invalidCount).toBe(0)
  })

  it('二维区域粘贴按 G-5 不支持（不产生写操作）', () => {
    const plan = buildPastePlan({ parsed: [['a'], ['b']], fields, startCol: 0 })
    expect(plan.unsupported).toBe(true)
    expect(plan.operations).toEqual([])
    expect(buildPasteSummary(plan)).toBe('当前仅支持单值或单行粘贴')
  })

  it('提示文案包含写入数、只读跳过、选项不匹配与服务端跳过/失败', () => {
    const plan = buildPastePlan({
      parsed: [['你好', '=SUM()', '暂停']],
      fields,
      startCol: 0,
    })
    expect(buildPasteSummary(plan, 1, 0)).toBe(
      '已粘贴 2 个单元格；跳过只读列 1 个；选项不匹配置空 1 个；服务端跳过 1 项',
    )
    expect(buildPasteSummary(plan, 0, 1)).toContain('1 项写入失败')
  })
})

describe('编辑撤销栈（会话内）', () => {
  it('push/peek 取最近一步、drop 后回退到上一步', () => {
    editHistoryStore.getState().clear()
    editHistoryStore.getState().push({
      baseId: 'base_1',
      recordId: 'rec_1',
      fieldId: 'fld_a',
      before: 'old1',
      after: 'new1',
      versionAtEdit: 1,
    })
    editHistoryStore.getState().push({
      baseId: 'base_1',
      recordId: 'rec_1',
      fieldId: 'fld_b',
      before: 'old2',
      after: 'new2',
      versionAtEdit: 2,
    })
    editHistoryStore.getState().push({
      baseId: 'base_2',
      recordId: 'rec_2',
      fieldId: 'fld_a',
      before: 'other',
      after: 'other-new',
      versionAtEdit: 1,
    })

    expect(editHistoryStore.getState().peek('base_1')?.fieldId).toBe('fld_b')
    const entry = editHistoryStore.getState().peek('base_1')
    editHistoryStore.getState().drop(entry!.id)
    expect(editHistoryStore.getState().peek('base_1')?.fieldId).toBe('fld_a')
    expect(editHistoryStore.getState().peek('base_missing')).toBeNull()
    editHistoryStore.getState().clear()
    expect(editHistoryStore.getState().entries).toEqual([])
  })
})

describe('单表 5 万行上限', () => {
  it('已加载达到上限时判为已达上限', () => {
    expect(isRecordCapacityReached(RECORD_ROW_LIMIT - 1, true)).toBe(false)
    expect(isRecordCapacityReached(RECORD_ROW_LIMIT, false)).toBe(true)
    expect(isRecordCapacityReached(RECORD_ROW_LIMIT + 1, false)).toBe(true)
  })
})
