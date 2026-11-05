import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { isApiError, type ApiError } from '@/core/http'
import { logoutAndClear } from '@/features/auth/logout'
import { HealthNotice } from '@/features/health/HealthNotice'
import { PermissionGate, usePermission } from '@/features/permissions'
import { homeKeys } from '@/query-keys'
import { Button, EmptyState } from '@/ui-kit'

import {
  createBase,
  deleteBase,
  duplicateBase,
  fetchHome,
  renameBase,
  searchBases,
  toggleStar,
  type BaseSummary,
} from './api'
import { BaseCard } from './components/BaseCard'
import styles from './HomePage.module.css'

const MAX_BASE_NAME_LENGTH = 50
const MAX_SEARCH_LENGTH = 50

interface RenameTarget {
  base: BaseSummary
  draft: string
  error: string | null
}

function errorText(error: unknown): string {
  if (error === null || error === undefined) return ''
  if (isApiError(error)) return `${error.catalog.title}。${error.catalog.action}`
  return '操作失败，请稍后重试'
}

function apiError(error: unknown): ApiError | null {
  return isApiError(error) ? error : null
}

/** 首页：Base 四区聚合、名称搜索、新建与卡片管理（票 02）。 */
export function HomePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const pagePermission = usePermission()
  const [searchTerm, setSearchTerm] = useState('')
  const [renameTarget, setRenameTarget] = useState<RenameTarget | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<BaseSummary | null>(null)

  const trimmedSearch = searchTerm.trim()
  const searchActive = trimmedSearch.length > 0 && trimmedSearch.length <= MAX_SEARCH_LENGTH

  const homeQuery = useQuery({
    queryKey: homeKeys.aggregate(),
    queryFn: fetchHome,
    retry: false,
  })

  const searchQuery = useQuery({
    queryKey: homeKeys.search(trimmedSearch),
    queryFn: () => searchBases(trimmedSearch),
    enabled: searchActive,
    retry: false,
  })

  /** 写成功后按白名单失效：聚合必失效；搜索仅在激活时失效。 */
  function invalidateHome(): void {
    void queryClient.invalidateQueries({ queryKey: homeKeys.aggregate() })
    if (searchActive) {
      void queryClient.invalidateQueries({ queryKey: homeKeys.searchScope() })
    }
  }

  const createMutation = useMutation({
    mutationFn: () => createBase(''),
    onSuccess: (base) => {
      invalidateHome()
      navigate(`/base/${encodeURIComponent(base.id)}`)
    },
  })

  const renameMutation = useMutation({
    mutationFn: (input: { baseId: string; name: string }) => renameBase(input.baseId, input.name),
    onSuccess: () => {
      invalidateHome()
      setRenameTarget(null)
    },
  })

  const duplicateMutation = useMutation({
    mutationFn: (baseId: string) => duplicateBase(baseId),
    onSuccess: () => invalidateHome(),
  })

  const deleteMutation = useMutation({
    mutationFn: (baseId: string) => deleteBase(baseId),
    onSuccess: () => {
      invalidateHome()
      setDeleteTarget(null)
    },
  })

  const starMutation = useMutation({
    mutationFn: (input: { baseId: string; starred: boolean }) =>
      toggleStar(input.baseId, input.starred),
    onSuccess: () => invalidateHome(),
  })

  async function handleLogout() {
    await logoutAndClear(queryClient)
    navigate('/login', { replace: true })
  }

  function openRename(base: BaseSummary) {
    renameMutation.reset()
    setRenameTarget({ base, draft: base.name, error: null })
  }

  function submitRename(event: FormEvent) {
    event.preventDefault()
    if (renameTarget === null) return
    const name = renameTarget.draft.trim()
    if (name.length < 1 || name.length > MAX_BASE_NAME_LENGTH) {
      setRenameTarget({
        ...renameTarget,
        error: `名称需为 1–${MAX_BASE_NAME_LENGTH} 个字符`,
      })
      return
    }
    setRenameTarget({ ...renameTarget, error: null })
    renameMutation.mutate({ baseId: renameTarget.base.id, name })
  }

  function openDelete(base: BaseSummary) {
    deleteMutation.reset()
    setDeleteTarget(base)
  }

  function renderCard(base: BaseSummary) {
    return (
      <BaseCard
        key={base.id}
        base={base}
        onOpen={(target) => navigate(`/base/${encodeURIComponent(target.id)}`)}
        onRename={openRename}
        onDuplicate={(target) => duplicateMutation.mutate(target.id)}
        onDelete={openDelete}
        onToggleStar={(target) => starMutation.mutate({ baseId: target.id, starred: !target.starred })}
      />
    )
  }

  function renderSections() {
    const home = homeQuery.data
    if (home === undefined) return null
    const sections = [
      { key: 'star', title: '星标', bases: home.starList, empty: '暂无星标的多维表格' },
      { key: 'recent', title: '最近访问', bases: home.recentList, empty: '暂无最近访问的多维表格' },
      { key: 'created', title: '我创建的', bases: home.createdList, empty: '还没有创建过多维表格' },
      { key: 'shared', title: '与我共享', bases: home.sharedList, empty: '暂无与你共享的多维表格' },
    ]
    const visible = pagePermission.internal ? sections : sections.filter((item) => item.key === 'shared')

    return visible.map((section) => (
      <section key={section.key} className={styles.section} aria-label={section.title}>
        <h2 className={styles.sectionTitle}>{section.title}</h2>
        {section.bases.length === 0 ? (
          section.key === 'shared' ? (
            <EmptyState
              title="暂无与你共享的多维表格"
              description="他人将多维表格共享给你后，会出现在这里。"
            />
          ) : (
            <p className={styles.empty}>{section.empty}</p>
          )
        ) : (
          <div className={styles.grid}>{section.bases.map((base) => renderCard(base))}</div>
        )}
      </section>
    ))
  }

  function renderSearchResults() {
    if (!searchActive) return null
    if (searchQuery.isLoading) {
      return <p role="status">正在搜索…</p>
    }
    if (searchQuery.isError) {
      return (
        <div role="alert" className={styles.errorBox}>
          <p>{errorText(searchQuery.error)}</p>
          <Button variant="secondary" onClick={() => void searchQuery.refetch()}>
            重新搜索
          </Button>
        </div>
      )
    }
    const results = searchQuery.data ?? []
    return (
      <section className={styles.section} aria-label="搜索结果">
        <h2 className={styles.sectionTitle}>搜索结果</h2>
        {results.length === 0 ? (
          <p className={styles.empty}>没有找到匹配「{trimmedSearch}」的多维表格</p>
        ) : (
          <div className={styles.grid}>{results.map((base) => renderCard(base))}</div>
        )}
      </section>
    )
  }

  const createError = apiError(createMutation.error)
  const pageActionError = apiError(duplicateMutation.error) ?? apiError(starMutation.error)

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>协作多维表格</h1>
        <Button variant="secondary" onClick={() => void handleLogout()}>
          退出登录
        </Button>
      </header>
      <HealthNotice />
      <div className={styles.toolbar}>
        <input
          type="search"
          className={styles.search}
          aria-label="搜索多维表格名称"
          placeholder="搜索多维表格名称"
          maxLength={MAX_SEARCH_LENGTH}
          value={searchTerm}
          onChange={(event) =>
            setSearchTerm(event.target.value.slice(0, MAX_SEARCH_LENGTH))
          }
        />
        <PermissionGate state={pagePermission.resolve('create_base')}>
          <Button disabled={createMutation.isPending} onClick={() => createMutation.mutate()}>
            {createMutation.isPending ? '创建中…' : '新建多维表格'}
          </Button>
        </PermissionGate>
      </div>

      {createError === null ? null : (
        <div role="alert" className={styles.errorBox}>
          <p>{createError.catalog.title}</p>
          <p>{createError.catalog.action}</p>
        </div>
      )}
      {pageActionError === null ? null : (
        <div role="alert" className={styles.errorBox}>
          <p>{pageActionError.catalog.title}</p>
        </div>
      )}

      {searchActive ? (
        renderSearchResults()
      ) : homeQuery.isLoading ? (
        <p role="status">正在加载首页…</p>
      ) : homeQuery.isError ? (
        <div role="alert" className={styles.errorBox}>
          <p>{errorText(homeQuery.error)}</p>
          <Button variant="secondary" onClick={() => void homeQuery.refetch()}>
            重新加载
          </Button>
        </div>
      ) : (
        renderSections()
      )}

      {renameTarget === null ? null : (
        <div className={styles.mask}>
          <div role="dialog" aria-modal="true" aria-labelledby="rename-base-title" className={styles.dialog}>
            <h2 id="rename-base-title" className={styles.dialogTitle}>
              重命名多维表格
            </h2>
            <form onSubmit={submitRename} className={styles.form}>
              <label htmlFor="rename-base-input">Base 名称</label>
              <input
                id="rename-base-input"
                value={renameTarget.draft}
                maxLength={MAX_BASE_NAME_LENGTH}
                onChange={(event) =>
                  setRenameTarget({ ...renameTarget, draft: event.target.value, error: null })
                }
              />
              {renameTarget.error === null ? null : <p role="alert">{renameTarget.error}</p>}
              {renameMutation.error === null ? null : (
                <p role="alert">{errorText(renameMutation.error)}</p>
              )}
              <div className={styles.dialogActions}>
                <Button
                  type="submit"
                  disabled={renameMutation.isPending || renameTarget.draft.trim() === ''}
                >
                  保存
                </Button>
                <Button variant="secondary" onClick={() => setRenameTarget(null)}>
                  取消
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {deleteTarget === null ? null : (
        <div className={styles.mask}>
          <div role="dialog" aria-modal="true" aria-labelledby="delete-base-title" className={styles.dialog}>
            <h2 id="delete-base-title" className={styles.dialogTitle}>
              删除多维表格
            </h2>
            <p>确定删除「{deleteTarget.name}」吗？</p>
            <p className={styles.dangerHint}>删除后不可恢复（P0）</p>
            {deleteMutation.error === null ? null : (
              <p role="alert">{errorText(deleteMutation.error)}</p>
            )}
            <div className={styles.dialogActions}>
              <Button
                variant="danger"
                disabled={deleteMutation.isPending}
                onClick={() => deleteMutation.mutate(deleteTarget.id)}
              >
                确认删除
              </Button>
              <Button variant="secondary" onClick={() => setDeleteTarget(null)}>
                取消
              </Button>
            </div>
          </div>
        </div>
      )}
    </main>
  )
}
