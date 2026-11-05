import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { useQuery, useQueryClient } from '@tanstack/react-query'

import { deleteBase, duplicateBase, renameBase, toggleStar } from '@/features/home/api'
import { HealthNotice } from '@/features/health/HealthNotice'
import { usePermission } from '@/features/permissions'
import { baseKeys, homeKeys, tableKeys, viewKeys, fieldKeys } from '@/query-keys'
import { trackWrite, useSaveState } from '@/stores'
import { Button } from '@/ui-kit'

import { fetchBaseById } from './api/base'
import { fetchFields } from './api/field'
import {
  createTable,
  deleteTable,
  duplicateTable,
  fetchTables,
  renameTable,
} from './api/table'
import { fetchViews } from './api/view'
import { apiErrorTitle } from './errorText'
import { GridArea } from './components/GridArea'
import { TableTabs } from './components/TableTabs'
import { ViewBar } from './components/ViewBar'
import { WorkspaceFooter } from './components/WorkspaceFooter'
import { WorkspaceTopBar } from './components/WorkspaceTopBar'
import styles from './BaseWorkspacePage.module.css'

function ErrorBox({
  title,
  retryLabel,
  onRetry,
}: {
  title: string
  retryLabel: string
  onRetry: () => void
}) {
  return (
    <div role="alert" className={styles.errorBox}>
      <p>{title}</p>
      <Button variant="secondary" onClick={onRetry}>
        {retryLabel}
      </Button>
    </div>
  )
}

