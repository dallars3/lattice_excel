import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/styles/global.css'

import { App } from './App'

const rootElement = document.getElementById('root')
if (rootElement === null) {
  throw new Error('未找到 #root 挂载点')
}

// 票 01：仅挂占位首页；路由表与 QueryClient/realtimeClient 由票 02 接入。
createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
