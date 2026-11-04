// formatNumber 表驱动测试（票 08 number 演示；需求 4.3 数字字段行：整数/小数 0-4 位、
// 千分位开关、超精度四舍五入、空值与 0 区分显示）。期望值全部为手算字面量（独立事实源，非复算）。
import { describe, expect, it } from 'vitest'

import { formatNumber } from './format-number'

describe('formatNumber（票 08 number 演示）', () => {
  it.each([
    { value: 1234567, config: {}, expected: '1234567' }, // 缺省 = 整数、无千分位
    { value: 1234567, config: { precision: 0 }, expected: '1234567' },
    { value: 1234567, config: { thousands: true }, expected: '1,234,567' },
    { value: 1234.5678, config: { precision: 2, thousands: true }, expected: '1,234.57' }, // 超精度四舍五入
    { value: 9.999, config: { precision: 2 }, expected: '10.00' }, // 两位内进位
    { value: 1234567.891, config: { precision: 2, thousands: true }, expected: '1,234,567.89' },
    { value: -1234567.891, config: { precision: 2, thousands: true }, expected: '-1,234,567.89' },
    { value: -1234.5, config: { precision: 1, thousands: true }, expected: '-1,234.5' },
    { value: 0, config: { thousands: true }, expected: '0' }, // 0 正常显示（与空值区分）
    { value: null, config: { thousands: true }, expected: '' }, // 空值 → 空串
    { value: undefined, config: {}, expected: '' },
    { value: -0.4, config: { precision: 0 }, expected: '0' }, // 舍入到 0 不留 "-0" 形态
    { value: 1234.5678, config: { precision: 9, thousands: true }, expected: '1,234.5678' }, // 位数钳制到 4
    { value: 1.5, config: { precision: -1 }, expected: '2' }, // 负位数钳制到 0
    { value: Number.NaN, config: {}, expected: '' }, // 非有限值安全显示为空
    { value: Number.POSITIVE_INFINITY, config: {}, expected: '' },
  ])('value=$value config=$config → "$expected"', ({ value, config, expected }) => {
    expect(formatNumber(value, config)).toBe(expected)
  })
})
