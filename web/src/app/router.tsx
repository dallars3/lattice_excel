import { Navigate, Route, Routes } from 'react-router-dom'

import { LoginPage } from '@/features/auth/LoginPage'
import { GridDemoPage } from '@/features/grid-view/GridDemoPage'
import { HomePage } from '@/features/workspace/HomePage'
import { BaseWorkspacePage } from '@/features/workspace/BaseWorkspacePage'

// 路由表（票 02 建 / 票 09 增演示路由）：history 模式（BrowserRouter 在 App 装配）。
// dev 下刷新不白屏由 Vite historyApiFallback 天然满足；build 产物整站回退为后端静态托管项（技术方案第 16 章）。
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/home" replace />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/home" element={<HomePage />} />
      <Route path="/base/:id" element={<BaseWorkspacePage />} />
      {/* 演示路由（票 09）：dev 专属语义 = 路由恒注册、生产亦可访问，但仅消费 MSW 假数据
          （worker 仅 dev 启用，见 src/mocks/browser.ts；生产环境请求不可达、页面呈错误态）。
          非主站三条路由之一；M2 真实网格接入 /base/:id 后保留作 grid 能力演示入口。 */}
      <Route path="/demo/grid" element={<GridDemoPage />} />
      <Route path="*" element={<Navigate to="/home" replace />} />
    </Routes>
  )
}
