import { BrowserRouter } from 'react-router-dom'

import { AppProviders } from './providers'
import { AppRoutes } from './router'

// 应用装配根（票 02）：QueryClient 挂根 + BrowserRouter（history 模式）路由出口。
// Zustand store 为模块级单例，天然全树可达（见 providers.tsx 说明）。
export function App() {
  return (
    <AppProviders>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </AppProviders>
  )
}
