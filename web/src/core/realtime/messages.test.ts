import { describe, expect, expectTypeOf, it } from 'vitest'

import {
  CLIENT_MESSAGE_NAMES,
  SERVER_MESSAGE_NAMES,
  W_MESSAGES,
  encodeClientMessage,
  parseClientMessage,
  parseServerMessage,
  type AckMessage,
  type CellChangedMessage,
  type ClientMessage,
  type JoinMessage,
  type ReplayRequestMessage,
  type SnapshotMessage,
  type StructureChangedMessage,
} from './messages'

// 期望值独立事实源：《接口文档-v0.6.md》8.2「WebSocket 消息结构（W-01 ~ W-16）」，
// 16 条消息逐一抄录（名称/方向），不新造。
interface DocMessage {
  code: string
  name: string
  direction: 'c2s' | 's2c'
}

const DOC_MESSAGES: DocMessage[] = [
  { code: 'W-01', name: 'join', direction: 'c2s' },
  { code: 'W-02', name: 'leave', direction: 'c2s' },
  { code: 'W-03', name: 'heartbeat', direction: 'c2s' },
  { code: 'W-04', name: 'cursor', direction: 'c2s' },
  { code: 'W-05', name: 'selection', direction: 'c2s' },
  { code: 'W-06', name: 'editing', direction: 'c2s' },
  { code: 'W-07', name: 'replay_request', direction: 'c2s' },
  { code: 'W-08', name: 'snapshot', direction: 's2c' },
  { code: 'W-09', name: 'cell_changed', direction: 's2c' },
  { code: 'W-10', name: 'record_deleted', direction: 's2c' },
  { code: 'W-11', name: 'structure_changed', direction: 's2c' },
  { code: 'W-12', name: 'presence', direction: 's2c' },
  { code: 'W-13', name: 'cursor_echo', direction: 's2c' },
  { code: 'W-14', name: 'recalc_status', direction: 's2c' },
  { code: 'W-15', name: 'session_revoked', direction: 's2c' },
  { code: 'W-16', name: 'ack', direction: 's2c' },
]

// 每条消息的负载样例：字段按 8.2 载荷字段栏抄录；range 的内部结构文档未细化，
// change_type 取值为示意（文档未枚举），均不构成新契约。
const SAMPLE_MESSAGES: Record<string, Record<string, unknown>> = {
  join: { t: 'join', base_id: 'b1', table_id: 't1', last_seq: 1200 },
  leave: { t: 'leave' },
  heartbeat: { t: 'heartbeat' },
  cursor: { t: 'cursor', record_id: 'rec_1', field_id: 'fld_1', range: { record_id: 'rec_1', field_id: 'fld_1' } },
  selection: { t: 'selection', range: { record_id: 'rec_1', field_id: 'fld_1' } },
  editing: { t: 'editing', record_id: 'rec_1', field_id: 'fld_1', on: true },
  replay_request: { t: 'replay_request', last_seq: 1200 },
  snapshot: {
    t: 'snapshot',
    seq: 1203,
    member_list: [{ user_id: 'usr_1', color: 'blue', online: true, internal: true }],
  },
  cell_changed: { t: 'cell_changed', seq: 1204, record_id: 'rec_1', field_id: 'fld_num', value: 126, version: 8, operator: 'usr_1' },
  record_deleted: { t: 'record_deleted', seq: 1205, record_id: 'rec_1' },
  structure_changed: { t: 'structure_changed', seq: 1206, change_type: 'field_added', table_id: 't1', field_id: 'fld_1' },
  presence: { t: 'presence', user_id: 'usr_2', color: 'green', online: false, internal: false },
  cursor_echo: { t: 'cursor_echo', user_id: 'usr_2', record_id: 'rec_1', field_id: 'fld_1', range: { record_id: 'rec_1', field_id: 'fld_1' } },
  recalc_status: { t: 'recalc_status', record_id: 'rec_1', field_id: 'fld_formula', status: 'calc' },
  session_revoked: { t: 'session_revoked', reason: 'link_closed' },
  ack: { t: 'ack', msg_id: 'msg_1' },
}

describe('core/realtime messages W-01~W-16（表驱动）', () => {
  it('W_MESSAGES 恰好 16 条、编号连续、名称与方向逐一等于接口文档 8.2 清单（一个不少、一个不多）', () => {
    expect(W_MESSAGES).toHaveLength(16)
    expect([...W_MESSAGES]).toEqual(DOC_MESSAGES)
  })

  it('C→S / S→C 名单与 8.2 方向划分一致（7 + 9 = 16）', () => {
    expect([...CLIENT_MESSAGE_NAMES]).toEqual(DOC_MESSAGES.filter((m) => m.direction === 'c2s').map((m) => m.name))
    expect([...SERVER_MESSAGE_NAMES]).toEqual(DOC_MESSAGES.filter((m) => m.direction === 's2c').map((m) => m.name))
    expect(CLIENT_MESSAGE_NAMES.length + SERVER_MESSAGE_NAMES.length).toBe(16)
  })

  it.each(Object.keys(SAMPLE_MESSAGES))('消息 %s：编/解码回环保留全部负载字段', (name) => {
    const sample = SAMPLE_MESSAGES[name]
    const isClient = CLIENT_MESSAGE_NAMES.includes(name as never)
    if (isClient) {
      const encoded = encodeClientMessage(sample as unknown as ClientMessage)
      expect(JSON.parse(encoded)).toEqual(sample)
      expect(parseClientMessage(encoded)).toEqual(sample)
    } else {
      const raw = JSON.stringify(sample)
      expect(parseServerMessage(raw)).toEqual(sample)
    }
  })

  it('对账字段 seq / last_seq 按契约落类型（seq 为必填数值，last_seq 为可选数值）', () => {
    expectTypeOf<JoinMessage['last_seq']>().toEqualTypeOf<number | undefined>()
    expectTypeOf<ReplayRequestMessage['last_seq']>().toEqualTypeOf<number>()
    expectTypeOf<SnapshotMessage['seq']>().toEqualTypeOf<number>()
    expectTypeOf<CellChangedMessage['seq']>().toEqualTypeOf<number>()
    expectTypeOf<StructureChangedMessage['seq']>().toEqualTypeOf<number>()
    expectTypeOf<AckMessage['msg_id']>().toEqualTypeOf<string>()
  })

  it('非法输入解析返回 null 且不抛异常（未知 t / 非 JSON / 方向不符）', () => {
    expect(parseServerMessage('not-json')).toBeNull()
    expect(parseServerMessage('{"t":"join"}')).toBeNull() // join 属 C→S，不是服务端消息
    expect(parseServerMessage('{"t":"unknown_msg"}')).toBeNull()
    expect(parseClientMessage(JSON.stringify(SAMPLE_MESSAGES.snapshot))).toBeNull() // snapshot 属 S→C
    expect(parseClientMessage('{"t":"nope"}')).toBeNull()
  })

  it('parseServerMessage 覆盖全部 9 条 S→C 消息名（分发表无缺项）', () => {
    for (const name of SERVER_MESSAGE_NAMES) {
      const raw = JSON.stringify(SAMPLE_MESSAGES[name])
      expect(parseServerMessage(raw), name).not.toBeNull()
    }
  })
})
