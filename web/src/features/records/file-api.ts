// 文件与附件接口（票 08）：H-77 上传 / H-78 下载 / H-79 缩略图 / H-80 重命名 / H-81 删除。
// 事实源 excel_web_api/internal/gateway/file_routes.go：
// - H-77 为 multipart（字段 file + usage，query base_id），不能走 JSON httpClient，改用 fetch + FormData；
// - H-80/H-81 不接受 client_request_id，走 httpClient 的 attachClientRequestId:false；
// - 下载/缩略图为同源直链，下载链接由前端直接指向 H-78。
import { ApiError, buildUrl, http, isApiError, lookupErrorCatalog } from '@/core/http'
import { API_BASE } from '@/lib/env'

import { toNumber } from './api'

/** 单文件上限（需求 4.3 / 网关 maxGatewayUploadBytes）。 */
export const MAX_ATTACHMENT_BYTES = 20 * 1024 * 1024
/** 单记录附件个数上限（需求 4.3 / 接口文档 8.0.6）。 */
export const MAX_ATTACHMENTS_PER_RECORD = 10

/** 归一后的文件元数据：protojson int64（size/created_at）转 number。 */
export interface FileMeta {
  id: string
  name: string
  display_name: string
  ext: string
  mime: string
  size: number
  created_at: number
}

interface RawFileMeta {
  id?: string
  name?: string
  display_name?: string
  ext?: string
  mime?: string
  size?: number | string
  created_at?: number | string
}

interface EnvelopeShape {
  code?: unknown
  message?: unknown
  data?: unknown
}

function normalizeFileMeta(raw: RawFileMeta): FileMeta {
  return {
    id: raw.id ?? '',
    name: raw.name ?? '',
    display_name: raw.display_name ?? raw.name ?? '',
    ext: raw.ext ?? '',
    mime: raw.mime ?? '',
    size: toNumber(raw.size),
    created_at: toNumber(raw.created_at),
  }
}

function isEnvelope(value: unknown): value is EnvelopeShape {
  return typeof value === 'object' && value !== null && typeof (value as EnvelopeShape).code === 'number'
}

/** multipart 响应与 JSON 写接口共用同一信封；非 0 码抛 ApiError（文案取 error-catalog）。 */
async function parseMultipartEnvelope<T>(response: Response): Promise<T> {
  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    throw new ApiError(
      9001,
      '响应不是有效数据（可能为网关错误页）',
      null,
      lookupErrorCatalog(9001),
    )
  }
  if (!isEnvelope(payload)) {
    throw new ApiError(
      9001,
      '响应缺少统一信封（code/message/data）',
      null,
      lookupErrorCatalog(9001),
    )
  }
  const code = Number(payload.code)
  if (code === 0) return payload.data as T
  throw new ApiError(
    code,
    typeof payload.message === 'string' ? payload.message : lookupErrorCatalog(code).title,
    payload.data,
    lookupErrorCatalog(code),
  )
}

/**
 * H-77 上传附件：POST /api/v1/files?base_id=，multipart 字段 file + usage=attachment。
 * 不注入 client_request_id；会话票据经 credentials:'include' 同源 Cookie。
 */
export async function uploadFile(baseId: string, file: File): Promise<FileMeta> {
  const form = new FormData()
  form.append('file', file)
  form.append('usage', 'attachment')
  const response = await fetch(buildUrl(API_BASE, '/api/v1/files', { base_id: baseId }), {
    method: 'POST',
    body: form,
    credentials: 'include',
  })
  const raw = await parseMultipartEnvelope<RawFileMeta>(response)
  return normalizeFileMeta(raw)
}

/** H-80 重命名显示名：PATCH /api/v1/files/{file_id}?base_id=，body 仅 {display_name}。 */
export async function renameFile(
  fileId: string,
  displayName: string,
  baseId?: string,
): Promise<FileMeta> {
  const raw = await http.patch<RawFileMeta>(
    `/api/v1/files/${encodeURIComponent(fileId)}`,
    { display_name: displayName },
    {
      query: baseId === undefined || baseId === '' ? undefined : { base_id: baseId },
      attachClientRequestId: false,
    },
  )
  return normalizeFileMeta(raw)
}

/** H-81 删除附件：DELETE /api/v1/files/{file_id}?base_id=，无请求体。 */
export async function deleteFile(baseId: string, fileId: string): Promise<void> {
  await http.delete<unknown>(
    `/api/v1/files/${encodeURIComponent(fileId)}`,
    undefined,
    { query: { base_id: baseId }, attachClientRequestId: false },
  )
}

/** H-78 下载直链（同源）。 */
export function fileDownloadUrl(fileId: string): string {
  return `/api/v1/files/${encodeURIComponent(fileId)}/download`
}

/** H-79 缩略图直链（同源）。 */
export function fileThumbUrl(fileId: string): string {
  return `/api/v1/files/${encodeURIComponent(fileId)}/thumb`
}

/** 判断 5002（黑名单/超限）与一般写失败分离提示。 */
export function isFileRejectedError(error: unknown): boolean {
  return isApiError(error) && error.code === 5002
}
