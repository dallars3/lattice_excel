import '@testing-library/jest-dom/vitest'

import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { TableSummary } from '../api/table'
import { TableTabs } from './TableTabs'

// AC-TBL-02（票 04）：最后一张表的「删除表」入口必须禁用且提示原因，
// 不能出现「点击后无弹窗无反馈」的静默失败。

function table(id: string, name: string): TableSummary {
  return {
    id,
    baseId: 'base_1',
    name,
    fieldCount: 1,
    viewCount: 1,
    createdAt: 1730000000000,
  }
}

function renderTabs(tables: TableSummary[]) {
  return render(
    <TableTabs
      tables={tables}
      currentTableId={tables[0]?.id ?? null}
      canManageStructure
      onSelect={vi.fn()}
      onCreate={vi.fn().mockResolvedValue(undefined)}
      onRename={vi.fn().mockResolvedValue(undefined)}
      onDuplicate={vi.fn().mockResolvedValue(undefined)}
      onDelete={vi.fn().mockResolvedValue(undefined)}
    />,
  )
}

function openTableMenu(name: string) {
  fireEvent.click(screen.getByRole('button', { name: `表操作：${name}` }))
}

describe('TableTabs 删除表入口（票 04 / AC-TBL-02）', () => {
  it('仅剩最后一张表时「删除表」禁用，且菜单内出现「至少保留一张表」可见提示', () => {
    renderTabs([table('tbl_last', '唯一表')])

    openTableMenu('唯一表')

    const deleteItem = screen.getByRole('menuitem', { name: '删除表' })
    expect(deleteItem).toBeDisabled()
    // 提示必须是可见文本（不是只在 a11y 树里）。
    expect(screen.getByText('至少保留一张表')).toBeInTheDocument()
  })

  it('存在多张表时「删除表」可点且不显示保留提示', () => {
    renderTabs([table('tbl_a', '数据表 A'), table('tbl_b', '数据表 B')])

    openTableMenu('数据表 A')

    expect(screen.getByRole('menuitem', { name: '删除表' })).toBeEnabled()
    expect(screen.queryByText('至少保留一张表')).not.toBeInTheDocument()
  })
})
