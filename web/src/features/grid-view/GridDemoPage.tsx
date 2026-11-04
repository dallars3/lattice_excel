// 网格演示页（票 09）：grid 核心逻辑的消费证明页——
// windowing 纵向虚拟窗口 / row-height 三档行高 / frozen-columns sticky 双通道 /
// loaded-ranges LRU 页缓存 / selection+keyboard 状态机 / registry 渲染器与编辑器注册表。
//
// dev 专用语义（M1 定位）：路由恒注册（生产亦可访问），但数据仅来自 MSW 假接口——
// worker 仅 dev 启用（src/mocks/browser.ts + main.tsx 的 DEV 门），生产环境请求不可达，
// 页面按 error-catalog 呈现错误态横幅。M2 起真实网格接入 /base/:id 工作台，本页保留作能力演示。
// 键盘全程（需求 3.8.1）：方向移动选中 → F2/Enter 进入编辑 → Enter 提交下移 / Tab 提交右移 /
// Esc 取消不落盘 / Delete 清空；commit 信号的焦点推进统一回键位状态机裁决。
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, UIEvent } from 'react'
import { Link } from 'react-router-dom'

import {
  GRID_KEYS,
  LoadedRangeCache,
  ROW_HEIGHT_TIERS,
  RowHeightModel,
  applyKey,
  computeColumnLayout,
  computeWindow,
  createCellRegistry,
  initialKeyboardState,
  registerDemoCells,
  requestEdit,
  selectCell,
  selectionRect,
  withSelection,
  type CellRef,
  type EditorCommitMove,
  type GridBounds,
  type GridKey,
  type GridKeyEvent,
  type GridKeyEffect,
  type KeyboardState,
  type RowHeightTier,
} from '@/core/grid'
import { isApiError } from '@/core/http'
import { Banner, EmptyState } from '@/ui-kit'

import { DEMO_COLUMNS, type DemoColumn } from './demo-table'
import { useDemoRecords } from './useDemoRecords'
import styles from './GridDemoPage.module.css'

/** 视口兜底尺寸：jsdom/无布局环境（clientWidth/Height 为 0）时供窗口计算；真实浏览器恒走实测值。 */
const FALLBACK_VIEWPORT = { width: 800, height: 600 }
/** 演示仅 10 列，横向 overscan 取 2 让窗口推进可观测；纵向保持默认 5（技术方案 6.1）。 */
const DEMO_COLUMN_OVERSCAN = 2
/** 距已加载底部剩余行数 ≤ 该值时预取下一页。 */
const FETCH_AHEAD_ROWS = 5

const ROW_TIER_LABELS: Record<RowHeightTier, string> = { compact: '紧凑', medium: '中等', high: '宽松' }

function cx(...parts: ReadonlyArray<string | false | undefined>): string {
  return parts.filter((part) => part !== undefined && part !== '').join(' ')
}

function cellKey(row: number, col: number): string {
  return `${row}:${col}`
}

