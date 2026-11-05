// 网格记录数据源（票 05）：useInfiniteQuery 消费 H-48，首页 limit=20、后续页 limit=100；
// 页游标推进完全交给 core/http 游标 helper，网格滚动只触发 fetchNextPage。
import { useInfiniteQuery } from '@tanstack/react-query'

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
    queryFn: ({ pageParam }) =>
      queryRecords({
        baseId,
        tableId: tableId ?? '',
        viewId,
        cursor: pageParam,
        limit:
          pageParam === null
            ? RECORDS_FIRST_PAGE_LIMIT
            : RECORDS_NEXT_PAGE_LIMIT,
      }),
    initialPageParam: null as string | null,
    getNextPageParam,
    enabled: enabled && baseId !== '' && tableId !== null && viewId !== null,
  })

  const pages = query.data?.pages ?? []
  const rows: RecordRow[] = pages.flatMap((page) => [...page.items])

  return {
    rows,
    pages,
    hasMore: query.hasNextPage ?? false,
    isFetchingNextPage: query.isFetchingNextPage,
    fetchNextPage: query.fetchNextPage,
    isError: query.isError,
    error: query.error,
    isFetching: query.isFetching,
    refetch: query.refetch,
  }
}
