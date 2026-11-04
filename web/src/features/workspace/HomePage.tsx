import { Link } from 'react-router-dom'

import { HealthNotice } from '@/features/health/HealthNotice'

import styles from './HomePage.module.css'

// 占位首页（票 02）：承接票 01 占位首页的标题与试运行声明（声明并入 HealthNotice）。
// Base 列表、最近打开等首页能力（HOME-01/HOME-02）随 M2 落地。
export function HomePage() {
  return (
    <main className={styles.page}>
      <h1 className={styles.title}>协作多维表格</h1>
      <HealthNotice />
      <nav className={styles.nav} aria-label="占位导航">
        <Link to="/login">前往登录</Link>
        <Link to="/base/base_001">打开演示 Base 工作台</Link>
      </nav>
    </main>
  )
}
