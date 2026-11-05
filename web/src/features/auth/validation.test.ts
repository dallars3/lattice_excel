import { describe, expect, it } from 'vitest'

import { validateEnterpriseName, validatePassword, validateUsername } from './validation'

describe('注册表单校验（需求 4.1 / 票 01）', () => {
  it.each([
    { value: '', want: '请输入用户名' },
    { value: 'abc', want: '用户名需为 4–20 位' },
    { value: 'a'.repeat(21), want: '用户名需为 4–20 位' },
    { value: '1alice', want: '用户名需以字母开头' },
    { value: 'ali-ce', want: '用户名仅可包含字母、数字与下划线' },
    { value: 'Alice_01', want: null },
    { value: 'abcd', want: null },
  ])('用户名 $value → $want', ({ value, want }) => {
    expect(validateUsername(value)).toBe(want)
  })

  it.each([
    { value: '', want: '请输入密码' },
    { value: 'Ab1', want: '密码需为 8–32 位' },
    { value: `A1${'a'.repeat(31)}`, want: '密码需为 8–32 位' },
    { value: 'abcdefgh', want: '密码需同时包含字母与数字' },
    { value: '12345678', want: '密码需同时包含字母与数字' },
    { value: 'Pass1234', want: null },
  ])('密码 $value → $want', ({ value, want }) => {
    expect(validatePassword(value)).toBe(want)
  })

  it.each([
    { value: '', want: null },
    { value: '启', want: '企业名称需为 2–30 个字符' },
    { value: '   ', want: '企业名称需为 2–30 个字符' },
    { value: '启航科技', want: null },
    { value: '企'.repeat(31), want: '企业名称需为 2–30 个字符' },
  ])('企业名称 $value → $want', ({ value, want }) => {
    expect(validateEnterpriseName(value)).toBe(want)
  })
})
