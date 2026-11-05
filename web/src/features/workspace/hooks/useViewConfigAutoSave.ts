// 视图配置即时保存（票 07）：本地立即更新，串行提交 H-47；提交中合并连续改动。
// - 成功：回读服务端响应并覆盖本地；失败：保留本地改动并暴露错误/重试。
// - 每次 H-47 经 trackWrite，计入顶栏「保存中」。
import { useCallback, useEffect, useRef, useState } from 'react'

import { trackWrite } from '@/stores'

import { updateViewConfig } from '../api/view'
import { apiErrorText } from '../errorText'
import { createEmptyViewConfig, type ViewConfig } from '../types/view-config'

export interface UseViewConfigAutoSaveOptions {
  viewId: string | null
  /** H-46 读取结果；null 表示未加载/切换中。 */
  serverConfig: ViewConfig | null
  /** 成功回读后同步查询缓存，避免 serverConfig 旧值覆盖本地。 */
  onSaved?: (viewId: string, config: ViewConfig) => void
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
          configRef.current = saved
          setConfig(saved)
          onSavedRef.current?.(id, saved)
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
      const next = updater(configRef.current ?? createEmptyViewConfig())
      configRef.current = next
      setConfig(next)
      pendingRef.current = next
      setError(null)
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
