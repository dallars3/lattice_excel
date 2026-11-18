import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { themeStore } from '@/stores/theme'
import '@/styles/global.css'
import '@/styles/dark.css' // 暗色主题（纸墨 Paper·Ink）：[data-theme='dark'] 时整树覆盖 token

import { App } from './App'

// 票 02：App 内完成装配（QueryClient 挂根 + BrowserRouter history 模式路由）。
// 票 09：MSW 只进 dev（spec 依赖治理）——DEV 分支经构建期静态消除，动态 import 的
// msw/browser 与 worker 启动不进产物（build 输出无 msw chunk）；生产直接渲染。
function mount(): void {
  const rootElement = document.getElementById('root')
  if (rootElement === null) {
    throw new Error('未找到 #root 挂载点')
  }
  createRoot(rootElement).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

async function bootstrap(): Promise<void> {
  // 主题先于首帧同步（index.html 内联脚本已预置 data-theme，这里对齐 store 状态）
  themeStore.getState().initFromDocument()
  if (import.meta.env.DEV) {
    const { enableDemoMocks } = await import('@/mocks/browser')
    await enableDemoMocks()
  }
  mount()
}

void bootstrap()
