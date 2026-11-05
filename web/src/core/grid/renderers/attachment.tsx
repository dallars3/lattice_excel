// attachment 渲染器（票 05）：需求 4.3 附件行「文件名标签 + 缩略图」。
// 值为 file_id 列表；{files} 映射（file_id → 元数据）由网格注入，缺 metadata 时用 file_id 兜底。
// 缩略图走同源直链 /api/v1/files/{id}/thumb（H-79），不引入外部资源。
import { defineRenderer } from '../registry'

import styles from './m2.module.css'

export interface AttachmentFileInfo {
  readonly name?: string
  readonly display_name?: string
}

export interface AttachmentCellConfig {
  /** file_id → 文件元数据（缺 metadata 时回退 id）。 */
  readonly files?: Readonly<Record<string, AttachmentFileInfo | string>>
}

function asIds(value: unknown): string[] {
  const list = Array.isArray(value) ? value : [value]
  const ids: string[] = []
  for (const item of list) {
    if (item === null || item === undefined || item === '') continue
    if (typeof item === 'object') {
      const fileId = (item as { file_id?: unknown; id?: unknown }).file_id ??
        (item as { file_id?: unknown; id?: unknown }).id
      if (typeof fileId === 'string' && fileId !== '') ids.push(fileId)
      continue
    }
    ids.push(String(item))
  }
  return ids
}

function fileName(files: AttachmentCellConfig['files'], fileId: string): string {
  const info = files?.[fileId]
  if (typeof info === 'string') return info
  if (info !== undefined) {
    const name = info.name ?? info.display_name
    if (name !== undefined && name !== '') return name
  }
  return fileId
}

function AttachmentCell({ value, config }: { readonly value: unknown; readonly config: unknown }) {
  const ids = asIds(value)
  if (ids.length === 0) {
    return <span className={styles.attachment} />
  }
  const files = ((config ?? {}) as AttachmentCellConfig).files
  return (
    <span className={styles.attachment}>
      {ids.map((fileId, index) => {
        const name = fileName(files, fileId)
        return (
          <span key={`${fileId}-${index}`} className={styles.attachment__item}>
            <img
              className={styles.attachment__thumb}
              src={`/api/v1/files/${encodeURIComponent(fileId)}/thumb`}
              alt={name}
            />
            <span className={styles.attachment__name} title={name}>
              {name}
            </span>
          </span>
        )
      })}
    </span>
  )
}

/** 附件字段渲染器：缩略图 + 文件名标签列表。 */
export const ATTACHMENT_RENDERER = defineRenderer<unknown, AttachmentCellConfig>({
  align: 'left',
  clip: 'ellipsis',
  component: AttachmentCell,
})
