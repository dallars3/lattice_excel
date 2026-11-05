import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { ApiError, isApiError } from '@/core/http'
import { HealthNotice } from '@/features/health/HealthNotice'
import { authStore } from '@/stores'
import { Button } from '@/ui-kit'

import { registerAndSignIn, type RegisterPayload } from './api'
import {
  validateEnterpriseName,
  validatePassword,
  validateUsername,
} from './validation'
import styles from './RegisterPage.module.css'

type RegisterPath = 'create' | 'invite' | 'link'

/** 三条归属路径由 URL 参数区分：邀请 / 分享链接 / 无参数（直接建企业）。 */
export function resolveRegisterPath(search: URLSearchParams): RegisterPath {
  if (search.has('invite') || search.has('invite_id')) return 'invite'
  if ((search.get('link_token') ?? '').trim() !== '') return 'link'
  return 'create'
}

const PATH_HINT: Record<RegisterPath, string> = {
  create: '注册即为你新建一个企业，你将自动成为该企业所有者。',
  invite: '你将加入邀请你的企业，注册后即可与同事协作。',
  link: '你将凭分享链接获得对应表格的访问权限，不会创建企业。',
}

interface FieldErrors {
  username?: string
  password?: string
  enterprise?: string
}

export function RegisterPage() {
  const [search] = useSearchParams()
  const navigate = useNavigate()
  const path = resolveRegisterPath(search)
  const linkToken = (search.get('link_token') ?? '').trim()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [enterpriseName, setEnterpriseName] = useState('')
  const [errors, setErrors] = useState<FieldErrors>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextErrors: FieldErrors = {
      username: validateUsername(username) ?? undefined,
      password: validatePassword(password) ?? undefined,
      enterprise:
        path === 'create' ? validateEnterpriseName(enterpriseName) ?? undefined : undefined,
    }
    setErrors(nextErrors)
    setFormError(null)
    if (nextErrors.username || nextErrors.password || nextErrors.enterprise) return

    const payload: RegisterPayload = { username, password }
    if (path === 'create' && enterpriseName.trim() !== '') {
      payload.enterprise_name = enterpriseName.trim()
    }
    if (linkToken !== '') {
      payload.link_token = linkToken
    }

    setSubmitting(true)
    try {
      const me = await registerAndSignIn(payload)
      authStore.getState().setSession({
        user: me.user,
        enterprise: me.enterprise,
        myRole: me.my_role,
      })
      navigate('/home', { replace: true })
    } catch (error) {
      if (isApiError(error)) {
        if (error.code === 1002) {
          setErrors({ username: '用户名已存在' })
        } else if (error.code === 1001) {
          setFormError('输入有误，请按提示修改后重试')
        } else {
          setFormError(describeApiError(error))
        }
      } else {
        setFormError('网络异常，请稍后重试')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>注册</h1>
      <HealthNotice />
      <p className={styles.pathHint}>{PATH_HINT[path]}</p>
      <form className={styles.form} onSubmit={handleSubmit} noValidate>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="register-username">
            用户名
          </label>
          <input
            id="register-username"
            className={styles.input}
            value={username}
            autoComplete="username"
            aria-invalid={errors.username !== undefined}
            aria-describedby={errors.username === undefined ? undefined : 'register-username-error'}
            onChange={(event) => setUsername(event.target.value)}
          />
          {errors.username !== undefined && (
            <p id="register-username-error" className={styles.fieldError}>
              {errors.username}
            </p>
          )}
        </div>

        <div className={styles.field}>
          <label className={styles.label} htmlFor="register-password">
            密码
          </label>
          <input
            id="register-password"
            className={styles.input}
            type="password"
            value={password}
            autoComplete="new-password"
            aria-invalid={errors.password !== undefined}
            aria-describedby={errors.password === undefined ? undefined : 'register-password-error'}
            onChange={(event) => setPassword(event.target.value)}
          />
          {errors.password !== undefined && (
            <p id="register-password-error" className={styles.fieldError}>
              {errors.password}
            </p>
          )}
        </div>

        {path === 'create' && (
          <div className={styles.field}>
            <label className={styles.label} htmlFor="register-enterprise">
              企业名称（可选）
            </label>
            <input
              id="register-enterprise"
              className={styles.input}
              value={enterpriseName}
              aria-invalid={errors.enterprise !== undefined}
              aria-describedby={
                errors.enterprise === undefined ? undefined : 'register-enterprise-error'
              }
              onChange={(event) => setEnterpriseName(event.target.value)}
            />
            {errors.enterprise !== undefined && (
              <p id="register-enterprise-error" className={styles.fieldError}>
                {errors.enterprise}
              </p>
            )}
          </div>
        )}

        {formError !== null && (
          <p role="alert" className={styles.formError}>
            {formError}
          </p>
        )}

        <Button type="submit" disabled={submitting}>
          {submitting ? '注册中…' : '注册'}
        </Button>
      </form>
      <p className={styles.switch}>
        <Link to="/login">已有账号？去登录</Link>
      </p>
    </main>
  )
}

function describeApiError(error: ApiError): string {
  return `${error.catalog.title}。${error.catalog.action}`
}
