import { NOTICE_TEXT } from '@/lib/env'
import { Banner } from '@/ui-kit'

import styles from './HealthNotice.module.css'

// 试运行免责声明（技术方案第 14 章）：文案来自 VITE_NOTICE_TEXT（票 01 环境变量），
// 视觉复用 ui-kit Banner 警告语义；票 02 在登录页、首页、Base 工作台三处强制挂载。
export function HealthNotice() {
  return (
    <Banner tone="warning" className={styles.notice}>
      {NOTICE_TEXT}
    </Banner>
  )
}
