import { QueryClient, QueryClientProvider, useInfiniteQuery } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { extractCursorPage, getNextPageParam, type CursorPage } from './cursor'
import { http } from './client'
import { recordsKeys } from '@/query-keys/records'
import { mockCursorRecords, type MockCursorRow } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

const PATH = '/api/v1/bases/b_1/records'

describe('core/http cursor getNextPageParam（pageParam 推进，表驱动）', () => {
  const cases: { name: string; page: Pick<CursorPage<unknown>, 'hasMore' | 'nextCursor'>; want: string | undefined }[] = [
    { name: '还有更多且游标存在：推进到 next_cursor', page: { hasMore: true, nextCursor: 'cur_3' }, want: 'cur_3' },
    { name: '没有更多：undefined（终止拉取）', page: { hasMore: false, nextCursor: null }, want: undefined },
    { name: '还有更多但游标缺失（契约违背）：按终止处理，不死循环', page: { hasMore: true, nextCursor: null }, want: undefined },
  ]
  it.each(cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    expect(getNextPageParam({ items: [], ...c.page })).toBe(c.want)
  })
})

describe('core/http cursor extractCursorPage（列表字段归一化）', () => {
  it('从 H-48 形状的 data 抽出 items/hasMore/nextCursor', () => {
    const data = {
      record_list: [{ id: 'rec_1' }, { id: 'rec_2' }],
      has_more: true,
      next_cursor: 'cur_2',
    }
    const page = extractCursorPage<{ id: string }>(data, 'record_list')
    expect(page.items.map((r) => r.id)).toEqual(['rec_1', 'rec_2'])
    expect(page.hasMore).toBe(true)
    expect(page.nextCursor).toBe('cur_2')
  })

  it('列表字段缺失（契约违背）：抛错而非返回空页', () => {
    const data = { has_more: false, next_cursor: null }
    expect(() => extractCursorPage(data, 'record_list')).toThrow()
  })
})

describe('query-keys records（plan §3.5 首版）', () => {
  it("list 为 useInfiniteQuery 固定 key ['records', viewId]；page 为单页 key ['records', viewId, cursor]", () => {
    expect(recordsKeys.all).toEqual(['records'])
    expect(recordsKeys.list('vw_1')).toEqual(['records', 'vw_1'])
    expect(recordsKeys.page('vw_1', 'cur_3')).toEqual(['records', 'vw_1', 'cur_3'])
    expect(recordsKeys.page('vw_1', null)).toEqual(['records', 'vw_1', null])
  })
})

describe('core/http cursor 集成：useInfiniteQuery + MSW 假接口走通', () => {
  it('多页拉取至 has_more 终止；pageParam 按 next_cursor 推进', async () => {
    const rows: MockCursorRow[] = Array.from({ length: 7 }, (_, i) => ({ id: `rec_${i + 1}` }))
    const seenCursors: (string | null)[] = []
    mswServer.use(
      mockCursorRecords({
        path: PATH,
        rows,
        onRequest: (request) => {
          seenCursors.push(new URL(request.url).searchParams.get('cursor'))
        },
      }),
    )

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )

    const fetchPage = ({ pageParam }: { pageParam: string | null }) =>
      http
        .get<{ record_list: MockCursorRow[]; has_more: boolean; next_cursor: string | null }>(PATH, {
          query: { table_id: 'tbl_1', view_id: 'vw_1', cursor: pageParam ?? undefined, limit: 3 },
        })
        .then((data) => extractCursorPage<MockCursorRow>(data, 'record_list'))

    const { result } = renderHook(
      () =>
        useInfiniteQuery({
          queryKey: recordsKeys.list('vw_1'),
          queryFn: fetchPage,
          initialPageParam: null as string | null,
          getNextPageParam,
        }),
      { wrapper },
    )

    // 第 1 页：limit=3 → rec_1..3，还有更多
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.pages[0].items.map((r) => r.id)).toEqual(['rec_1', 'rec_2', 'rec_3'])
    expect(result.current.hasNextPage).toBe(true)

    // 第 2 页
    await result.current.fetchNextPage()
    await waitFor(() =>
      expect(result.current.data?.pages).toHaveLength(2),
    )
    expect(result.current.data?.pages[1].items.map((r) => r.id)).toEqual(['rec_4', 'rec_5', 'rec_6'])
    expect(seenCursors[1]).toBe('cur_3')

    // 第 3 页：只剩 1 行，has_more=false 终止
    await result.current.fetchNextPage()
    await waitFor(() =>
      expect(result.current.hasNextPage).toBe(false),
    )
    const flattened = result.current.data?.pages.flatMap((page) => page.items.map((r) => r.id))
    expect(flattened).toEqual(['rec_1', 'rec_2', 'rec_3', 'rec_4', 'rec_5', 'rec_6', 'rec_7'])
    expect(seenCursors[2]).toBe('cur_6')
    expect(seenCursors).toHaveLength(3)
  })
})
