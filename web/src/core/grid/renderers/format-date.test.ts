// formatDate 表驱动测试（票 05）：三种日期格式 + include_time；空值与非法值安全显示空串。
// 期望值使用本地 Date 构造（不受运行环境时区影响），且为手算字面量。
import { describe, expect, it } from 'vitest'

import { formatDate } from './format-date'

const SAMPLE = new Date(2024, 0, 5).getTime()
const WITH_TIME = new Date(2024, 0, 5, 9, 8).getTime()

describe('formatDate（三种格式 + 是否含时间）', () => {
  it.each([
    { value: SAMPLE, config: {}, expected: '2024-01-05' }, // 缺省 YYYY-MM-DD
    { value: SAMPLE, config: { format: 'YYYY/MM/DD' }, expected: '2024/01/05' },
    { value: SAMPLE, config: { format: 'YYYY-MM-DD' }, expected: '2024-01-05' },
    { value: SAMPLE, config: { format: 'YYYY年M月D日' }, expected: '2024年1月5日' },
    { value: WITH_TIME, config: { format: 'YYYY-MM-DD', include_time: true }, expected: '2024-01-05 09:08' },
    { value: WITH_TIME, config: { format: 'YYYY/MM/DD', include_time: true }, expected: '2024/01/05 09:08' },
    { value: WITH_TIME, config: { format: 'YYYY年M月D日', include_time: true }, expected: '2024年1月5日 09:08' },
    { value: 0, config: {}, expected: '1970-01-01' }, // 0 是合法时间（与空值不同）
    { value: null, config: {}, expected: '' },
    { value: undefined, config: {}, expected: '' },
    { value: 'not-a-date', config: {}, expected: '' },
    { value: Number.NaN, config: {}, expected: '' },
  ])('value=$value config=$config → "$expected"', ({ value, config, expected }) => {
    expect(formatDate(value, config)).toBe(expected)
  })
})
