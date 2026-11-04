import { Navigate, Route, Routes } from 'react-router-dom'

import { LoginPage } from '@/features/auth/LoginPage'
import { HomePage } from '@/features/workspace/HomePage'
import { BaseWorkspacePage } from '@/features/workspace/BaseWorkspacePage'

// 路由表（票 02）：history 模式（BrowserRouter 在 App 装配）。
// dev 下刷新不白屏由 Vite historyApiFallback 天然满足；build 产物整站回退为后端静态托管项（技术方案第 16 章）。
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/home" replace />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/home" element={<HomePage />} />
      <Route path="/base/:id" element={<BaseWorkspacePage />} />
      <Route path="*" element={<Navigate to="/home" replace />} />
    </Routes>
  )
}
