// 分组呈现纯逻辑（票 07 / N1）：组名取选项名/人员名/日期桶/「空」，组头不携带记录数。
import { describe, expect, it } from 'vitest'

import type { RecordRow } from '@/features/records/api'
import type { FieldRecord } from './api/field'
import { buildViewRows, groupKeyForValue, groupLabelForValue } from './view-grouping'

function field(overrides: Partial<FieldRecord>): FieldRecord {
  return {
    id: 'fld_status',
    tableId: 'tbl_1',
    name: '状态',
    type: 'select',
    configMap: {
      option_list: [
        { id: 'opt_1', name: '进行中', color: 'blue' },
        { id: 'opt_2', name: '完成', color: 'green' },
      ],
    },
    isPrimary: false,
    position: 1,
    description: '',
    ...overrides,
  }
}

function row(id: string, value: unknown): RecordRow {
  return {
    id,
    tableId: 'tbl_1',
    cellsMap: { fld_status: value },
    createdBy: 'usr_1',
    createdAt: 0,
    version: 1,
  }
}

const PERSON_DIRECTORY = {
  usr_1: { name: 'Alice' },
  usr_2: { name: 'Bob' },
}

describe('groupKeyForValue / groupLabelForValue', () => {
  it('单选取选项名；空值归入「空」', () => {
    const status = field({})
    expect(groupLabelForValue('opt_1', status, undefined, {})).toBe('进行中')
    expect(groupKeyForValue('opt_1', status, undefined)).toBe('opt_1')
    expect(groupLabelForValue(null, status, undefined, {})).toBe('空')
    expect(groupKeyForValue(null, status, undefined)).toBe('__empty__')
  })

  it('人员取显示名；多选按选项名并列', () => {
    const person = field({ id: 'fld_person', name: '负责人', type: 'person', configMap: {} })
    expect(groupLabelForValue('usr_1', person, undefined, PERSON_DIRECTORY)).toBe('Alice')
    expect(groupLabelForValue(['usr_1', 'usr_2'], person, undefined, PERSON_DIRECTORY)).toBe(
      'Alice、Bob',
    )

    const tags = field({
      id: 'fld_tags',
      name: '标签',
      type: 'multiselect',
      configMap: {
        option_list: [
          { id: 'opt_1', name: '重要', color: 'red' },
          { id: 'opt_2', name: '普通', color: 'gray' },
        ],
      },
    })
    expect(groupLabelForValue(['opt_1', 'opt_2'], tags, undefined, {})).toBe('重要、普通')
  })

  it('日期按日/周/月分桶并给出可读标签', () => {
    const date = field({ id: 'fld_date', name: '截止日期', type: 'date', configMap: {} })
    const millis = new Date(2024, 0, 10).getTime() // 周三
    expect(groupLabelForValue(millis, date, 'day', {})).toBe('2024-01-10')
    expect(groupKeyForValue(millis, date, 'week')).toBe('2024-01-08')
    expect(groupLabelForValue(millis, date, 'week', {})).toBe('2024-01-08 周')
    expect(groupLabelForValue(millis, date, 'month', {})).toBe('2024 年 1 月')
  })
})

describe('buildViewRows（N1 组头无计数）', () => {
  it('按组头 + 组内数据行平铺，组头只有 key/label/collapsed', () => {
    const status = field({})
    const visual = buildViewRows({
      rows: [row('rec_1', 'opt_1'), row('rec_2', 'opt_1'), row('rec_3', 'opt_2')],
      group: { fieldId: 'fld_status' },
      fields: [status],
      personDirectory: {},
      collapsedKeys: new Set(),
    })

    expect(visual.map((entry) => entry.kind)).toEqual([
      'group',
      'record',
      'record',
      'group',
      'record',
    ])
    const groupHeader = visual[0]
    expect(groupHeader.kind).toBe('group')
    if (groupHeader.kind === 'group') {
      expect(groupHeader.label).toBe('进行中')
      expect(groupHeader).not.toHaveProperty('count')
    }
  })

  it('折叠组隐藏其数据行，仍保留组头', () => {
    const status = field({})
    const visual = buildViewRows({
      rows: [row('rec_1', 'opt_1'), row('rec_2', 'opt_1'), row('rec_3', 'opt_2')],
      group: { fieldId: 'fld_status' },
      fields: [status],
      personDirectory: {},
      collapsedKeys: new Set(['opt_1']),
    })

    expect(visual.map((entry) => (entry.kind === 'record' ? entry.record.id : entry.label))).toEqual([
      '进行中',
      '完成',
      'rec_3',
    ])
    expect(visual[0]).toMatchObject({ kind: 'group', collapsed: true })
  })

  it('无分组时退化为普通记录行（手动拖行路径不受影响）', () => {
    const visual = buildViewRows({
      rows: [row('rec_1', 'opt_1')],
      group: null,
      fields: [field({})],
      personDirectory: {},
      collapsedKeys: new Set(),
    })
    expect(visual).toEqual([
      {
        kind: 'record',
        record: row('rec_1', 'opt_1'),
        recordIndex: 0,
      },
    ])
  })
})
