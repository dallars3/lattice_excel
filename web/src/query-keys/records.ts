// records 域 query key 工厂首版（plan §3.5 随票 04 落地）：
// - list(viewId)：useInfiniteQuery 固定 key（页游标走 pageParam 推进，不进 key）；
// - page(viewId, cursor)：单页 key ['records', viewId, cursor]（技术方案第 7 章原型，
//   供网格 loadedRanges 按 cursor 区间缓存与 cell_changed 精确失效使用）。
// 其余域 key（base/table/field/view/comment/notification/share）M2 按需补。
export const recordsKeys = {
  all: ['records'] as const,
  /** 表作用域（票 05）：['records','table',tableId]，覆盖该表全部视图/记录查询。 */
  table: (tableId: string) => ['records', 'table', tableId] as const,
  /**
   * 单视图无限查询固定 key（票 05）：['records','table',tableId,'list',viewId]。
   * 票 07：第三个参数为筛选/排序/分组条件签名；有签名时追加 'conditions' 段，
   * 条件变更即新 key，useInfiniteQuery 从空游标全量重拉（不拼接旧页）。
   */
  listFor: (tableId: string, viewId: string, signature = '') =>
    (signature === ''
      ? ['records', 'table', tableId, 'list', viewId]
      : ['records', 'table', tableId, 'list', viewId, 'conditions', signature]) as readonly [
      string,
      ...string[],
    ],
  /** 单记录 key（票 05，供票 08）：['records','record',recordId]。 */
  record: (recordId: string) => ['records', 'record', recordId] as const,
  /** useInfiniteQuery 固定 key：['records', viewId]（M1 形状保留）。 */
  list: (viewId: string) => ['records', viewId] as const,
  /** 单页 key（技术方案第 7 章形状）：['records', viewId, cursor]（M1 形状保留）。 */
  page: (viewId: string, cursor: string | null) => ['records', viewId, cursor] as const,
}

export type RecordsQueryKey = ReturnType<typeof recordsKeys.page>
