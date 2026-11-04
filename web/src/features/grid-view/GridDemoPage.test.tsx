import '@testing-library/jest-dom/vitest'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { mockCursorRecords, mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'

import { GridDemoPage } from './GridDemoPage'
import { buildDemoRows, DEMO_RECORDS_PATH, DEMO_ROW_TOTAL } from './demo-table'
import styles from './GridDemoPage.module.css'

// 票 09 演示页：MSW 假接口是唯一 HTTP 边界（spec Testing Decisions），用例经真实
// cursor/limit/has_more 语义驱动页面，全程不依赖手工操作。
function renderDemoPage(): ReturnType<typeof render> {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <MemoryRouter initialEntries={['/demo/grid']}>
      <QueryClientProvider client={queryClient}>
        <GridDemoPage />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

function getViewport(): HTMLElement {
  return screen.getByTestId('demo-grid-viewport')
}

function renderedRowCount(container: HTMLElement): number {
  return container.querySelectorAll('[data-row-index]').length
}

function scrollViewportTo(top: number, left = 0): void {
  const viewport = getViewport()
  viewport.scrollTop = top
  viewport.scrollLeft = left
  fireEvent.scroll(viewport)
}

describe('GridDemoPage 游标窗口加载（票 09 AC1）', () => {
  it('首屏经 MSW 游标假接口加载第一页（limit 20），窗口内渲染 20 行', async () => {
    const seenCursors: (string | null)[] = []
    const seenLimits: string[] = []
    mswServer.use(
      mockCursorRecords({
        path: DEMO_RECORDS_PATH,
        rows: buildDemoRows(DEMO_ROW_TOTAL),
        onRequest: (request) => {
          const url = new URL(request.url)
          seenCursors.push(url.searchParams.get('cursor'))
          seenLimits.push(url.searchParams.get('limit') ?? '')
        },
      }),
    )
    const { container } = renderDemoPage()

    await screen.findByText('记录 1')
    expect(screen.getByText('记录 20')).toBeInTheDocument()
    expect(screen.queryByText('记录 21')).not.toBeInTheDocument()
    expect(renderedRowCount(container)).toBe(20)
    expect(seenCursors[0]).toBeNull()
    expect(seenLimits[0]).toBe('20')
    // loadedRanges LRU 消费可观测：页缓存计数与行数
    expect(screen.getByTestId('demo-load-status')).toHaveTextContent('已加载 20 行')
    expect(screen.getByTestId('demo-load-status')).toHaveTextContent('缓存 1 页')
  })

  it('滚动到底触发 fetchNextPage 加载新窗口；cursor 按 next_cursor 链推进', async () => {
    const seenCursors: (string | null)[] = []
    mswServer.use(
      mockCursorRecords({
        path: DEMO_RECORDS_PATH,
        rows: buildDemoRows(DEMO_ROW_TOTAL),
        onRequest: (request) => {
          seenCursors.push(new URL(request.url).searchParams.get('cursor'))
        },
      }),
    )
    const { container } = renderDemoPage()
    await screen.findByText('记录 1')

    // 滚动到已加载内容底部附近 → 第 2 页（cursor=cur_20）
    scrollViewportTo(1500)
    await screen.findByText('记录 21')
    expect(screen.getByText('记录 40')).toBeInTheDocument()
    expect(seenCursors[1]).toBe('cur_20')

    // 再次滚动 → 第 3 页（cursor=cur_40），has_more=false 终止，不再发请求
    scrollViewportTo(1500)
    await screen.findByText('记录 41')
    expect(screen.getByText('记录 58')).toBeInTheDocument()
    expect(screen.queryByText('记录 60')).not.toBeInTheDocument() // 窗口外不渲染（overscan 边界）
    expect(seenCursors[2]).toBe('cur_40')
    expect(seenCursors).toHaveLength(3)
    // 纵向虚拟化：scrollTop 1500 时窗口仅覆盖视口上下 overscan 内的行（约 32..57），非全量渲染
    expect(renderedRowCount(container)).toBe(26)
    expect(screen.getByTestId('demo-load-status')).toHaveTextContent('已加载 60 行')
    expect(screen.getByTestId('demo-load-status')).toHaveTextContent('缓存 3 页')
  })
})

describe('GridDemoPage 三渲染器可见（票 09 AC4：经注册表取实现）', () => {
  beforeEach(() => {
    mswServer.use(mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }))
  })

  it('text 列：单行省略渲染且全量值经 title 可获取', async () => {
    renderDemoPage()
    const cell = await screen.findByTestId('cell-0-0')
    expect(cell).toHaveTextContent('记录 1')
    expect(within(cell).getByTitle('记录 1')).toBeInTheDocument()
  })

  it('number 列：千分位 + 小数位按字段配置渲染（数量 1,500 / 金额 12,000.25）', async () => {
    renderDemoPage()
    expect(await screen.findByTestId('cell-11-1')).toHaveTextContent('1,500')
    expect(screen.getByTestId('cell-11-2')).toHaveTextContent('12,000.25')
  })

  it('select 列：选项标签按配置色渲染（色值来自字段配置注入，非组件写死）', async () => {
    renderDemoPage()
    const cell = await screen.findByTestId('cell-0-3')
    const tag = within(cell).getByText('进行中')
    expect(tag.style.backgroundColor).toBe('var(--presence-color-1)')
  })
})

describe('GridDemoPage 错误态消费 error-catalog（票 04 缝）', () => {
  it('非 0 信封码 → 横幅展示目录文案与动作', async () => {
    mswServer.use(mockEnvelope({ path: DEMO_RECORDS_PATH, code: 5001 }))
    renderDemoPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('已达容量上限')
    expect(alert).toHaveTextContent('请清理不需要的数据后重试，或联系管理员')
  })
})

