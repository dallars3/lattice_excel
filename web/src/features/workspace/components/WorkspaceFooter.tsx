import { Button } from '@/ui-kit'

import styles from '../BaseWorkspacePage.module.css'

export interface WorkspaceFooterProps {
  /** 已加载记录数（G-3：契约无总数）。 */
  loadedCount?: number
  /** 仍有下一页时展示 n+，不虚构总数。 */
  hasMore?: boolean
}

/** 底栏：左侧新增记录入口位（票 06 接线，本票禁用占位）；右侧按 G-3 展示已加载条数。 */
export function WorkspaceFooter({ loadedCount = 0, hasMore = false }: WorkspaceFooterProps) {
  return (
    <footer role="contentinfo" aria-label="底栏" className={styles.footer}>
      <div className={styles.footerLeft}>
        <Button variant="secondary" disabled>
          + 新增记录
        </Button>
      </div>
      <span className={styles.footerCount} data-testid="footer-record-count">
        已加载 {loadedCount}
        {hasMore ? '+' : ''} 条
      </span>
    </footer>
  )
}
