// loadedRanges：已加载窗口区间缓存（票 07，技术方案 6.1「loadedRanges: Map<cursor, Page>」）。
// 语义唯一事实源：
// - 技术方案 6.1：grid 只消费「可见行窗口 + 各 cursor 区间缓存」；筛选/排序/分组变更时清空并
//   重置 cursor（ADR 0002 唯一全量重拉场景）。
// - 技术方案 12 章内存策略：loadedRanges 上限（≈10 页）按 LRU 淘汰。
// DOM 无关纯状态：insert/get 触及刷新新鲜度（has 只查询不触及）；超上限淘汰最久未触及页。
// 游标分页语义对齐票 04 cursor helper（cursor/limit/has_more/next_cursor），本模块只管缓存与淘汰。

/** 一页已加载区间：cursor 键 + 该页覆盖的行区间（闭区间）+ 行序记录 id。 */
export interface GridPage {
  /** 服务端游标（Map<cursor, Page> 的键；票 04 游标分页事实源）。 */
  readonly cursor: string
  /** 该页起始行号（视图当前排序下的行序，0 起）。 */
  readonly startIndex: number
  /** 该页末行号（闭区间）。 */
  readonly endIndex: number
  /** 该页记录 id 序列（与行序一致）。 */
  readonly recordIds: readonly string[]
}

/** 默认 LRU 上限：10 页（技术方案 12 章「≈10 页」）。 */
export const DEFAULT_LOADED_RANGES_CAPACITY = 10

export interface LoadedRangeCacheOptions {
  /** LRU 上限（页数）；缺省 10，<=0 显式拒绝。 */
  readonly capacity?: number
}

/**
 * 已加载窗口区间缓存：Map<cursor, Page> 语义 + LRU 淘汰。
 * - put（insert 或更新既有 cursor）触及刷新新鲜度；
 * - get 命中触及刷新新鲜度；has 只查询不触及；
 * - 插入新 cursor 超上限时淘汰最久未触及页，被淘汰页完整记入 evicted 日志（观测通道，内容不静默丢弃）；
 * - clear 供筛选/排序/分组变更时全量清空（不计入 LRU 淘汰日志）。
 */
export class LoadedRangeCache {
  private readonly capacity: number
  /** Map 迭代序即新鲜度序（最旧 → 最新）；触及通过 delete+set 移到末尾。 */
  private readonly pages = new Map<string, GridPage>()
  /** LRU 淘汰日志（累计、只增；clear 不写入）。 */
  readonly evicted: GridPage[] = []

  constructor(options?: LoadedRangeCacheOptions) {
    const capacity = options?.capacity ?? DEFAULT_LOADED_RANGES_CAPACITY
    if (!Number.isInteger(capacity) || capacity <= 0) {
      throw new Error(`非法 loadedRanges 上限：${String(capacity)}`)
    }
    this.capacity = capacity
  }

  get size(): number {
    return this.pages.size
  }

  /** 插入或更新一页：触及（刷新为最新）；新 cursor 超上限时先淘汰最久未触及页。 */
  put(page: GridPage): void {
    if (this.pages.has(page.cursor)) {
      this.pages.delete(page.cursor) // 更新既有页：先移除再插入到末尾（刷新新鲜度），不引发淘汰
    } else if (this.pages.size >= this.capacity) {
      const oldestCursor = this.pages.keys().next().value
      if (oldestCursor !== undefined) {
        const evictedPage = this.pages.get(oldestCursor)
        this.pages.delete(oldestCursor)
        if (evictedPage) {
          this.evicted.push(evictedPage)
        }
      }
    }
    this.pages.set(page.cursor, page)
  }

  /** 取回一页；命中即触及（刷新为最新）。未命中返回 undefined。 */
  get(cursor: string): GridPage | undefined {
    const page = this.pages.get(cursor)
    if (page !== undefined) {
      this.pages.delete(cursor)
      this.pages.set(cursor, page)
    }
    return page
  }

  /** 是否已加载该 cursor（只查询，不触及、不改变新鲜度）。 */
  has(cursor: string): boolean {
    return this.pages.has(cursor)
  }

  /** 全部 cursor 按新鲜度序（最旧 → 最新），供断言与调试观测。 */
  keysByRecency(): string[] {
    return [...this.pages.keys()]
  }

  /** 全量清空（筛选/排序/分组变更时调用；不计入 LRU 淘汰日志）。 */
  clear(): void {
    this.pages.clear()
  }
}
