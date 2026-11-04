// 行高模型：三档估算 + 实测校准（票 07，技术方案 6.1「行高三档 + 分组行实测缓存（估算→实测校准）」）。
// 语义唯一事实源：技术方案 6.1；需求文档 3.5 行高「紧凑 / 中等 / 高」三档。
// DOM 无关纯状态：估算值是「未实测行的乐观猜测」，实测值回写缓存后覆盖估算（实测是真相）。
// 校准收敛语义：估算偏小/偏大只影响首次切窗；实测回写后同偏移切窗随校准单调收敛
// （不跳行不重复由 windowing 的连续节结构保证，见 windowing.ts）。

import { buildPrefixOffsets } from './windowing'

/** 行高三档（需求文档 3.5：紧凑 / 中等 / 高）。 */
export const ROW_HEIGHT_TIERS = ['compact', 'medium', 'high'] as const
export type RowHeightTier = (typeof ROW_HEIGHT_TIERS)[number]

/** 三档估算行高（px）。事实源未给定具体像素值，取行业常规默认（32/40/48），构造时可整档覆写。 */
export const ROW_HEIGHT_ESTIMATE_PX: Record<RowHeightTier, number> = {
  compact: 32,
  medium: 40,
  high: 48,
}

export interface RowHeightModelOptions {
  /** 按行估算函数（分组行高与普通行不同的表在此注入；缺省为所在档常量估算）。 */
  readonly estimateFor?: (index: number) => number
  /** 估算档位像素覆写（仅影响未实测行的估算值）。 */
  readonly estimateOverrides?: Partial<Record<RowHeightTier, number>>
}

/**
 * 行高模型：估算（按档）+ 实测缓存（Map<rowIndex, measuredHeight>）。
 * 与 windowing 的契约：sizes(totalRows) 直接可作 computeWindow 的尺寸表输入。
 */
export class RowHeightModel {
  private tierValue: RowHeightTier
  private readonly estimates: Record<RowHeightTier, number>
  private readonly measured = new Map<number, number>()
  private readonly estimateFor: ((index: number) => number) | undefined

  constructor(tier: RowHeightTier = 'medium', options?: RowHeightModelOptions) {
    this.tierValue = tier
    this.estimates = { ...ROW_HEIGHT_ESTIMATE_PX, ...(options?.estimateOverrides ?? {}) }
    this.estimateFor = options?.estimateFor
  }

  get tier(): RowHeightTier {
    return this.tierValue
  }

  /** 三档行高即时切换（需求 3.5 / 票 09 演示）；已实测行保留实测值。 */
  setTier(tier: RowHeightTier): void {
    this.tierValue = tier
  }

  /** 未实测行的估算值：注入的按行估算函数优先（分组行），否则取所在档常量。 */
  estimateAt(index: number): number {
    if (this.estimateFor) {
      return this.estimateFor(index)
    }
    return this.estimates[this.tierValue]
  }

  /** 实测值回写缓存（渲染层量得真实行高后调用）；重复回写以最新为准。 */
  setMeasured(index: number, height: number): void {
    if (!Number.isFinite(height) || height <= 0) {
      throw new Error(`非法实测行高：index=${index}, height=${String(height)}`)
    }
    this.measured.set(index, height)
  }

  /** 该行是否已有实测值。 */
  isMeasured(index: number): boolean {
    return this.measured.has(index)
  }

  /** 实测缓存规模。 */
  measuredCount(): number {
    return this.measured.size
  }

  /** 行高取值：实测覆盖估算。 */
  heightAt(index: number): number {
    const measuredHeight = this.measured.get(index)
    return measuredHeight ?? this.estimateAt(index)
  }

  /** 输出前 totalRows 行的尺寸表（供 computeWindow 消费）。 */
  sizes(totalRows: number): number[] {
    const sizes: number[] = new Array(totalRows)
    for (let i = 0; i < totalRows; i += 1) {
      sizes[i] = this.heightAt(i)
    }
    return sizes
  }

  /** 前缀和（内容总高与各行起点偏移），供消费层跨帧缓存复用。 */
  prefixOffsets(totalRows: number): { readonly offsets: number[]; readonly totalSize: number } {
    return buildPrefixOffsets(this.sizes(totalRows))
  }
}
