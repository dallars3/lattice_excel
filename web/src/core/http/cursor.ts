// 游标分页 helper（技术方案 5.1 / ADR 0002）：适配 @tanstack/react-query useInfiniteQuery。
// 契约（接口文档 8.0.2 / H-48）：请求 cursor+limit；响应列表字段名随接口而定（record_list 等），
// 统一附 has_more、next_cursor。网格窗口按需 fetchNextPage。

/** 归一化后的游标页：列表字段经 extractCursorPage 抽象掉（record_list/comment_list/…）。 */
export interface CursorPage<T> {
  readonly items: readonly T[]
  readonly hasMore: boolean
  readonly nextCursor: string | null
}

/** 后端游标分页响应的公共形状（列表字段之外的公共字段）。 */
export interface CursorEnvelopeBase {
  readonly has_more: boolean
  readonly next_cursor: string | null
}

/**
 * useInfiniteQuery 的 getNextPageParam：pageParam 即请求 cursor（首页 null）。
 * - has_more 且 next_cursor 存在 → 推进到 next_cursor；
 * - 否则返回 undefined 终止（含 has_more=true 而 next_cursor=null 的契约违背防御，避免死循环）。
 */
export function getNextPageParam<T>(lastPage: CursorPage<T>): string | undefined {
  if (lastPage.hasMore && lastPage.nextCursor !== null) {
    return lastPage.nextCursor
  }
  return undefined
}

/**
 * 从后端分页 data 抽出归一化 CursorPage。listKey 指定该接口的列表字段名
 * （H-48 为 'record_list'；H-55 为 'history_list'；H-57 为 'comment_list'…）。
 * 列表字段缺失视为契约违背，抛错暴露问题（不静默返回空页）。
 */
export function extractCursorPage<T>(
  data: CursorEnvelopeBase,
  listKey: string,
): CursorPage<T> {
  const list: unknown = (data as unknown as Record<string, unknown>)[listKey]
  if (!Array.isArray(list)) {
    throw new Error(`游标分页契约违背：响应缺少列表字段 "${listKey}"`)
  }
  return {
    items: list as readonly T[],
    hasMore: data.has_more,
    nextCursor: data.next_cursor,
  }
}

/** 游标 limit 默认值（接口文档 8.0.2：默认 20，上限 100）。 */
export const DEFAULT_CURSOR_LIMIT = 20
/** 游标 limit 上限。 */
export const MAX_CURSOR_LIMIT = 100

/** limit 收敛：缺省 20；超上限截到 100；下限 1。 */
export function clampCursorLimit(limit?: number): number {
  if (limit === undefined) return DEFAULT_CURSOR_LIMIT
  if (limit > MAX_CURSOR_LIMIT) return MAX_CURSOR_LIMIT
  if (limit < 1) return 1
  return limit
}
