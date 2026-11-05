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
