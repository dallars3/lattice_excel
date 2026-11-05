// 乐观更新纯逻辑（票 06）：InfiniteQuery 数据结构的不可变变换。
// 纯函数、无 React/Query 依赖——网格写入层据此先落本地再按响应落版本或回滚。
// 空值 null 与数字 0 严格保留（只按 key 覆盖，不做真值判断）。
import type { RecordRow } from './api'

/** 与 core/http CursorPage 同形（保留 readonly 以兼容 React Query data）。 */
export interface RecordsPageState {
  readonly items: readonly RecordRow[]
  readonly hasMore: boolean
  readonly nextCursor: string | null
}

/** useInfiniteQuery 的数据形状（pages + pageParams）。 */
export interface RecordsData {
  readonly pages: readonly RecordsPageState[]
  readonly pageParams: readonly unknown[]
}

export interface CellPatch {
  readonly recordId: string
  readonly fieldId: string
  readonly value: unknown
  /** 省略时保留原版本（提交中只改值；成功响应再落版本）。 */
  readonly version?: number
}

/** 在全部页中查找记录。 */
export function findRecord(
  data: RecordsData | undefined,
  recordId: string,
): RecordRow | undefined {
  for (const page of data?.pages ?? []) {
    const found = page.items.find((row) => row.id === recordId)
    if (found !== undefined) return found
  }
  return undefined
}

/** 记录不存在/缓存缺失时原样返回（不虚构行；undefined 透传）。 */
export function applyOptimisticCell(
  data: RecordsData | undefined,
  patch: CellPatch,
): RecordsData | undefined {
  if (data === undefined) return undefined
  let changed = false
  const pages = data.pages.map((page) => {
    if (!page.items.some((row) => row.id === patch.recordId)) return page
    changed = true
    return {
      ...page,
      items: page.items.map((row) => {
        if (row.id !== patch.recordId) return row
        return {
          ...row,
          cellsMap: { ...row.cellsMap, [patch.fieldId]: patch.value },
          version: patch.version ?? row.version,
        }
      }),
    }
  })
  return changed ? { ...data, pages } : data
}

/** 失败回滚：语义同写入旧值（version 可选恢复到旧版本）。 */
export function rollbackCell(
  data: RecordsData | undefined,
  patch: CellPatch,
): RecordsData | undefined {
  return applyOptimisticCell(data, patch)
}

/** 删除记录（跨页过滤，保留页结构与 hasMore/nextCursor）。 */
export function removeRecords(
  data: RecordsData | undefined,
  recordIds: readonly string[],
): RecordsData | undefined {
  if (data === undefined) return undefined
  const target = new Set(recordIds)
  if (target.size === 0) return data
  let changed = false
  const pages = data.pages.map((page) => {
    const items = page.items.filter((row) => !target.has(row.id))
    if (items.length === page.items.length) return page
    changed = true
    return { ...page, items }
  })
  return changed ? { ...data, pages } : data
}

/** 追加记录到末页（无页时建立首页；hasMore/nextCursor 不变）。 */
export function appendRecord(
  data: RecordsData | undefined,
  record: RecordRow,
): RecordsData | undefined {
  if (data === undefined) return { pages: [{ items: [record], hasMore: false, nextCursor: null }], pageParams: [null] }
  if (data.pages.length === 0) {
    return { ...data, pages: [{ items: [record], hasMore: false, nextCursor: null }], pageParams: [...data.pageParams, null] }
  }
  const last = data.pages[data.pages.length - 1]
  if (last === undefined) return data
  const pages = [...data.pages]
  pages[pages.length - 1] = { ...last, items: [...last.items, record] }
  return { ...data, pages }
}
