import { Link } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'

import styles from './LoginPage.module.css'

// 占位登录页（票 02）：仅验证路由可达与导航骨架；真实认证流程（H-01/H-02）随 M2 落地。
export function LoginPage() {
  return (
    <main className={styles.page}>
      <h1 className={styles.title}>登录</h1>
      <HealthNotice />
      <nav className={styles.nav} aria-label="占位导航">
        <Link to="/home">前往首页</Link>
      </nav>
    </main>
  )
}
