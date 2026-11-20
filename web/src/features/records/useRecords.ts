// 网格记录数据源（票 05）：useInfiniteQuery 消费 H-48，首页 limit=20、后续页 limit=100；
// 页游标推进完全交给 core/http 游标 helper，网格滚动只触发 fetchNextPage。
import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query'

import { getNextPageParam } from '@/core/http'
import { recordsKeys } from '@/query-keys/records'

import { queryRecords, type RecordRow } from './api'

export const RECORDS_FIRST_PAGE_LIMIT = 20
export const RECORDS_NEXT_PAGE_LIMIT = 100

export interface UseRecordsOptions {
  baseId: string
  tableId: string | null
  viewId: string | null
  /** 筛选/排序/分组条件签名（票 07）：变更即新 key，从空游标全量重拉。 */
  signature?: string
  /** 字段/视图未就绪或全部字段隐藏时不请求。 */
  enabled?: boolean
}

/** 记录无限查询：返回网格消费所需的最小投影（行集合、分页状态与动作）。 */
export function useRecords({
  baseId,
  tableId,
  viewId,
  signature = '',
  enabled = true,
}: UseRecordsOptions) {
  const query = useInfiniteQuery({
    queryKey: recordsKeys.listFor(tableId ?? '', viewId ?? '', signature),
    queryFn: ({ pageParam, signal }) =>
      queryRecords({
        baseId,
        tableId: tableId ?? '',
        viewId,
        cursor: pageParam,
        limit:
          pageParam === null
            ? RECORDS_FIRST_PAGE_LIMIT
            : RECORDS_NEXT_PAGE_LIMIT,
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam,
    // 新确认条件 key 加载期间只把上一 key 结果当作临时展示；isPlaceholderData 供 UI 明示。
    placeholderData: keepPreviousData,
    enabled: enabled && baseId !== '' && tableId !== null && viewId !== null,
  })

  const pages = query.data?.pages ?? []
  const rows: RecordRow[] = pages.flatMap((page) => [...page.items])

  return {
    rows,
    pages,
    /** 当前 key 的首窗口数据已到达（placeholder 旧 key 不算就绪）：首屏探针/计数回报以此为界。 */
    hasData: query.data !== undefined && !query.isPlaceholderData,
    /** 新确认条件加载中，rows 来自上一 key 的 keepPreviousData 临时展示。 */
    isPlaceholderData: query.isPlaceholderData,
    hasMore: query.hasNextPage ?? false,
    isFetchingNextPage: query.isFetchingNextPage,
    fetchNextPage: query.fetchNextPage,
    isError: query.isError,
    error: query.error,
    isFetching: query.isFetching,
    refetch: query.refetch,
  }
}
