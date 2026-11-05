// 全局异常桥（票 09）：3002/5003/9001 统一提示与强制只读；离线横幅按 online/offline 切换。
import '@testing-library/jest-dom/vitest'

import type { ReactNode } from 'react'

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { http } from '@/core/http'
import { mockEnvelope } from '@/mocks/handlers'
import { mswServer } from '@/mocks/server'
import { workspaceStore } from '@/stores'

import {
  EXTERNAL_EXPORT_MESSAGE,
  OFFLINE_MESSAGE,
  PERMISSION_CHANGED_MESSAGE,
  RATE_LIMIT_MESSAGE,
  SERVICE_RETRY_MESSAGE,
} from './GlobalErrorBridge'
import { AppProviders, appQueryClient } from './providers'

function setOnline(online: boolean) {
  Object.defineProperty(window.navigator, 'onLine', {
    configurable: true,
    value: online,
  })
}

function TriggerRequest({ path }: { path: string }) {
  return (
    <button
      type="button"
      onClick={() => {
        void http.get(path).catch(() => undefined)
      }}
    >
      触发请求
    </button>
  )
}

function renderBridge(children: ReactNode) {
  render(<AppProviders>{children}</AppProviders>)
}

beforeEach(() => {
  appQueryClient.clear()
  workspaceStore.setState({ forcedReadOnly: false, offline: false, inFlightWriteCount: 0 })
  setOnline(true)
})

afterEach(() => {
  appQueryClient.clear()
  workspaceStore.setState({ forcedReadOnly: false, offline: false })
  setOnline(true)
})

describe('全局异常桥（3002 / 5003 / 9001）', () => {
  it('3002：转只读并同时提示权限变更与外部不可导出', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/probe', code: 3002 }))
    renderBridge(<TriggerRequest path="/api/v1/probe" />)

    fireEvent.click(screen.getByRole('button', { name: '触发请求' }))

    expect(await screen.findByText(PERMISSION_CHANGED_MESSAGE)).toBeInTheDocument()
    expect(screen.getByText(EXTERNAL_EXPORT_MESSAGE)).toBeInTheDocument()
    expect(workspaceStore.getState().forcedReadOnly).toBe(true)
  })

  it('5003：提示操作过于频繁', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/probe', code: 5003 }))
    renderBridge(<TriggerRequest path="/api/v1/probe" />)

    fireEvent.click(screen.getByRole('button', { name: '触发请求' }))
    expect(await screen.findByText(RATE_LIMIT_MESSAGE)).toBeInTheDocument()
  })

  it('9001：提示服务暂时不可用并说明重试上限', async () => {
    mswServer.use(mockEnvelope({ path: '/api/v1/probe', code: 9001 }))
    renderBridge(<TriggerRequest path="/api/v1/probe" />)

    fireEvent.click(screen.getByRole('button', { name: '触发请求' }))
    expect(await screen.findByText(SERVICE_RETRY_MESSAGE)).toBeInTheDocument()
  })
})

describe('离线横幅（online / offline）', () => {
  it('断网事件展示黄色横幅，恢复在线后自动收起', async () => {
    setOnline(true)
    renderBridge(<span>页面内容</span>)
    expect(screen.queryByText(OFFLINE_MESSAGE)).not.toBeInTheDocument()

    setOnline(false)
    window.dispatchEvent(new Event('offline'))
    expect(await screen.findByText(OFFLINE_MESSAGE)).toBeInTheDocument()

    setOnline(true)
    window.dispatchEvent(new Event('online'))
    await waitFor(() => expect(screen.queryByText(OFFLINE_MESSAGE)).not.toBeInTheDocument())
  })
})
