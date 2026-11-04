import '@testing-library/jest-dom/vitest'

import { QueryClient, useQueryClient } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { StoreApi, UseBoundStore } from 'zustand'
import { describe, expect, it } from 'vitest'

import { AppProviders } from '@/app/providers'

import {
  editSessionStore,
  offlineQueueStore,
  presenceStore,
  selectionStore,
  uiStore,
  viewConfigDraftStore,
} from './index'

type AnyStore = UseBoundStore<StoreApi<Record<string, unknown>>>

interface StoreCase {
  name: string
  store: AnyStore
  expected: Record<string, unknown>
}

// 技术方案第 8 章清单：6 个 store 骨架的初始状态（唯一事实源）。
const STORE_CASES: StoreCase[] = [
  { name: 'selectionStore', store: selectionStore as unknown as AnyStore, expected: { focusedCell: null, selectedRange: null } },
  { name: 'editSessionStore', store: editSessionStore as unknown as AnyStore, expected: { editingCell: null, snapshotBeforeEdit: null, version: 0 } },
  { name: 'presenceStore', store: presenceStore as unknown as AnyStore, expected: { onlineMembers: [], otherCursors: {}, editingBy: {} } },
  { name: 'offlineQueueStore', store: offlineQueueStore as unknown as AnyStore, expected: { pendingItems: [], replayProgress: null } },
  { name: 'viewConfigDraftStore', store: viewConfigDraftStore as unknown as AnyStore, expected: { isDirty: false, draft: null } },
  { name: 'uiStore', store: uiStore as unknown as AnyStore, expected: { bannerMessage: null, modalStack: [], undoDeadlineAt: null } },
]

function StoreProbe({ store }: { store: AnyStore }) {
  const state = store((s: Record<string, unknown>) => s)
  return <div data-testid="store-probe">{JSON.stringify(state)}</div>
}

function renderWithProviders(ui: ReactNode) {
  return render(<AppProviders>{ui}</AppProviders>)
}

describe('Zustand store 空骨架（票 02，技术方案第 8 章）', () => {
  it('store 清单齐备：6 个命名与第 8 章一一对应', () => {
    expect(STORE_CASES.map((c) => c.name).sort()).toStrictEqual([
      'editSessionStore',
      'offlineQueueStore',
      'presenceStore',
      'selectionStore',
      'uiStore',
      'viewConfigDraftStore',
    ])
  })

  it.each(STORE_CASES)('$name 可被任意组件消费到初始状态（无需 Provider）', ({ store, expected }: StoreCase) => {
    render(<StoreProbe store={store} />)

    expect(JSON.parse(screen.getByTestId('store-probe').textContent ?? '{}')).toEqual(expected)
  })

  it.each(STORE_CASES)('$name 在 providers 装配下同样可消费', ({ store, expected }: StoreCase) => {
    renderWithProviders(<StoreProbe store={store} />)

    expect(JSON.parse(screen.getByTestId('store-probe').textContent ?? '{}')).toEqual(expected)
  })
})

describe('providers 装配（票 02）', () => {
  it('QueryClient 挂根：子树组件可消费同一 client 实例', () => {
    let observed: QueryClient | null = null

    function Capture() {
      observed = useQueryClient()
      return <div data-testid="query-probe">ok</div>
    }

    renderWithProviders(<Capture />)

    expect(observed).toBeInstanceOf(QueryClient)
    expect(screen.getByTestId('query-probe')).toHaveTextContent('ok')
  })
})
