import { describe, expect, it } from 'vitest'

import {
  ALL_ERROR_CODES,
  ERROR_CATALOG,
  lookupErrorCatalog,
  type ErrorCode,
  type ErrorLayer,
  type RecoveryStrategy,
} from './error-catalog'

// 期望值独立事实源：《前端-技术方案-v0.1.md》5.1（全局：2001/3001/5003/9001/9002；
// 局部：1001/1002/4001/4002/5001/5002/3002）与《接口文档-v0.6.md》8.0.1（13 码与 HTTP 状态）。
interface Case {
  code: ErrorCode
  httpStatus: number
  layer: ErrorLayer
  recovery: RecoveryStrategy
}

const cases: Case[] = [
  { code: 1001, httpStatus: 400, layer: 'local', recovery: 'reinput' },
  { code: 1002, httpStatus: 400, layer: 'local', recovery: 'reinput' },
  { code: 2001, httpStatus: 401, layer: 'global', recovery: 'relogin' },
  { code: 2002, httpStatus: 401, layer: 'local', recovery: 'reinput' },
  { code: 3001, httpStatus: 403, layer: 'global', recovery: 'readonly-fallback' },
  { code: 3002, httpStatus: 403, layer: 'local', recovery: 'readonly-fallback' },
  { code: 4001, httpStatus: 409, layer: 'local', recovery: 'resolve-conflict' },
  { code: 4002, httpStatus: 409, layer: 'local', recovery: 'refresh' },
  { code: 5001, httpStatus: 413, layer: 'local', recovery: 'none' },
  { code: 5002, httpStatus: 413, layer: 'local', recovery: 'reinput' },
  { code: 5003, httpStatus: 429, layer: 'global', recovery: 'retry' },
  { code: 9001, httpStatus: 500, layer: 'global', recovery: 'retry' },
  { code: 9002, httpStatus: 500, layer: 'global', recovery: 'retry' },
]

describe('core/http error-catalog 13 错误码（表驱动）', () => {
  it('ALL_ERROR_CODES 与接口文档 8.0.1 的 13 码清单完全一致', () => {
    expect(ALL_ERROR_CODES).toEqual(cases.map((c) => c.code))
  })

  it.each(cases.map((c) => [c.code, c] as const))(
    '码 %i：三元组完整（文案/动作/恢复策略）且层级与恢复策略符合技术方案 5.1',
    (code, c) => {
      const entry = lookupErrorCatalog(code)
      expect(entry.code).toBe(code)
      expect(entry.httpStatus).toBe(c.httpStatus)
      expect(entry.layer).toBe(c.layer)
      expect(entry.recovery).toBe(c.recovery)
      // 文案（title）与动作（action）非空，面向用户的下一步指引。
      expect(entry.title.trim()).not.toBe('')
      expect(entry.action.trim()).not.toBe('')
    },
  )

  it('ERROR_CATALOG 恰好覆盖 13 码，无多余条目', () => {
    expect(Object.keys(ERROR_CATALOG).map(Number).sort((a, b) => a - b)).toEqual(
      [...ALL_ERROR_CODES].sort((a, b) => a - b),
    )
  })

  it('未知码回退到兜底条目：全局层、仅提示不可自动恢复', () => {
    const fallback = lookupErrorCatalog(424242)
    expect(fallback.layer).toBe('global')
    expect(fallback.recovery).toBe('none')
    expect(fallback.title.trim()).not.toBe('')
    expect(fallback.action.trim()).not.toBe('')
  })

  it('恢复策略枚举全部被 13 码用到且语义有注释对应（relogin/reinput/readonly-fallback/resolve-conflict/refresh/retry/none）', () => {
    const used = new Set(cases.map((c) => c.recovery))
    expect(used.size).toBeGreaterThanOrEqual(6)
    for (const c of cases) {
      expect(lookupErrorCatalog(c.code).recovery).toBe(c.recovery)
    }
  })
})
