import { useEffect, useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'

import type { FieldRecord } from '../api/field'
import type { ViewSummary } from '../api/view'
import { apiErrorText } from '../errorText'
import type { ViewConfig } from '../types/view-config'
import styles from '../BaseWorkspacePage.module.css'
import {
  FieldVisibilityPanel,
  FilterPanel,
  GroupPanel,
  SortPanel,
} from './view/ViewPanels'
import panelStyles from './view/ViewPanels.module.css'

const MIN_VIEW_NAME_LENGTH = 1
const MAX_VIEW_NAME_LENGTH = 30

type PanelKind = 'filters' | 'sorts' | 'group' | 'fields'
type DialogMode = 'create' | 'rename' | 'delete'

interface DialogState {
  mode: DialogMode
  view: ViewSummary | null
  draft: string
}

export interface ViewBarProps {
  views: ViewSummary[]
  currentViewId: string | null
  onSelect: (viewId: string) => void
  /** 可编辑及以上：新建/重命名/复制/删除与配置面板；可查看/可填写仅切换。 */
  canEdit: boolean
  fields?: FieldRecord[]
  config?: ViewConfig | null
  onUpdateConfig?: (updater: (current: ViewConfig) => ViewConfig) => void
  configLoading?: boolean
  configError?: string | null
  onRetryConfig?: () => void
  onCreate?: (name: string) => Promise<void>
  onRename?: (viewId: string, name: string) => Promise<void>
  onDuplicate?: (viewId: string) => Promise<void>
  onDelete?: (viewId: string) => Promise<void>
}

/** 视图栏：标签切换、视图 CRUD 与全量配置面板入口（配置计数徽标）。 */
export function ViewBar({
  views,
  currentViewId,
  onSelect,
  canEdit,
  fields = [],
  config = null,
  onUpdateConfig,
  configLoading = false,
  configError = null,
  onRetryConfig,
  onCreate,
  onRename,
  onDuplicate,
  onDelete,
}: ViewBarProps) {
  const [menuViewId, setMenuViewId] = useState<string | null>(null)
  const [activePanel, setActivePanel] = useState<PanelKind | null>(null)
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 票 09：权限中途降级后立即收起视图菜单、配置面板与对话框。
  useEffect(() => {
    if (canEdit) return
    setMenuViewId(null)
    setActivePanel(null)
    setDialog(null)
  }, [canEdit])

  const editable = canEdit && onUpdateConfig !== undefined && config !== null
  const filterCount = config?.filters.length ?? 0
  const sortCount = config?.sorts.length ?? 0
  const groupCount = config?.group === null || config?.group === undefined ? 0 : 1

  function togglePanel(kind: PanelKind) {
    setActivePanel((current) => (current === kind ? null : kind))
  }

  function openDialog(mode: DialogMode, view: ViewSummary | null) {
    setMenuViewId(null)
    setError(null)
    setDialog({ mode, view, draft: view?.name ?? '' })
  }

  function validateName(name: string): boolean {
    if (name.length < MIN_VIEW_NAME_LENGTH || name.length > MAX_VIEW_NAME_LENGTH) {
      setError(`名称需为 ${MIN_VIEW_NAME_LENGTH}–${MAX_VIEW_NAME_LENGTH} 个字符`)
      return false
    }
    const duplicated = views.some(
      (view) => view.id !== dialog?.view?.id && view.name === name,
    )
    if (duplicated) {
      setError('名称已被使用')
      return false
    }
    return true
  }

  function submitName(event: FormEvent) {
    event.preventDefault()
    if (dialog === null) return
    const name = dialog.draft.trim()
    if (!validateName(name)) return
    setError(null)
    setPending(true)
    const action =
      dialog.mode === 'create'
        ? onCreate?.(name) ?? Promise.resolve()
        : dialog.view === null || onRename === undefined
          ? Promise.resolve()
          : onRename(dialog.view.id, name)
    action
      .then(() => setDialog(null))
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  function confirmDelete() {
    if (dialog === null || dialog.view === null || onDelete === undefined) return
    setError(null)
    setPending(true)
    onDelete(dialog.view.id)
      .then(() => setDialog(null))
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  function runDuplicate(view: ViewSummary) {
    setMenuViewId(null)
    if (onDuplicate === undefined) return
    setError(null)
    setPending(true)
    onDuplicate(view.id)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  const badge = (count: number) =>
    count === 0 ? null : (
      <span className={styles.viewBadge} aria-hidden="true">
        {count}
      </span>
    )

  return (
    <div className={styles.viewBar}>
      <nav aria-label="视图" className={styles.viewList}>
        {views.length === 0 ? (
          <span className={styles.emptyHint}>暂无视图</span>
        ) : (
          views.map((view) => (
            <div key={view.id} className={styles.viewWrap}>
              <button
                type="button"
                aria-pressed={view.id === currentViewId}
                className={
                  view.id === currentViewId
                    ? `${styles.viewButton} ${styles.viewActive}`
                    : styles.viewButton
                }
                onClick={() => {
                  setActivePanel(null)
                  onSelect(view.id)
                }}
              >
                {view.name}
              </button>
              {canEdit ? (
                <>
                  <button
                    type="button"
                    className={styles.viewMenuButton}
                    aria-label={`视图操作：${view.name}`}
                    aria-haspopup="menu"
                    aria-expanded={menuViewId === view.id}
                    onClick={() =>
                      setMenuViewId((current) => (current === view.id ? null : view.id))
                    }
                  >
                    ⋯
                  </button>
                  {menuViewId === view.id ? (
                    <div role="menu" aria-label={`${view.name} 视图菜单`} className={styles.menu}>
                      <button
                        type="button"
                        role="menuitem"
                        onClick={() => openDialog('rename', view)}
                      >
                        重命名视图
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        disabled={pending}
                        onClick={() => runDuplicate(view)}
                      >
                        复制视图
                      </button>
                      <button
                        type="button"
                        role="menuitem"
                        disabled={views.length <= 1 || pending}
                        onClick={() => openDialog('delete', view)}
                      >
                        删除视图
                      </button>
                    </div>
                  ) : null}
                </>
              ) : null}
            </div>
          ))
        )}
        {canEdit && onCreate !== undefined ? (
          <Button variant="secondary" onClick={() => openDialog('create', null)}>
            新建视图
          </Button>
        ) : null}
      </nav>

      {canEdit ? (
        <div role="group" aria-label="视图工具栏" className={styles.viewToolbar}>
          <Button
            variant="secondary"
            aria-label="筛选"
            aria-expanded={activePanel === 'filters'}
            disabled={!editable}
            onClick={() => togglePanel('filters')}
          >
            筛选{badge(filterCount)}
          </Button>
          <Button
            variant="secondary"
            aria-label="排序"
            aria-expanded={activePanel === 'sorts'}
            disabled={!editable}
            onClick={() => togglePanel('sorts')}
          >
            排序{badge(sortCount)}
          </Button>
          <Button
            variant="secondary"
            aria-label="分组"
            aria-expanded={activePanel === 'group'}
            disabled={!editable}
            onClick={() => togglePanel('group')}
          >
            分组{badge(groupCount)}
          </Button>
          <Button
            variant="secondary"
            aria-label="字段显隐"
            aria-expanded={activePanel === 'fields'}
            disabled={!editable}
            onClick={() => togglePanel('fields')}
          >
            字段显隐
          </Button>
        </div>
      ) : null}

      {configLoading ? (
        <p role="status" className={styles.emptyHint}>
          正在加载视图配置…
        </p>
      ) : configError !== null ? (
        <div role="alert" className={styles.inlineError}>
          {configError}
          {onRetryConfig === undefined ? null : (
            <Button variant="secondary" onClick={onRetryConfig}>
              重新加载视图配置
            </Button>
          )}
        </div>
      ) : null}

      {error !== null && dialog === null ? (
        <p role="alert" className={styles.inlineError}>
          {error}
        </p>
      ) : null}

      {canEdit && activePanel !== null && config !== null && onUpdateConfig !== undefined ? (
        <div className={panelStyles.panelHost}>
          {activePanel === 'filters' ? (
            <FilterPanel fields={fields} config={config} onChange={onUpdateConfig} />
          ) : activePanel === 'sorts' ? (
            <SortPanel fields={fields} config={config} onChange={onUpdateConfig} />
          ) : activePanel === 'group' ? (
            <GroupPanel fields={fields} config={config} onChange={onUpdateConfig} />
          ) : (
            <FieldVisibilityPanel fields={fields} config={config} onChange={onUpdateConfig} />
          )}
        </div>
      ) : null}

      {dialog === null ? null : (
        <div className={styles.mask}>
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="view-dialog-title"
            className={styles.dialog}
          >
            <h2 id="view-dialog-title" className={styles.dialogTitle}>
              {dialog.mode === 'create'
                ? '新建视图'
                : dialog.mode === 'rename'
                  ? '重命名视图'
                  : '删除视图'}
            </h2>
            {dialog.mode === 'delete' ? (
              <>
                <p>确定删除「{dialog.view?.name}」吗？</p>
                <p className={styles.dangerHint}>每个数据表至少保留 1 个视图。</p>
                {error === null ? null : <p role="alert">{error}</p>}
                <div className={styles.dialogActions}>
                  <Button variant="danger" disabled={pending} onClick={confirmDelete}>
                    确认删除
                  </Button>
                  <Button variant="secondary" onClick={() => setDialog(null)}>
                    取消
                  </Button>
                </div>
              </>
            ) : (
              <form onSubmit={submitName} className={styles.form}>
                <label htmlFor="view-name">视图名称</label>
                <input
                  id="view-name"
                  value={dialog.draft}
                  maxLength={MAX_VIEW_NAME_LENGTH}
                  onChange={(event) => {
                    setDialog({ ...dialog, draft: event.target.value })
                    setError(null)
                  }}
                />
                {error === null ? null : <p role="alert">{error}</p>}
                <div className={styles.dialogActions}>
                  <Button
                    type="submit"
                    disabled={pending || dialog.draft.trim() === ''}
                  >
                    {dialog.mode === 'create' ? '创建' : '保存'}
                  </Button>
                  <Button variant="secondary" onClick={() => setDialog(null)}>
                    取消
                  </Button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
