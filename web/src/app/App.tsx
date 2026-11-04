import { NOTICE_TEXT } from '@/lib/env'

import styles from './App.module.css'

// 占位首页（票 01）：仅验证工程可启动、可渲染；路由表与 QueryClient 由票 02 接入。
export function App() {
  return (
    <main className={styles.page}>
      <h1 className={styles.title}>协作多维表格</h1>
      <p className={styles.notice}>{NOTICE_TEXT}</p>
    </main>
  )
}
