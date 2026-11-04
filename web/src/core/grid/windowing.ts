// 虚拟滚动窗口计算（票 07，技术方案 6.1 自研虚拟滚动）。
// 语义唯一事实源：技术方案 6.1——行虚拟化（三档行高估算 + 实测校准、overscan 上下各 5 行）、
// 「横向滚动复用同一虚拟化引擎」（冻结列模块复用本函数做横向切窗）。
// DOM 无关纯函数：输入尺寸表与滚动偏移，输出可见节集合（含 overscan）——
// spec 好测试标准口径「给定数据窗口与滚动偏移，输出可见行集合」。
//
// 复杂度：每次调用 O(n) 构建前缀和 + O(log n) 二分定位。调用方（票 09 接线层）
// 在行高表未变化时应缓存前缀和结果、仅传偏移重算；本模块保持纯函数无内部状态。

/** 滚动帧预算（技术方案 6.1：16ms）。 */
// 消费策略（技术方案 6.1「超出降级策略：暂停 overscan 扩充」）由消费层（票 09/M2 渲染接线）
// 用本常量做帧内计时判断；M1 只落常量与语义锚点，不做降级实现。
export const FRAME_BUDGET_MS = 16

/** 默认 overscan：上下各 5 行（技术方案 6.1）。 */
export const DEFAULT_OVERSCAN = 5

/** 一个可见节（行或列）：在内容坐标中的索引、起点偏移与尺寸。 */
export interface WindowSection {
  readonly index: number
  /** 内容坐标中的起点偏移（= 前面所有节尺寸之和）。 */
  readonly offset: number
  readonly size: number
}

export interface ComputeWindowInput {
  /** 节尺寸表（行高表或列宽表；索引即行号/列号）。 */
  readonly sizes: readonly number[]
  /** 滚动偏移（纵向 scrollTop 或横向 scrollLeft）；负值按 0 钳制。 */
  readonly scrollOffset: number
  /** 视口尺寸（视口高或滚动区视口宽）；≤0 输出空窗口。 */
  readonly viewportSize: number
  /** overscan 行数，上下各该值；缺省 5（技术方案 6.1）。 */
  readonly overscan?: number
}

export interface WindowResult {
  /** 首个可见节索引（含 overscan）；空窗口为 -1。 */
  readonly start: number
  /** 末个可见节索引（含 overscan，闭区间）；空窗口为 -1。 */
  readonly end: number
  /** 不含 overscan 的首个真实可见节索引；空窗口为 -1。 */
  readonly contentStart: number
  /** 不含 overscan 的末个真实可见节索引；空窗口为 -1。 */
  readonly contentEnd: number
  /** 可见节集合（含 overscan，索引严格连续——不跳行不重复的结构保证）。 */
  readonly sections: readonly WindowSection[]
  /** 全部节尺寸之和（内容总高/总宽）。 */
  readonly totalSize: number
}

/** 空窗口（空表/零视口）。 */
const EMPTY_WINDOW: WindowResult = {
  start: -1,
  end: -1,
  contentStart: -1,
  contentEnd: -1,
  sections: [],
  totalSize: 0,
}

/** 前缀和：offsets[i] = 第 i 节起点偏移；末元素后追加总长，便于二分。 */
export function buildPrefixOffsets(sizes: readonly number[]): { readonly offsets: number[]; readonly totalSize: number } {
  const offsets: number[] = new Array(sizes.length)
  let acc = 0
  for (let i = 0; i < sizes.length; i += 1) {
    offsets[i] = acc
    acc += sizes[i]
  }
  return { offsets, totalSize: acc }
}

/** 第一个与 [viewStart, viewEnd) 相交的节索引（不存在返回 -1）。 */
function firstIntersecting(offsets: readonly number[], totalSize: number, viewStart: number): number {
  // 二分：找最后一个 offset <= viewStart 的节，再校正其是否与视口相交
  let low = 0
  let high = offsets.length - 1
  let candidate = -1
  while (low <= high) {
    const mid = (low + high) >> 1
    if (offsets[mid] <= viewStart) {
      candidate = mid
      low = mid + 1
    } else {
      high = mid - 1
    }
  }
  if (candidate === -1) {
    return offsets.length > 0 ? 0 : -1 // viewStart 落在首节之前
  }
  // candidate 起点 <= viewStart：若其底沿 <= viewStart 则视口从下一节开始
  const end = candidate + 1 < offsets.length ? offsets[candidate + 1] : totalSize
  if (end <= viewStart) {
    return candidate + 1 < offsets.length ? candidate + 1 : -1
  }
  return candidate
}

/**
 * 给定尺寸表与滚动偏移，输出可见节集合（含 overscan）。
 * 行虚拟化与冻结列横向虚拟化共用本引擎（技术方案 6.1「横向滚动复用同一虚拟化引擎」）。
 */
export function computeWindow(input: ComputeWindowInput): WindowResult {
  const { sizes, scrollOffset, viewportSize } = input
  const overscan = input.overscan ?? DEFAULT_OVERSCAN
  if (sizes.length === 0 || viewportSize <= 0) {
    return EMPTY_WINDOW
  }
  const { offsets, totalSize } = buildPrefixOffsets(sizes)
  if (totalSize <= 0) {
    return EMPTY_WINDOW
  }
  // 偏移钳制到合法滚动区间 [0, max(0, totalSize - viewportSize)]：
  // 越界滚动（惯性过冲/负偏移）输出钳制后的窗口，与顶/底边界语义一致
  const clampedOffset = Math.min(Math.max(0, scrollOffset), Math.max(0, totalSize - viewportSize))
  const viewEnd = clampedOffset + viewportSize
  const contentStart = firstIntersecting(offsets, totalSize, clampedOffset)
  if (contentStart === -1) {
    // 防御分支：钳制后仍无相交节（理论上不可达），输出末尾窗口
    const last = sizes.length - 1
    const start = Math.max(0, last - overscan)
    return makeWindow(sizes, offsets, totalSize, start, last, last, last)
  }
  // 内容末节：第一个底沿 >= 视口底的节
  let contentEnd = contentStart
  while (contentEnd < sizes.length - 1 && offsets[contentEnd] + sizes[contentEnd] < viewEnd) {
    contentEnd += 1
  }
  const start = Math.max(0, contentStart - Math.max(0, overscan))
  const end = Math.min(sizes.length - 1, contentEnd + Math.max(0, overscan))
  return makeWindow(sizes, offsets, totalSize, start, end, contentStart, contentEnd)
}

function makeWindow(
  sizes: readonly number[],
  offsets: readonly number[],
  totalSize: number,
  start: number,
  end: number,
  contentStart: number,
  contentEnd: number,
): WindowResult {
  const sections: WindowSection[] = []
  for (let i = start; i <= end; i += 1) {
    sections.push({ index: i, offset: offsets[i], size: sizes[i] })
  }
  return { start, end, contentStart, contentEnd, sections, totalSize }
}
