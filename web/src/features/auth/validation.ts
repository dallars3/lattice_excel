// 注册表单字段级校验（需求 4.1）：非法输入不发请求，文案直接锚定字段。
export const USERNAME_PATTERN = /^[A-Za-z][A-Za-z0-9_]*$/
export const PASSWORD_HAS_LETTER = /[A-Za-z]/
export const PASSWORD_HAS_DIGIT = /[0-9]/

export function validateUsername(value: string): string | null {
  if (value === '') return '请输入用户名'
  if (value.length < 4 || value.length > 20) return '用户名需为 4–20 位'
  if (!/^[A-Za-z]/.test(value)) return '用户名需以字母开头'
  if (!USERNAME_PATTERN.test(value)) return '用户名仅可包含字母、数字与下划线'
  return null
}

export function validatePassword(value: string): string | null {
  if (value === '') return '请输入密码'
  if (value.length < 8 || value.length > 32) return '密码需为 8–32 位'
  if (!PASSWORD_HAS_LETTER.test(value) || !PASSWORD_HAS_DIGIT.test(value)) {
    return '密码需同时包含字母与数字'
  }
  return null
}

/** 企业名称可选；仅在建企业路径可见，填写时校验 2–30 字。 */
export function validateEnterpriseName(value: string): string | null {
  if (value === '') return null
  const trimmed = value.trim()
  if (trimmed.length < 2 || trimmed.length > 30) return '企业名称需为 2–30 个字符'
  return null
}

/**
 * 留空企业名时的默认值「{username}的企业」。
 * 按 Unicode 码点截断到 30 字（不把代理对拆成半个字符）。
 */
export function defaultEnterpriseName(username: string): string {
  return Array.from(`${username}的企业`).slice(0, 30).join('')
}
