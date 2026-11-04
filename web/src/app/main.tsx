import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@/styles/global.css'
import '@/styles/dark.css' // 暗色扩展位（票 03）：空占位，默认不生效；单独文件避免 @import 内联空规则产生构建告警

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
