// 底栏计数测试（票 05 / G-3）：展示已加载 n 条；has_more 时 n+，不虚构总数。
// 票 06 扩展：新增入口、批量条、5 万上限、删除 7 秒撤销窗口。
import '@testing-library/jest-dom/vitest'

import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { uiStore } from '@/stores'

import { WorkspaceFooter } from './WorkspaceFooter'

describe('WorkspaceFooter（票 05 G-3 口径）', () => {
  it('无更多页：已加载 n 条', () => {
    render(<WorkspaceFooter loadedCount={20} hasMore={false} />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 20 条')
  })

  it('仍有下一页：已加载 n+ 条', () => {
    render(<WorkspaceFooter loadedCount={20} hasMore />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 20+ 条')
  })

  it('默认 0 条（首屏未加载）', () => {
    render(<WorkspaceFooter />)
    expect(screen.getByTestId('footer-record-count')).toHaveTextContent('已加载 0 条')
  })
})

describe('底栏记录写入入口（票 06）', () => {
  afterEach(() => {
    uiStore.getState().clearUndoWindow()
    vi.useRealTimers()
  })

  it('可新增时展示入口；权限隐藏时不渲染', () => {
    const onCreateRecord = vi.fn()
    const { rerender } = render(
      <WorkspaceFooter canCreateRecord onCreateRecord={onCreateRecord} />,
    )
    fireEvent.click(screen.getByRole('button', { name: '+ 新增记录' }))
    expect(onCreateRecord).toHaveBeenCalledTimes(1)

    rerender(<WorkspaceFooter canCreateRecord={false} />)
    expect(screen.queryByRole('button', { name: '+ 新增记录' })).not.toBeInTheDocument()
  })

  it('已加载达到 5 万行：新增按钮禁用并提示联系管理者', () => {
    render(<WorkspaceFooter canCreateRecord recordLimitReached />)
    expect(screen.getByRole('button', { name: '+ 新增记录' })).toBeDisabled()
    expect(screen.getByText('已达单表 5 万行上限，请联系管理者')).toBeInTheDocument()
  })

  it('勾选行后出现批量条；删除需二次确认', () => {
    const onDeleteSelected = vi.fn()
    const onCopySelected = vi.fn()
    render(
      <WorkspaceFooter
        canCreateRecord
        canDeleteRecord
        selectedRecordIds={['rec_1', 'rec_2']}
        onDeleteSelected={onDeleteSelected}
        onCopySelected={onCopySelected}
      />,
    )

    expect(screen.getByLabelText('批量操作')).toHaveTextContent('已选择 2 条')
    fireEvent.click(screen.getByRole('button', { name: '复制记录' }))
    expect(onCopySelected).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/确定删除选中的 2 条记录/)).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(onDeleteSelected).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认删除' }))
    expect(onDeleteSelected).toHaveBeenCalledTimes(1)
  })

  it('删除后浮出「已删除 n 条 [撤销]」，撤销回调可用', () => {
    uiStore.getState().setUndoWindow({
      baseId: 'base_1',
      recordIds: ['rec_1', 'rec_2'],
      deadlineAt: Date.now() + 7000,
    })
    const onUndoDelete = vi.fn()
    const { rerender } = render(<WorkspaceFooter baseId="base_1" onUndoDelete={onUndoDelete} />)

    expect(screen.getByRole('status')).toHaveTextContent('已删除 2 条')
    fireEvent.click(screen.getByRole('button', { name: '撤销' }))
    expect(onUndoDelete).toHaveBeenCalledTimes(1)

    // 其他 Base 不显示该窗口
    rerender(<WorkspaceFooter baseId="base_2" />)
    expect(screen.queryByText('已删除 2 条')).not.toBeInTheDocument()
  })

  it('7 秒后撤销入口消失', () => {
    vi.useFakeTimers()
    uiStore.getState().setUndoWindow({
      baseId: 'base_1',
      recordIds: ['rec_1'],
      deadlineAt: Date.now() + 7000,
    })
    render(<WorkspaceFooter baseId="base_1" />)
    expect(screen.getByText('已删除 1 条')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(7000)
    })
    expect(screen.queryByText('已删除 1 条')).not.toBeInTheDocument()
  })
})
