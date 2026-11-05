import { Navigate, Route, Routes } from 'react-router-dom'

import { LoginPage } from '@/features/auth/LoginPage'
import { RegisterPage } from '@/features/auth/RegisterPage'
import { ReloginModal } from '@/features/auth/ReloginModal'
import { RequireSession } from '@/features/auth/RequireSession'
import { GridDemoPage } from '@/features/grid-view/GridDemoPage'
import { HomePage } from '@/features/workspace/HomePage'
import { BaseWorkspacePage } from '@/features/workspace/BaseWorkspacePage'

// 路由表（票 02 建 / 票 09 增演示路由 / 票 01 认证闭环）：
// /home、/base/:id 经 RequireSession 守卫；2001 全局重登弹窗随路由树常驻。
export function AppRoutes() {
  return (
    <>
      <Routes>
        <Route path="/" element={<Navigate to="/home" replace />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route
          path="/home"
          element={
            <RequireSession>
              <HomePage />
            </RequireSession>
          }
        />
        <Route
          path="/base/:id"
          element={
            <RequireSession>
              <BaseWorkspacePage />
            </RequireSession>
          }
        />
        {/* 演示路由（票 09）：dev 专属语义 = 路由恒注册、生产亦可访问，但仅消费 MSW 假数据
            （worker 仅 dev 启用，见 src/mocks/browser.ts；生产环境请求不可达、页面呈错误态）。
            非主站三条路由之一；M2 真实网格接入 /base/:id 后保留作 grid 能力演示入口。 */}
        <Route path="/demo/grid" element={<GridDemoPage />} />
        <Route path="*" element={<Navigate to="/home" replace />} />
      </Routes>
      <ReloginModal />
    </>
  )
}
