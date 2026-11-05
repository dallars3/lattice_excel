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

/** H-52 批量返回项（result_list / fail_row_list 同形）。 */
export interface BatchResultItem {
  readonly index: number
  readonly recordId: string
  readonly ok: boolean
  readonly version: number
}

/** 批量写回所需的操作快照：before 用于失败回滚。 */
export interface BatchOperationSnapshot {
  readonly recordId: string
  readonly fieldId: string
  readonly before: unknown
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

/**
 * 批量成功项写回记录版本：同一记录多字段写入时以最后一项的版本为准。
 * 版本缺失（0）时不写回，避免把缓存版本降为 0 造成后续 H-51 假冲突。
 */
export function applyBatchVersions(
  data: RecordsData | undefined,
  resultList: readonly BatchResultItem[],
): RecordsData | undefined {
  if (data === undefined) return undefined
  const versions = new Map<string, number>()
  for (const item of resultList) {
    if (!item.ok || item.recordId === '' || item.version <= 0) continue
    versions.set(item.recordId, item.version)
  }
  if (versions.size === 0) return data
  let changed = false
  const pages = data.pages.map((page) => {
    if (!page.items.some((row) => versions.has(row.id))) return page
    changed = true
    return {
      ...page,
      items: page.items.map((row) => {
        const version = versions.get(row.id)
        return version === undefined || version === row.version ? row : { ...row, version }
      }),
    }
  })
  return changed ? { ...data, pages } : data
}

/**
 * 批量部分失败回滚：result_list/fail_row_list 中 ok=false 或无版本的 index，
 * 按对应操作的 before 值恢复单元格；成功项保持乐观后的新值。
 */
export function revertBatchFailures(
  data: RecordsData | undefined,
  operations: readonly BatchOperationSnapshot[],
  resultList: readonly BatchResultItem[],
): RecordsData | undefined {
  if (data === undefined) return undefined
  let next = data
  for (const item of resultList) {
    if (item.ok && item.version > 0) continue
    const operation = operations[item.index]
    if (operation === undefined) continue
    const updated = applyOptimisticCell(next, {
      recordId: operation.recordId,
      fieldId: operation.fieldId,
      value: operation.before,
    })
    if (updated !== undefined) next = updated
  }
  return next
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
