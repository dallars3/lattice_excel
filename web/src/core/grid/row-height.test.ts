// 行高三档估算 + 实测校准表驱动测试（票 07，技术方案 6.1「行高三档 + 分组行实测缓存（估算→实测校准）」）。
// 收敛断言的事实源：完全校准后的窗口必须等于直接用实测真值表（字面量）切出的窗口。
import { describe, expect, it } from 'vitest'

import { computeWindow } from './windowing'
import {
  ROW_HEIGHT_ESTIMATE_PX,
  ROW_HEIGHT_TIERS,
  RowHeightModel,
} from './row-height'

describe('三档行高估算', () => {
  it('三档枚举：紧凑/中等/宽松，估算值互异且为正', () => {
    expect([...ROW_HEIGHT_TIERS]).toEqual(['compact', 'medium', 'high'])
    expect(ROW_HEIGHT_ESTIMATE_PX.compact).toBeGreaterThan(0)
    expect(ROW_HEIGHT_ESTIMATE_PX.medium).toBeGreaterThan(ROW_HEIGHT_ESTIMATE_PX.compact)
    expect(ROW_HEIGHT_ESTIMATE_PX.high).toBeGreaterThan(ROW_HEIGHT_ESTIMATE_PX.medium)
  })

  it('默认档为中等；未实测行取所在档估算值', () => {
    const model = new RowHeightModel()
    expect(model.tier).toBe('medium')
    expect(model.heightAt(0)).toBe(ROW_HEIGHT_ESTIMATE_PX.medium)
    const compact = new RowHeightModel('compact')
    expect(compact.heightAt(0)).toBe(ROW_HEIGHT_ESTIMATE_PX.compact)
  })

  it('档位切换即时生效：未实测行随档变化，已实测行保留实测值（实测是真相）', () => {
    const model = new RowHeightModel('compact')
    model.setMeasured(3, 55)
    model.setTier('high')
    expect(model.heightAt(0)).toBe(ROW_HEIGHT_ESTIMATE_PX.high)
    expect(model.heightAt(3)).toBe(55)
  })

  it('分组行混合估算：注入按行估算函数（分组行高与普通行不同）', () => {
    // 每 5 行第 0 行为分组行：估算 56px；普通行 40px
    const model = new RowHeightModel('medium', {
      estimateFor: (index) => (index % 5 === 0 ? 56 : 40),
    })
    expect(model.heightAt(0)).toBe(56)
    expect(model.heightAt(1)).toBe(40)
    // 分组行同样走「实测覆盖估算」
    model.setMeasured(0, 72)
    expect(model.heightAt(0)).toBe(72)
  })

  it('非法实测值显式拒绝（<=0 / 非有限值），不静默写入', () => {
    const model = new RowHeightModel()
    expect(() => model.setMeasured(0, 0)).toThrow()
    expect(() => model.setMeasured(0, -40)).toThrow()
    expect(() => model.setMeasured(0, Number.NaN)).toThrow()
    expect(() => model.setMeasured(0, Number.POSITIVE_INFINITY)).toThrow()
    expect(model.measuredCount()).toBe(0)
  })
})

describe('实测缓存', () => {
  it('setMeasured 回写后 heightAt 返回实测值；isMeasured/measuredCount 反映缓存', () => {
    const model = new RowHeightModel('medium')
    expect(model.isMeasured(7)).toBe(false)
    model.setMeasured(7, 63)
    expect(model.isMeasured(7)).toBe(true)
    expect(model.heightAt(7)).toBe(63)
    expect(model.heightAt(8)).toBe(ROW_HEIGHT_ESTIMATE_PX.medium)
    expect(model.measuredCount()).toBe(1)
    model.setMeasured(7, 66) // 重复实测覆盖为最新值
    expect(model.heightAt(7)).toBe(66)
    expect(model.measuredCount()).toBe(1)
  })

  it('sizes(totalRows) 输出窗口计算可消费的尺寸表（估算+实测混合）', () => {
    const model = new RowHeightModel('compact') // 32
    model.setMeasured(1, 64)
    expect(model.sizes(3)).toEqual([32, 64, 32])
  })
})

