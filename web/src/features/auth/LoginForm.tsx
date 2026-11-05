import { useId, useState, type FormEvent } from 'react'

import { ApiError, isApiError } from '@/core/http'
import { authStore } from '@/stores'
import { Button } from '@/ui-kit'

import { loginAndLoadMe } from './api'
import styles from './LoginForm.module.css'

export interface LoginFormProps {
  /** 登录并写入会话后回调（页面决定跳转，弹窗决定关闭）。 */
  onAuthenticated?: () => void
  submitLabel?: string
}

/** 登录表单：登录成功后以 H-04 me 为身份事实源写入 authStore。 */
export function LoginForm({ onAuthenticated, submitLabel = '登录' }: LoginFormProps) {
  const idPrefix = useId()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<{ username?: string; password?: string }>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextErrors = {
      username: username.trim() === '' ? '请输入用户名' : undefined,
      password: password === '' ? '请输入密码' : undefined,
    }
    setErrors(nextErrors)
    setFormError(null)
    if (nextErrors.username !== undefined || nextErrors.password !== undefined) return

    setSubmitting(true)
    try {
      const me = await loginAndLoadMe({ username, password })
      authStore.getState().setSession({
        user: me.user,
        enterprise: me.enterprise,
        myRole: me.my_role,
      })
      onAuthenticated?.()
    } catch (error) {
      if (isApiError(error)) {
        setFormError(describeLoginError(error))
      } else {
        setFormError('网络异常，请稍后重试')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form className={styles.form} onSubmit={handleSubmit} noValidate>
      <div className={styles.field}>
        <label className={styles.label} htmlFor={`${idPrefix}-username`}>
          用户名
        </label>
        <input
          id={`${idPrefix}-username`}
          className={styles.input}
          value={username}
          autoComplete="username"
          aria-invalid={errors.username !== undefined}
          aria-describedby={
            errors.username === undefined ? undefined : `${idPrefix}-username-error`
          }
          onChange={(event) => setUsername(event.target.value)}
        />
        {errors.username !== undefined && (
          <p id={`${idPrefix}-username-error`} className={styles.fieldError}>
            {errors.username}
          </p>
        )}
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor={`${idPrefix}-password`}>
          密码
        </label>
        <input
          id={`${idPrefix}-password`}
          className={styles.input}
          type="password"
          value={password}
          autoComplete="current-password"
          aria-invalid={errors.password !== undefined}
          aria-describedby={
            errors.password === undefined ? undefined : `${idPrefix}-password-error`
          }
          onChange={(event) => setPassword(event.target.value)}
        />
        {errors.password !== undefined && (
          <p id={`${idPrefix}-password-error`} className={styles.fieldError}>
            {errors.password}
          </p>
        )}
      </div>

      {formError !== null && (
        <p role="alert" className={styles.formError}>
          {formError}
        </p>
      )}

      <Button type="submit" disabled={submitting}>
        {submitting ? '登录中…' : submitLabel}
      </Button>

      <p className={styles.forgotNote}>如遗忘密码，账号将无法使用</p>
    </form>
  )
}

// 附录 B 2002：按场景区分两种文案；后端 message 即场景信号（网关原样透传）。
const LOGIN_BAD_CREDENTIALS_TEXT = '用户名或密码错误'
const LOGIN_LOCKED_TEXT = '失败次数过多，请稍后再试'

function describeLoginError(error: ApiError): string {
  if (error.code === 2002) {
    if (error.message === LOGIN_BAD_CREDENTIALS_TEXT || error.message === LOGIN_LOCKED_TEXT) {
      return error.message
    }
    // 未细分场景的 2002（如旧后端/兜底信封）保持合并提示。
    return '用户名或密码错误，或账号已锁定，请稍后再试'
  }
  if (error.code === 1001) return '输入有误，请按提示修改后重试'
  return `${error.catalog.title}。${error.catalog.action}`
}
