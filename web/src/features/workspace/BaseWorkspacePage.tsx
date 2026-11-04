import { Link, useParams } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'

import styles from './BaseWorkspacePage.module.css'

// 占位 Base 工作台（票 02）：验证 /base/:id 路由参数可达；网格/工具栏等随 M2 落地。
export function BaseWorkspacePage() {
  const { id } = useParams<{ id: string }>()

  return (
    <main className={styles.page}>
      <h1 className={styles.title}>Base 工作台</h1>
      <p className={styles.meta}>
        Base ID：<span>{id}</span>
      </p>
      <HealthNotice />
      <nav className={styles.nav} aria-label="占位导航">
        <Link to="/home">返回首页</Link>
        <Link to="/login">前往登录</Link>
      </nav>
    </main>
  )
}