describe('校准收敛（估算偏小 → 实测偏大）', () => {
  it('同一滚动偏移下，可见集随校准单调收敛：contentStart 单调不增、行数单调不多于估算态', () => {
    const TOTAL = 100
    const VIEWPORT = 160
    const OFFSET = 320
    const MEASURED = 64 // 实测偏大（估算 32）

    const model = new RowHeightModel('compact')
    const rounds: Array<{ readonly contentStart: number; readonly count: number }> = []

    // 迭代校准循环：切窗 → 对窗口+overscan 内的行「实测」→ 回写 → 再切窗，直到可见集稳定
    for (let round = 0; round < 50; round += 1) {
      const window = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })
      if (window.start === -1) throw new Error('窗口不应为空')
      const before = rounds[rounds.length - 1]
      if (before && before.contentStart === window.contentStart && before.count === window.sections.length) {
        break // 已收敛到不动点
      }
      rounds.push({ contentStart: window.contentStart, count: window.sections.length })
      for (const section of window.sections) {
        model.setMeasured(section.index, MEASURED)
      }
    }

    // 收敛发生：至少两轮，且 contentStart 随校准单调不增（行变高 → 同偏移下上方行数变少）
    expect(rounds.length).toBeGreaterThanOrEqual(2)
    for (let i = 1; i < rounds.length; i += 1) {
      expect(rounds[i].contentStart).toBeLessThanOrEqual(rounds[i - 1].contentStart)
    }
    // 全部行实测后模型不再变化（不动点）
    const stabilized = rounds[rounds.length - 1]
    const finalWindow = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })
    expect(finalWindow.contentStart).toBe(stabilized.contentStart)
    expect(finalWindow.sections.length).toBe(stabilized.count)
  })

  it('完全校准后窗口 == 实测真值表窗口（独立事实源字面量 [64]*100），不跳行不重复', () => {
    const TOTAL = 100
    const OFFSET = 320
    const VIEWPORT = 160
    const MEASURED = 64

    const model = new RowHeightModel('compact')
    for (let i = 0; i < TOTAL; i += 1) model.setMeasured(i, MEASURED)

    const calibrated = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })
    const truth = computeWindow({ sizes: Array.from({ length: TOTAL }, () => MEASURED), scrollOffset: OFFSET, viewportSize: VIEWPORT })
    expect(calibrated.start).toBe(truth.start)
    expect(calibrated.end).toBe(truth.end)
    expect(calibrated.sections.map((s) => s.index)).toEqual(truth.sections.map((s) => s.index))
    expect(calibrated.sections.map((s) => s.offset)).toEqual(truth.sections.map((s) => s.offset))
    // 连续性（不跳行不重复）
    for (let i = 0; i < calibrated.sections.length; i += 1) {
      expect(calibrated.sections[i].index).toBe(calibrated.start + i)
    }
  })

  it('校准后同偏移可见行数少于估算态（实测偏大 → 视口装得更少），且窗口集合随部分校准逐步逼近真值', () => {
    const TOTAL = 100
    const OFFSET = 320
    const VIEWPORT = 160
    const MEASURED = 64

    const model = new RowHeightModel('compact')
    const estimateWindow = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })

    // 部分校准：只实测前 50 行
    for (let i = 0; i < 50; i += 1) model.setMeasured(i, MEASURED)
    const partialWindow = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })

    // 完全校准
    for (let i = 50; i < TOTAL; i += 1) model.setMeasured(i, MEASURED)
    const fullWindow = computeWindow({ sizes: model.sizes(TOTAL), scrollOffset: OFFSET, viewportSize: VIEWPORT })

    // 行变高 → 同偏移下首可见行索引单调下移、可见行数单调减少
    expect(estimateWindow.contentStart).toBe(10) // 320 / 32 = 行 10 起
    expect(partialWindow.contentStart).toBeLessThanOrEqual(estimateWindow.contentStart)
    expect(fullWindow.contentStart).toBe(5) // 320 / 64 = 行 5 起（真值）
    expect(fullWindow.contentStart).toBeLessThanOrEqual(partialWindow.contentStart)
    expect(fullWindow.sections.length).toBeLessThan(estimateWindow.sections.length)
    // 每一步窗口都连续
    for (const w of [estimateWindow, partialWindow, fullWindow]) {
      for (let i = 0; i < w.sections.length; i += 1) {
        expect(w.sections[i].index).toBe(w.start + i)
      }
    }
  })
})