export function GridDemoPage() {
  const query = useDemoRecords()

  // CellRenderer/CellEditor 注册表（票 08）：演示实现经 registerDemoCells 注册，未知类型走兜底。
  const registry = useMemo(() => {
    const instance = createCellRegistry()
    registerDemoCells(instance)
    return instance
  }, [])

  // loadedRanges（票 07 LRU）消费：每页到达即登记 cursor 区间；LRU 上限与淘汰数在状态栏可观测。
  const rangeCache = useRef(new LoadedRangeCache())
  const gridData = useMemo(() => {
    const pages = query.data?.pages ?? []
    const rows = pages.flatMap((page) => page.items)
    let acc = 0
    pages.forEach((page, index) => {
      rangeCache.current.put({
        cursor: String(query.data?.pageParams[index] ?? ''),
        startIndex: acc,
        endIndex: acc + page.items.length - 1,
        recordIds: page.items.map((row) => row.id),
      })
      acc += page.items.length
    })
    return { rows, pageCount: pages.length, evictedCount: rangeCache.current.evicted.length }
  }, [query.data])
  const rows = gridData.rows

  // 键盘状态机（票 07）：页面持有状态，DOM 事件经 applyKey 迁移，迁移产物驱动渲染。
  const [kb, setKb] = useState<KeyboardState>(initialKeyboardState)
  // 编辑提交值覆盖层（演示数据本地提交；真实记录写入随 M2 服务接入）
  const [cellOverrides, setCellOverrides] = useState<Record<string, unknown>>({})
  // 三档行高（需求 3.5）/ 冻结列开关（技术方案 6.1 sticky 双通道）
  const [tier, setTier] = useState<RowHeightTier>('medium')
  const [frozenCount, setFrozenCount] = useState(0)

  const viewportRef = useRef<HTMLDivElement | null>(null)
  const [measured, setMeasured] = useState({ width: 0, height: 0 })
  const [scrollTop, setScrollTop] = useState(0)
  const [scrollLeft, setScrollLeft] = useState(0)

  useLayoutEffect(() => {
    const el = viewportRef.current
    if (el === null) return undefined
    const measure = () => setMeasured({ width: el.clientWidth, height: el.clientHeight })
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [])

  const viewportWidth = measured.width > 0 ? measured.width : FALLBACK_VIEWPORT.width
  const viewportHeight = measured.height > 0 ? measured.height : FALLBACK_VIEWPORT.height

  const bounds: GridBounds = { rowCount: rows.length, colCount: DEMO_COLUMNS.length }
  const rowModel = useMemo(() => new RowHeightModel(tier), [tier])
  const rowSizes = useMemo(() => rowModel.sizes(rows.length), [rowModel, rows.length])
  const rowWindow = useMemo(
    () => computeWindow({ sizes: rowSizes, scrollOffset: scrollTop, viewportSize: viewportHeight }),
    [rowSizes, scrollTop, viewportHeight],
  )
  const columnWidths = useMemo(() => DEMO_COLUMNS.map((column) => column.width), [])
  const columnLayout = useMemo(
    () =>
      computeColumnLayout({
        columnWidths,
        frozenCount,
        scrollLeft,
        viewportWidth,
        overscan: DEMO_COLUMN_OVERSCAN,
      }),
    [columnWidths, frozenCount, scrollLeft, viewportWidth],
  )

  // 焦点行滚动跟随：窗口外行不渲染，键盘移动后把焦点行带回窗口（横向列少，不做横向跟随）
  useEffect(() => {
    if (kb.focus === null) return
    const el = viewportRef.current
    if (el === null) return
    const row = kb.focus.row
    if (row < 0 || row >= rowSizes.length) return
    let top = 0
    for (let i = 0; i < row; i += 1) top += rowSizes[i] ?? 0
    const bottom = top + (rowSizes[row] ?? 0)
    if (top < el.scrollTop) {
      el.scrollTop = top
      setScrollTop(top)
    } else if (bottom > el.scrollTop + viewportHeight) {
      const next = bottom - viewportHeight
      el.scrollTop = next
      setScrollTop(next)
    }
  }, [kb, rowSizes, viewportHeight])

  // 滚动 → 纵横窗口重算 + 距底部预取下一页（fetchNextPage 仅由滚动事件触发，首屏不自动追页）
  const loadRef = useRef({ rowCount: 0, hasNextPage: false, fetchingNext: false, sizes: [] as number[], viewportH: 0 })
  loadRef.current = {
    rowCount: rows.length,
    hasNextPage: query.hasNextPage,
    fetchingNext: query.isFetchingNextPage,
    sizes: rowSizes,
    viewportH: viewportHeight,
  }

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    const el = event.currentTarget
    setScrollTop(el.scrollTop)
    setScrollLeft(el.scrollLeft)
    const snapshot = loadRef.current
    const win = computeWindow({ sizes: snapshot.sizes, scrollOffset: el.scrollTop, viewportSize: snapshot.viewportH })
    if (
      win.contentEnd >= 0 &&
      win.contentEnd >= snapshot.rowCount - FETCH_AHEAD_ROWS &&
      snapshot.hasNextPage &&
      !snapshot.fetchingNext
    ) {
      void query.fetchNextPage()
    }
  }

  // 键盘副作用信号消费（键位状态机只发信号，真实动作在消费层）
  const consumeEffect = (effect: GridKeyEffect) => {
    switch (effect.kind) {
      case 'clearCells': {
        // Delete：选区矩形内单元格清空（演示数据本地提交）
        const rect = effect.rect
        setCellOverrides((prev) => {
          const next = { ...prev }
          for (let r = rect.top; r <= rect.bottom; r += 1) {
            for (let c = rect.left; c <= rect.right; c += 1) {
              next[cellKey(r, c)] = ''
            }
          }
          return next
        })
        break
      }
      case 'beginEdit':
        break // 迁移已进入 editing；编辑器由渲染层按焦点格挂载
      case 'commit':
      case 'cancel':
        break // 编辑态提交/取消经编辑器 onCommit/onReject 走 handleEditorCommit/Reject，容器不重复消费
      case 'appendRow':
      case 'clipboard':
      case 'undo':
      case 'find':
        break // M1 信号位：新增记录/剪贴板 TSV/撤销/查找随 M2 落地
      default:
        break
    }
  }

  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (kb.mode === 'editing') {
      return // 编辑态三键归编辑器（resolveEditorKey）处理，容器不重复消费
    }
    if (!(GRID_KEYS as readonly string[]).includes(event.key)) {
      return
    }
    event.preventDefault()
    const keyEvent: GridKeyEvent = {
      key: event.key as GridKey,
      ctrl: event.ctrlKey || event.metaKey,
      shift: event.shiftKey,
    }
    const { state, effects } = applyKey(kb, keyEvent, bounds)
    setKb(state)
    effects.forEach(consumeEffect)
  }

  // 键盘焦点归位：单元格/编辑器均非可聚焦元素，提交/取消/点击后把焦点移回网格视口，
  // 后续键事件才能继续到达 handleKeyDown（真实浏览器按 DOM 焦点路由，RTL 直发事件不走此路径）。
  const focusViewport = () => {
    viewportRef.current?.focus()
  }

  const handleEditorCommit = (value: unknown, move: EditorCommitMove) => {
    const focus = kb.focus
    if (focus !== null) {
      setCellOverrides((prev) => ({ ...prev, [cellKey(focus.row, focus.col)]: value }))
    }
    // 焦点推进语义统一由键位状态机裁决：按提交移动语义还原对应键事件，取迁移目标态
    // （编辑器已产出提交值，此处迁移的 commit 信号不重复消费）
    const transition = applyKey(kb, move === 'down' ? { key: 'Enter' } : { key: 'Tab', shift: move === 'left' }, bounds)
    setKb(transition.state)
    focusViewport() // 编辑器随迁移卸载，焦点立即回到视口，连续键入不中断
  }

  const handleEditorReject = () => {
    // cancel 迁移：丢弃草稿、不落盘（AC-REC-02）
    setKb(applyKey(kb, { key: 'Escape' }, bounds).state)
    focusViewport()
  }

  const handleCellClick = (cell: CellRef) => {
    // 点击选中：SelectionModel 产选择、withSelection 并回键盘态；点击即离开编辑态（草稿丢弃）
    setKb((current) => withSelection({ mode: 'idle', focus: current.focus, anchor: current.anchor }, selectCell(cell, bounds)))
    focusViewport()
  }

  const handleCellDoubleClick = (cell: CellRef) => {
    // 双击进入编辑（需求 3.8.2 三通道之一）：与 F2 同一 beginEdit 迁移
    setKb(requestEdit(kb, cell, bounds).state)
  }

  // 覆盖层键 = row:col（与键盘 clearCells 信号同键口径）；列键仅用于未覆盖时的取值回源。
  const cellValue = (row: number, col: number, columnKey: string): unknown => {
    const override = cellOverrides[cellKey(row, col)]
    if (override !== undefined) {
      return override
    }
    return rows[row]?.cells_map?.[columnKey] ?? null
  }

  const editingCell: CellRef | null = kb.mode === 'editing' ? kb.focus : null
  const activeRect =
    kb.focus === null ? null : selectionRect({ kind: 'range', anchor: kb.anchor ?? kb.focus, focus: kb.focus })

  const isFocused = (row: number, col: number): boolean => kb.focus?.row === row && kb.focus?.col === col
  const isCellSelected = (row: number, col: number): boolean =>
    activeRect !== null &&
    row >= activeRect.top &&
    row <= activeRect.bottom &&
    col >= activeRect.left &&
    col <= activeRect.right

  const editorHostRef = useRef<HTMLSpanElement | null>(null)
  useEffect(() => {
    if (kb.mode !== 'editing') return
    editorHostRef.current?.querySelector('input')?.focus()
  }, [kb])

  const renderEditor = (row: number, col: number, column: DemoColumn) => {
    const editor = registry.queryEditor(column.fieldType)
    const Editor = editor.component
    return (
      <span ref={editorHostRef} className={styles['demo-cell-editor']}>
        <Editor value={cellValue(row, col, column.key)} config={column.config} onCommit={handleEditorCommit} onReject={handleEditorReject} />
      </span>
    )
  }

  const renderCell = (row: number, col: number, left: number, width: number, column: DemoColumn, frozen: boolean) => {
    const renderer = registry.queryRenderer(column.fieldType)
    const Cell = renderer.component
    const focused = isFocused(row, col)
    const editing = editingCell !== null && editingCell.row === row && editingCell.col === col
    const alignClass =
      renderer.align === 'right'
        ? styles['demo-cell--right']
        : renderer.align === 'center'
          ? styles['demo-cell--center']
          : undefined
    return (
      <div
        key={col}
        role="gridcell"
        className={cx(
          styles['demo-cell'],
          alignClass,
          frozen ? styles['demo-cell-frozen'] : undefined,
          focused ? styles['demo-cell-focused'] : undefined,
          !focused && isCellSelected(row, col) ? styles['demo-cell-selected'] : undefined,
        )}
        style={{ left, width }}
        data-testid={`cell-${row}-${col}`}
        data-cell-col={col}
        onClick={() => handleCellClick({ row, col })}
        onDoubleClick={() => handleCellDoubleClick({ row, col })}
      >
        {editing ? renderEditor(row, col, column) : <Cell value={cellValue(row, col, column.key)} config={column.config} />}
      </div>
    )
  }

  const errorMessage = query.isError
    ? isApiError(query.error)
      ? `${query.error.catalog.title}——${query.error.catalog.action}`
      : String(query.error)
    : null

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>网格演示</h1>
      <p className={styles.meta}>
        dev 专用演示页（票 09）：MSW 假接口游标加载 + 虚拟滚动窗口 + 键盘导航 + 冻结列/行高三档 + 渲染器注册表演示。
        数据仅来自 dev 启用的 MSW worker，生产环境请求不可达；M2 起真实网格接入 Base 工作台。
      </p>
      {errorMessage === null ? null : <Banner tone="warning">{errorMessage}</Banner>}
      {rows.length === 0 && errorMessage === null ? (
        <EmptyState title="暂无记录" description="游标分页未返回任何记录" />
      ) : null}
      <div className={styles.toolbar}>
        <label className={styles.control}>
          <input
            type="checkbox"
            checked={frozenCount > 0}
            onChange={(event) => setFrozenCount(event.target.checked ? 1 : 0)}
          />
          冻结首列
        </label>
        <span className={styles.controlGroup} role="radiogroup" aria-label="行高三档">
          {ROW_HEIGHT_TIERS.map((candidate) => (
            <label key={candidate} className={styles.control}>
              <input
                type="radio"
                name="demo-row-height"
                value={candidate}
                checked={tier === candidate}
                onChange={() => setTier(candidate)}
              />
              {ROW_TIER_LABELS[candidate]}
            </label>
          ))}
        </span>
        <span className={styles.status} data-testid="demo-load-status">
          {`已加载 ${rows.length} 行 · 缓存 ${gridData.pageCount} 页 · LRU 淘汰 ${gridData.evictedCount} 页`}
        </span>
        <Link to="/home" className={styles.backLink}>
          返回首页
        </Link>
      </div>
      {/* 列头与主体同构双通道：固定区常驻，滚动区按横向窗口渲染并随 scrollLeft 反向平移对齐 */}
      <div className={styles.header} data-testid="demo-grid-header">
        <div className={styles.headerFrozen} style={{ width: columnLayout.frozenWidth }}>
          {columnLayout.frozen.map((col) => {
            const column = DEMO_COLUMNS[col.index]
            return column === undefined ? null : (
              <span key={col.index} className={styles['header-cell']} style={{ left: col.offset, width: col.width }}>
                {column.name}
              </span>
            )
          })}
        </div>
        <div className={styles.headerClip}>
          <div
            className={styles.headerTrack}
            style={{
              width: columnLayout.totalWidth - columnLayout.frozenWidth,
              transform: `translateX(${-scrollLeft}px)`,
            }}
          >
            {columnLayout.scrollable.map((col) => {
              const column = DEMO_COLUMNS[col.index]
              return column === undefined ? null : (
                <span key={col.index} className={styles['header-cell']} style={{ left: col.offset, width: col.width }}>
                  {column.name}
                </span>
              )
            })}
          </div>
        </div>
      </div>
      <div
        ref={viewportRef}
        className={styles.viewport}
        role="grid"
        aria-label="网格演示（假数据）"
        tabIndex={0}
        data-testid="demo-grid-viewport"
        onScroll={handleScroll}
        onKeyDown={handleKeyDown}
      >
        <div className={styles.content} style={{ width: columnLayout.totalWidth, height: rowWindow.totalSize }}>
          {rowWindow.sections.map((section) => {
            const row = rows[section.index]
            if (row === undefined) return null
            return (
              <div
                key={section.index}
                role="row"
                className={styles.row}
                data-row-index={section.index}
                style={{ top: section.offset, height: section.size, width: columnLayout.totalWidth }}
              >
                {/* 固定区通道：sticky 定位（left = 通道内偏移），完整输出不虚拟化 */}
                {columnLayout.frozen.map((col) => {
                  const column = DEMO_COLUMNS[col.index]
                  return column === undefined
                    ? null
                    : renderCell(section.index, col.index, col.offset, col.width, column, true)
                })}
                {/* 滚动区通道：横向虚拟化窗口，内容坐标定位（frozenWidth + 通道内偏移） */}
                {columnLayout.scrollable.map((col) => {
                  const column = DEMO_COLUMNS[col.index]
                  return column === undefined
                    ? null
                    : renderCell(section.index, col.index, columnLayout.frozenWidth + col.offset, col.width, column, false)
                })}
              </div>
            )
          })}
        </div>
      </div>
    </main>
  )
}