/** Base 工作台（票 03）：深链加载 Base/表/默认视图，交付五段式骨架与数据表管理。 */
export function BaseWorkspacePage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const baseId = id ?? ''
  const saveState = useSaveState()

  const [selectedTableId, setSelectedTableId] = useState<string | null>(null)
  const [selectedViewId, setSelectedViewId] = useState<string | null>(null)

  // 契约无 GET /bases/{id}：用 H-21 四列表兜底查找（票 03 G 项）。
  const baseQuery = useQuery({
    queryKey: baseKeys.detail(baseId),
    queryFn: () => fetchBaseById(baseId),
    enabled: baseId !== '',
    retry: false,
  })
  const base = baseQuery.data ?? null

  // 表列表与 Base 查找并行发起；Base 不存在/无权时不请求表域。
  const tablesQuery = useQuery({
    queryKey: tableKeys.list(baseId),
    queryFn: () => fetchTables(baseId),
    enabled: baseId !== '' && base !== null,
    retry: false,
  })
  const tables = tablesQuery.data ?? []
  const currentTable =
    tables.find((table) => table.id === selectedTableId) ?? tables[0] ?? null
  const currentTableId = currentTable?.id ?? null

  const viewsQuery = useQuery({
    queryKey: viewKeys.list(currentTableId ?? ''),
    queryFn: () => fetchViews(currentTableId ?? ''),
    enabled: currentTableId !== null,
    retry: false,
  })
  const views = viewsQuery.data ?? []
  const currentView =
    views.find((view) => view.id === selectedViewId) ?? views[0] ?? null

  // H-34 字段列表：随当前表切换重新加载（query key 按 tableId 精确隔离）。
  const fieldsQuery = useQuery({
    queryKey: fieldKeys.list(currentTableId ?? ''),
    queryFn: () => fetchFields(currentTableId ?? ''),
    enabled: currentTableId !== null,
    retry: false,
  })
  const fields = fieldsQuery.data ?? []

  // 权限门：表结构入口仅可管理可见；Base 菜单项由顶栏自行判权。
  const permission = usePermission(base)
  const canManageStructure = permission.resolve('manage_structure') === 'allow'
  const canEditFields = permission.resolve('edit_view') === 'allow'

  async function invalidateBase(baseTarget: string) {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: baseKeys.detail(baseTarget) }),
      queryClient.invalidateQueries({ queryKey: homeKeys.aggregate() }),
    ])
  }

  async function handleRenameBase(name: string) {
    if (base === null) return
    await trackWrite(() => renameBase(base.id, name))
    await invalidateBase(base.id)
  }

  async function handleDuplicateBase() {
    if (base === null) return
    const copy = await trackWrite(() => duplicateBase(base.id))
    await queryClient.invalidateQueries({ queryKey: homeKeys.aggregate() })
    navigate(`/base/${encodeURIComponent(copy.id)}`)
  }

  async function handleDeleteBase() {
    if (base === null) return
    await trackWrite(() => deleteBase(base.id))
    queryClient.removeQueries({ queryKey: baseKeys.detail(base.id) })
    await queryClient.invalidateQueries({ queryKey: homeKeys.aggregate() })
    navigate('/home')
  }

  async function handleToggleStar() {
    if (base === null) return
    await trackWrite(() => toggleStar(base.id, !base.starred))
    await invalidateBase(base.id)
  }

  async function handleCreateTable(name: string) {
    const table = await trackWrite(() => createTable(baseId, name))
    await queryClient.invalidateQueries({ queryKey: tableKeys.list(baseId) })
    setSelectedTableId(table.id)
    setSelectedViewId(null)
  }

  async function handleRenameTable(tableId: string, name: string) {
    await trackWrite(() => renameTable(tableId, name))
    await queryClient.invalidateQueries({ queryKey: tableKeys.list(baseId) })
  }

  async function handleDuplicateTable(tableId: string) {
    await trackWrite(() => duplicateTable(tableId))
    await queryClient.invalidateQueries({ queryKey: tableKeys.list(baseId) })
  }

  async function handleDeleteTable(tableId: string) {
    await trackWrite(() => deleteTable(tableId))
    queryClient.removeQueries({ queryKey: viewKeys.list(tableId) })
    await queryClient.invalidateQueries({ queryKey: tableKeys.list(baseId) })
    if (selectedTableId === tableId) {
      setSelectedTableId(null)
      setSelectedViewId(null)
    }
  }

  if (baseQuery.isLoading) {
    return (
      <main className={styles.page}>
        <HealthNotice />
        <p role="status">正在加载 Base…</p>
      </main>
    )
  }

  if (baseQuery.isError) {
    return (
      <main className={styles.page}>
        <HealthNotice />
        <ErrorBox
          title={apiErrorTitle(baseQuery.error)}
          retryLabel="重新加载"
          onRetry={() => void baseQuery.refetch()}
        />
      </main>
    )
  }

  if (base === null) {
    return (
      <main className={styles.page}>
        <HealthNotice />
        <section className={styles.permissionEmpty}>
          <h1 className={styles.title}>你没有访问该表格的权限</h1>
          <p className={styles.permissionHint}>请确认链接是否正确，或联系多维表格管理员。</p>
          <Link to="/home">返回首页</Link>
        </section>
      </main>
    )
  }

  return (
    <main className={styles.page}>
      <HealthNotice />
      <WorkspaceTopBar
        base={base}
        saveState={saveState}
        onRename={handleRenameBase}
        onDuplicate={handleDuplicateBase}
        onDelete={handleDeleteBase}
        onToggleStar={handleToggleStar}
      />

      <section aria-label="表与视图栏" className={styles.workspaceBody}>
        {tablesQuery.isLoading ? (
          <p role="status">正在加载数据表…</p>
        ) : tablesQuery.isError ? (
          <ErrorBox
            title={apiErrorTitle(tablesQuery.error)}
            retryLabel="重新加载数据表"
            onRetry={() => void tablesQuery.refetch()}
          />
        ) : (
          <>
            <TableTabs
              tables={tables}
              currentTableId={currentTableId}
              canManageStructure={canManageStructure}
              onSelect={(tableId) => {
                setSelectedTableId(tableId)
                setSelectedViewId(null)
              }}
              onCreate={handleCreateTable}
              onRename={handleRenameTable}
              onDuplicate={handleDuplicateTable}
              onDelete={handleDeleteTable}
            />
            {currentTable === null ? (
              <p className={styles.emptyHint}>暂无数据表</p>
            ) : viewsQuery.isLoading ? (
              <p role="status">正在加载视图…</p>
            ) : viewsQuery.isError ? (
              <ErrorBox
                title={apiErrorTitle(viewsQuery.error)}
                retryLabel="重新加载视图"
                onRetry={() => void viewsQuery.refetch()}
              />
            ) : (
              <ViewBar
                views={views}
                currentViewId={currentView?.id ?? null}
                onSelect={setSelectedViewId}
              />
            )}
          </>
        )}
      </section>

      <GridArea
        tableId={currentTableId}
        fields={fields}
        fieldsLoading={fieldsQuery.isLoading}
        fieldsError={fieldsQuery.error}
        onRetryFields={() => void fieldsQuery.refetch()}
        canManageStructure={canManageStructure && currentTableId !== null}
        canEditFields={canEditFields && currentTableId !== null}
        tables={tables}
        views={views}
      />
      <WorkspaceFooter />
    </main>
  )
}