describe('GridDemoPage 页面骨架', () => {
  beforeEach(() => {
    mswServer.use(mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }))
  })

  it('渲染标题、列头与网格视口（role=grid）', async () => {
    const { container } = renderDemoPage()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('网格演示')
    expect(screen.getByTestId('demo-grid-viewport')).toBeInTheDocument()
    await screen.findByText('记录 1')
    expect(within(screen.getByTestId('demo-grid-header')).getByText('名称')).toBeInTheDocument()
    expect(within(screen.getByTestId('demo-grid-header')).getByText('金额')).toBeInTheDocument()
    expect(container.querySelector('[role="grid"]')).not.toBeNull()
  })
})

describe('GridDemoPage 键盘全程（票 09 AC2：移动 → 编辑 → 提交/取消）', () => {
  beforeEach(() => {
    mswServer.use(mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }))
  })

  it('方向键移动选中；F2 进入编辑；Enter 提交下移；Esc 取消不落盘；Tab 提交右移', async () => {
    renderDemoPage()
    await screen.findByText('记录 1')

    // 点击选中 (0,0)：焦点随点击回到网格视口（真实浏览器按 DOM 焦点路由键事件）
    fireEvent.click(screen.getByTestId('cell-0-0'))
    expect(screen.getByTestId('cell-0-0')).toHaveClass(styles['demo-cell-focused'])
    expect(screen.getByTestId('demo-grid-viewport')).toHaveFocus()

    // 方向键移动选中 → (0,1)
    fireEvent.keyDown(getViewport(), { key: 'ArrowRight' })
    expect(screen.getByTestId('cell-0-1')).toHaveClass(styles['demo-cell-focused'])
    expect(screen.getByTestId('cell-0-0')).not.toHaveClass(styles['demo-cell-focused'])

    // 回到 (0,0)（text 列）走编辑全程
    fireEvent.keyDown(getViewport(), { key: 'ArrowLeft' })
    fireEvent.keyDown(getViewport(), { key: 'F2' })
    const input = screen.getByRole('textbox') as HTMLInputElement
    expect(input).toBeInTheDocument()
    expect(input.value).toBe('记录 1')

    // Enter 提交并下移
    fireEvent.change(input, { target: { value: '键盘改名' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.queryByRole('textbox')).not.toBeInTheDocument())
    expect(screen.getByTestId('cell-0-0')).toHaveTextContent('键盘改名')
    expect(screen.getByTestId('cell-1-0')).toHaveClass(styles['demo-cell-focused'])
    expect(screen.getByTestId('demo-grid-viewport')).toHaveFocus() // 编辑器卸载后焦点回到视口

    // Esc 取消不落盘
    fireEvent.keyDown(getViewport(), { key: 'F2' })
    const draft = screen.getByRole('textbox') as HTMLInputElement
    fireEvent.change(draft, { target: { value: '不落盘草稿' } })
    fireEvent.keyDown(draft, { key: 'Escape' })
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByTestId('cell-1-0')).toHaveTextContent('记录 2')

    // Tab 提交右移
    fireEvent.keyDown(getViewport(), { key: 'F2' })
    const tabInput = screen.getByRole('textbox') as HTMLInputElement
    fireEvent.change(tabInput, { target: { value: 'Tab 提交' } })
    fireEvent.keyDown(tabInput, { key: 'Tab' })
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByTestId('cell-1-0')).toHaveTextContent('Tab 提交')
    expect(screen.getByTestId('cell-1-1')).toHaveClass(styles['demo-cell-focused'])
  })
})

describe('GridDemoPage 冻结列 + 三档行高（票 09 AC3）', () => {
  beforeEach(() => {
    mswServer.use(mockCursorRecords({ path: DEMO_RECORDS_PATH, rows: buildDemoRows(DEMO_ROW_TOTAL) }))
  })

  it('开启冻结首列后：固定区 sticky 通道常驻，滚动区横向窗口随 scrollLeft 推进', async () => {
    const { container } = renderDemoPage()
    await screen.findByText('记录 1')

    fireEvent.click(screen.getByRole('checkbox', { name: '冻结首列' }))
    const frozenCell = screen.getByTestId('cell-0-0')
    expect(frozenCell).toHaveClass(styles['demo-cell-frozen'])

    // 横向滚动：滚动区窗口推进（滚动区首列移出、末列进入），冻结列常驻
    scrollViewportTo(0, 700)
    await waitFor(() => {
      expect(screen.queryByTestId('cell-0-1')).not.toBeInTheDocument()
      expect(screen.getByTestId('cell-0-9')).toBeInTheDocument()
    })
    expect(screen.getByTestId('cell-0-0')).toBeInTheDocument()
    expect(screen.getByTestId('cell-0-0')).toHaveClass(styles['demo-cell-frozen'])
    expect(container.querySelectorAll('[data-row-index]')).not.toHaveLength(0)
  })

  it('行高三档即时切换：紧凑/中等/宽松', async () => {
    const { container } = renderDemoPage()
    await screen.findByText('记录 1')
    const firstRow = container.querySelector<HTMLElement>('[data-row-index="0"]')
    expect(firstRow?.style.height).toBe('40px')

    fireEvent.click(screen.getByRole('radio', { name: '紧凑' }))
    expect(firstRow?.style.height).toBe('32px')

    fireEvent.click(screen.getByRole('radio', { name: '宽松' }))
    expect(firstRow?.style.height).toBe('48px')

    fireEvent.click(screen.getByRole('radio', { name: '中等' }))
    expect(firstRow?.style.height).toBe('40px')
  })
})
