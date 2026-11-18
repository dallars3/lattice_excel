// 工作台错误文案单测（票 07）：apiErrorDetailText 优先展示服务端领域文案
// （如「未知函数: CONCAT」），空文案回退目录三元组。
import { describe, expect, it } from 'vitest'

import { ApiError, lookupErrorCatalog } from '@/core/http'

import { apiErrorDetailText, apiErrorText } from './errorText'

describe('workspace apiErrorDetailText（票 07 行内校验文案）', () => {
  it('ApiError 携带服务端领域文案时优先展示', () => {
    const error = new ApiError(1001, '未知函数: CONCAT', null, lookupErrorCatalog(1001))
    expect(apiErrorDetailText(error)).toBe('未知函数: CONCAT')
  })

  it('ApiError 文案为空白时回退目录三元组（title+action）', () => {
    const error = new ApiError(1001, '   ', null, lookupErrorCatalog(1001))
    expect(apiErrorDetailText(error)).toBe(apiErrorText(error))
    expect(apiErrorDetailText(error)).toContain('请求参数缺失或不合法')
  })

  it('非 ApiError 回退通用兜底文案', () => {
    expect(apiErrorDetailText(new Error('boom'))).toBe('操作失败，请稍后重试')
    expect(apiErrorDetailText(null)).toBe('')
  })
})
