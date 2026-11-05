import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'

import { LoginForm } from './LoginForm'
import styles from './LoginPage.module.css'

/** next 回跳只接受站内路径：以 / 开头且不以 // 开头，否则回首页。 */
export function resolveNextPath(raw: string | null): string {
  if (raw !== null && raw.startsWith('/') && !raw.startsWith('//')) return raw
  return '/home'
}

export function LoginPage() {
  const [search] = useSearchParams()
  const navigate = useNavigate()
  const nextPath = resolveNextPath(search.get('next'))

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>登录</h1>
      <HealthNotice />
      <LoginForm onAuthenticated={() => navigate(nextPath, { replace: true })} />
      <p className={styles.switch}>
        <Link to="/register">没有账号？去注册</Link>
      </p>
    </main>
  )
}
