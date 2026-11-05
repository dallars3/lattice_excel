import { Button } from '@/ui-kit'

import type { ViewSummary } from '../api/view'
import styles from '../BaseWorkspacePage.module.css'

export interface ViewBarProps {
  views: ViewSummary[]
  currentViewId: string | null
  onSelect: (viewId: string) => void
}

const TOOLBAR_PLACEHOLDERS = ['筛选', '排序', '分组', '字段显隐']

/** 视图栏（本票仅列表与本地切换）：工具栏按钮位预留，CRUD/配置属票 07。 */
export function ViewBar({ views, currentViewId, onSelect }: ViewBarProps) {
  return (
    <div className={styles.viewBar}>
      <nav aria-label="视图" className={styles.viewList}>
        {views.length === 0 ? (
          <span className={styles.emptyHint}>暂无视图</span>
        ) : (
          views.map((view) => (
            <button
              key={view.id}
              type="button"
              aria-pressed={view.id === currentViewId}
              className={
                view.id === currentViewId ? `${styles.viewButton} ${styles.viewActive}` : styles.viewButton
              }
              onClick={() => onSelect(view.id)}
            >
              {view.name}
            </button>
          ))
        )}
      </nav>
      <div role="group" aria-label="视图工具栏" className={styles.viewToolbar}>
        {TOOLBAR_PLACEHOLDERS.map((label) => (
          <Button key={label} variant="secondary" disabled>
            {label}
          </Button>
        ))}
      </div>
    </div>
  )
}
