// 视图配置即时保存（票 07）：本地立即更新，串行提交 H-47；提交中合并连续改动。
// - 成功：回读服务端响应并覆盖本地；失败：保留本地改动并暴露错误/重试。
// - 每次 H-47 经 trackWrite，计入顶栏「保存中」。
import { useCallback, useEffect, useRef, useState } from 'react'

import { trackWrite } from '@/stores'

import { updateViewConfig } from '../api/view'
import { apiErrorText } from '../errorText'
import { encodeViewConfig } from '../types/view-config-codec'
import { createEmptyViewConfig, type ViewConfig } from '../types/view-config'
import { isFilterComplete } from '../types/view-config-validation'

/** 只比较实际会下发的字段：未填完的筛选不影响 H-47 载荷。 */
function encodedKey(config: ViewConfig): string {
  return JSON.stringify(encodeViewConfig(config))
}

export interface UseViewConfigAutoSaveOptions {
  viewId: string | null
  /** 当前表 id（race-closeout 01）：随成功回调透传，供调用方按表前缀失效记录查询。 */
  tableId?: string | null
  /** H-46 读取结果；null 表示未加载/切换中。 */
  serverConfig: ViewConfig | null
  /** 成功回读后同步查询缓存，避免 serverConfig 旧值覆盖本地；可返回 Promise，串行循环会等待其完成。 */
  onSaved?: (viewId: string, config: ViewConfig, tableId: string | null) => void | Promise<void>
}

export interface ViewConfigAutoSave {
  config: ViewConfig | null
  error: string | null
  isSaving: boolean
  update: (updater: (current: ViewConfig) => ViewConfig) => void
  retry: () => void
}

export function useViewConfigAutoSave({
  viewId,
  tableId,
  serverConfig,
  onSaved,
}: UseViewConfigAutoSaveOptions): ViewConfigAutoSave {
  const [config, setConfig] = useState<ViewConfig | null>(serverConfig)
  const [error, setError] = useState<string | null>(null)
  const [isSaving, setIsSaving] = useState(false)

  const configRef = useRef<ViewConfig | null>(serverConfig)
  const pendingRef = useRef<ViewConfig | null>(null)
  const inFlightRef = useRef(false)
  const viewRef = useRef<string | null>(viewId)
  const onSavedRef = useRef(onSaved)
  onSavedRef.current = onSaved
  const tableIdRef = useRef<string | null>(tableId ?? null)
  tableIdRef.current = tableId ?? null
  const runRef = useRef<(id: string) => Promise<void>>(async () => undefined)

  const run = useCallback(async (id: string) => {
    if (inFlightRef.current) return
    inFlightRef.current = true
    setIsSaving(true)
    let failureText: string | null = null

    while (viewRef.current === id && pendingRef.current !== null) {
      const next = pendingRef.current
      pendingRef.current = null
      try {
        const saved = await trackWrite(() => updateViewConfig(id, next))
        if (viewRef.current !== id) return
        // 仅当没有更新的改动时采用服务端回读，避免旧响应覆盖新草稿。
        if (pendingRef.current === null) {
          // 服务端不回存未填完的筛选；把本地面板里尚未填完的条件合并回来，避免输入行被响应抹掉。
          const localIncomplete =
            configRef.current?.filters.filter((filter) => !isFilterComplete(filter)) ?? []
          const merged =
            localIncomplete.length === 0
              ? saved
              : { ...saved, filters: [...saved.filters, ...localIncomplete] }
          configRef.current = merged
          setConfig(merged)
          // race-closeout 修复收口：onSaved 可能返回 Promise（如保存后按数据条件重取记录），
          // 串行循环等待其完成后再进入下一轮——突发连续改动时下一次提交排在重取之后，
          // 避免下一次保存的失效/重取与本轮重取交错（网格间歇性停滞的疑似根因）。
          await onSavedRef.current?.(id, merged, tableIdRef.current)
        }
        failureText = null
      } catch (failure) {
        if (viewRef.current !== id) return
        failureText = apiErrorText(failure)
        break
      }
    }

    if (viewRef.current !== id) return
    inFlightRef.current = false
    setIsSaving(false)
    setError(failureText)
    if (pendingRef.current !== null) void runRef.current(id)
  }, [])
  runRef.current = run

  // 视图切换重置本地态；服务端配置到达且无在途/草稿时同步（避免覆盖用户正在改的内容）。
  useEffect(() => {
    if (viewRef.current !== viewId) {
      viewRef.current = viewId
      pendingRef.current = null
      inFlightRef.current = false
      configRef.current = serverConfig
      setConfig(serverConfig)
      setError(null)
      setIsSaving(false)
      return
    }
    if (serverConfig === null) return
    if (pendingRef.current !== null || inFlightRef.current) return
    if (JSON.stringify(serverConfig) !== JSON.stringify(configRef.current)) {
      configRef.current = serverConfig
      setConfig(serverConfig)
    }
  }, [serverConfig, viewId])

  const update = useCallback(
    (updater: (current: ViewConfig) => ViewConfig) => {
      if (viewId === null) return
      const previous = configRef.current
      const next = updater(previous ?? createEmptyViewConfig())
      configRef.current = next
      setConfig(next)
      setError(null)
      // 新增但未填值的筛选只改本地草稿：实际载荷未变化，先不提交 H-47（补全值后才保存）。
      if (previous !== null && encodedKey(previous) === encodedKey(next)) return
      pendingRef.current = next
      void runRef.current(viewId)
    },
    [viewId],
  )

  const retry = useCallback(() => {
    if (viewId === null || configRef.current === null) return
    pendingRef.current = configRef.current
    setError(null)
    void runRef.current(viewId)
  }, [viewId])

  return { config, error, isSaving, update, retry }
}
