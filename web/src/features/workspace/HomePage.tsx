import { Link, useNavigate } from 'react-router-dom'

import { useQueryClient } from '@tanstack/react-query'

import { logoutAndClear } from '@/features/auth/logout'
import { HealthNotice } from '@/features/health/HealthNotice'
import { Button } from '@/ui-kit'

import styles from './HomePage.module.css'

// 占位首页（票 02）：承接票 01 占位首页的标题与试运行声明（声明并入 HealthNotice）。
// Base 列表、最近打开等首页能力（HOME-01/HOME-02）随 M2 落地；登出入是票 01 认证闭环一部分。
export function HomePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  async function handleLogout() {
    await logoutAndClear(queryClient)
    navigate('/login', { replace: true })
  }

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>协作多维表格</h1>
      <HealthNotice />
      <nav className={styles.nav} aria-label="占位导航">
        <Link to="/base/base_001">打开演示 Base 工作台</Link>
      </nav>
      <div className={styles.actions}>
        <Button variant="secondary" onClick={() => void handleLogout()}>
          退出登录
        </Button>
      </div>
    </main>
  )
}
