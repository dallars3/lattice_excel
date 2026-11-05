import { Button } from '@/ui-kit'

import styles from '../BaseWorkspacePage.module.css'

/** 底栏：左侧新增记录入口位（票 06 接线，本票禁用占位）；右侧按 G-3 展示已加载条数。 */
export function WorkspaceFooter() {
  return (
    <footer role="contentinfo" aria-label="底栏" className={styles.footer}>
      <div className={styles.footerLeft}>
        <Button variant="secondary" disabled>
          + 新增记录
        </Button>
      </div>
      <span className={styles.footerCount}>已加载 0 条</span>
    </footer>
  )
}
