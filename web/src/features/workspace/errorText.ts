// 工作台错误文案：统一从 error-catalog 取「发生了什么 / 下一步做什么」，不各自硬编码。
import { isApiError } from '@/core/http'

export function apiErrorTitle(error: unknown): string {
  if (error === null || error === undefined) return ''
  if (isApiError(error)) return error.catalog.title
  return '操作失败，请稍后重试'
}

export function apiErrorText(error: unknown): string {
  if (error === null || error === undefined) return ''
  if (isApiError(error)) return `${error.catalog.title}。${error.catalog.action}`
  return '操作失败，请稍后重试'
}

// 行内校验文案（票 07）：后端保存期校验（如公式表达式）经信封 message 透传领域
// 原因（如「未知函数: CONCAT」），表单类错误优先展示该原因；空文案回退目录三元组。
export function apiErrorDetailText(error: unknown): string {
  if (isApiError(error)) {
    const detail = error.message.trim()
    if (detail !== '') return detail
  }
  return apiErrorText(error)
}
