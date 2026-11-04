import { describe, expect, it } from 'vitest'

import {
  API_BASE,
  DEFAULT_NOTICE_TEXT,
  NOTICE_TEXT,
  resolveEnv,
  WS_URL,
  type AppEnv,
  type EnvRawVars,
  type LocationLike,
} from './env'

describe('lib/env resolveEnv（表驱动）', () => {
  interface Case {
    name: string
    raw: EnvRawVars
    location: LocationLike | undefined
    want: AppEnv
  }

  const cases: Case[] = [
    {
      name: '全部未设置：API_BASE 同源空串、http 页面推算 ws 同源、声明走默认文案',
      raw: {},
      location: { protocol: 'http:', host: 'localhost:3000' },
      want: { API_BASE: '', WS_URL: 'ws://localhost:3000', NOTICE_TEXT: DEFAULT_NOTICE_TEXT },
    },
    {
      name: 'https 页面：推算 wss 同源',
      raw: {},
      location: { protocol: 'https:', host: 'table.example.com' },
      want: { API_BASE: '', WS_URL: 'wss://table.example.com', NOTICE_TEXT: DEFAULT_NOTICE_TEXT },
    },
    {
      name: '非浏览器（无 location）：WS_URL 退化为空串',
      raw: {},
      location: undefined,
      want: { API_BASE: '', WS_URL: '', NOTICE_TEXT: DEFAULT_NOTICE_TEXT },
    },
    {
      name: '设置 VITE_API_BASE：透传不再同源',
      raw: { VITE_API_BASE: 'http://api.example.com' },
      location: { protocol: 'http:', host: 'localhost:3000' },
      want: {
        API_BASE: 'http://api.example.com',
        WS_URL: 'ws://localhost:3000',
        NOTICE_TEXT: DEFAULT_NOTICE_TEXT,
      },
    },
    {
      name: '设置 VITE_WS_URL：覆盖协议推算',
      raw: { VITE_WS_URL: 'wss://ws.example.com' },
      location: { protocol: 'https:', host: 'table.example.com' },
      want: { API_BASE: '', WS_URL: 'wss://ws.example.com', NOTICE_TEXT: DEFAULT_NOTICE_TEXT },
    },
    {
      name: '设置 VITE_NOTICE_TEXT：覆盖默认文案',
      raw: { VITE_NOTICE_TEXT: '覆盖声明文案' },
      location: { protocol: 'http:', host: 'localhost:3000' },
      want: { API_BASE: '', WS_URL: 'ws://localhost:3000', NOTICE_TEXT: '覆盖声明文案' },
    },
  ]

  it.each(cases)('$name', ({ raw, location, want }) => {
    expect(resolveEnv(raw, location)).toEqual(want)
  })
})

describe('lib/env 模块常量（测试进程未注入 VITE_*）', () => {
  it('导出值与默认语义一致：同源空串、ws 推算、默认试运行文案', () => {
    expect(API_BASE).toBe('')
    expect(WS_URL).toBe('ws://localhost:3000')
    expect(NOTICE_TEXT).toBe(DEFAULT_NOTICE_TEXT)
  })
})
