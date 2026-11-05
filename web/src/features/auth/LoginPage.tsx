import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'
import { Banner } from '@/ui-kit'

import { LoginForm } from './LoginForm'
import styles from './LoginPage.module.css'

/** next 回跳只接受站内路径：以 / 开头且不以 // 开头，否则回首页。 */
export function resolveNextPath(raw: string | null): string {
  if (raw !== null && raw.startsWith('/') && !raw.startsWith('//')) return raw
  return '/home'
}

// 需求 6.5 / 整体技术方案风险表：明文 HTTP 为已确认接受的试运行风险，登录页必须可见告知。
const HTTP_PLAINTEXT_NOTICE =
  '当前为明文 HTTP 传输（无加密），密码与数据可能被网络窃听；本系统仅用于试运行评估，正式启用前必须切换 HTTPS。'

export function LoginPage() {
  const [search] = useSearchParams()
  const navigate = useNavigate()
  const nextPath = resolveNextPath(search.get('next'))

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>登录</h1>
      <HealthNotice />
      <Banner tone="warning" className={styles.httpNotice}>
        {HTTP_PLAINTEXT_NOTICE}
      </Banner>
      <LoginForm onAuthenticated={() => navigate(nextPath, { replace: true })} />
      <p className={styles.switch}>
        <Link to="/register">没有账号？去注册</Link>
      </p>
    </main>
  )
}
