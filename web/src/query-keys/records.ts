// records 域 query key 工厂首版（plan §3.5 随票 04 落地）：
// - list(viewId)：useInfiniteQuery 固定 key（页游标走 pageParam 推进，不进 key）；
// - page(viewId, cursor)：单页 key ['records', viewId, cursor]（技术方案第 7 章原型，
//   供网格 loadedRanges 按 cursor 区间缓存与 cell_changed 精确失效使用）。
// 其余域 key（base/table/field/view/comment/notification/share）M2 按需补。
export const recordsKeys = {
  all: ['records'] as const,
  /** useInfiniteQuery 固定 key：['records', viewId]。 */
  list: (viewId: string) => ['records', viewId] as const,
  /** 单页 key（技术方案第 7 章形状）：['records', viewId, cursor]。 */
  page: (viewId: string, cursor: string | null) => ['records', viewId, cursor] as const,
}

export type RecordsQueryKey = ReturnType<typeof recordsKeys.page>
