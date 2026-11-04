// 演示页游标数据接线（票 09）：useInfiniteQuery × 票 04 游标 helper
// （extractCursorPage 归一化 + getNextPageParam 推进）× query-keys recordsKeys.list。
// 网格窗口按需 fetchNextPage（GridDemoPage 滚动触发）；页游标推进由 helper 统一裁决，页面不手写。
import { useInfiniteQuery } from '@tanstack/react-query'

import { extractCursorPage, getNextPageParam, http } from '@/core/http'
import { recordsKeys } from '@/query-keys/records'

import { DEMO_PAGE_SIZE, DEMO_RECORDS_PATH, DEMO_VIEW_ID, type DemoRecordRow } from './demo-table'

/** H-48 形状的 data（信封已被 http.get 剥离）。 */
interface DemoRecordEnvelope {
  readonly record_list: DemoRecordRow[]
  readonly has_more: boolean
  readonly next_cursor: string | null
}

export function fetchDemoRecordPage({ pageParam }: { pageParam: string | null }) {
  return http
    .get<DemoRecordEnvelope>(DEMO_RECORDS_PATH, {
      query: { cursor: pageParam ?? undefined, limit: DEMO_PAGE_SIZE },
    })
    .then((data) => extractCursorPage<DemoRecordRow>(data, 'record_list'))
}

export function useDemoRecords() {
  return useInfiniteQuery({
    queryKey: recordsKeys.list(DEMO_VIEW_ID),
    queryFn: fetchDemoRecordPage,
    initialPageParam: null as string | null,
    getNextPageParam,
  })
}
