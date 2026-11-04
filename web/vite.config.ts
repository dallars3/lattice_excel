import { fileURLToPath, URL } from 'node:url'

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// 首屏并发 chunk ≤6：网关无 HTTP/2（对齐结论 #4），按 HTTP/1.1 硬约束分组。
// 函数式 manualChunks：分组仅在依赖真正进入构建图时产生 chunk，不产生空文件。
const manualChunks = (id: string): string | undefined => {
  if (!id.includes('node_modules')) {
    return undefined
  }
  if (/[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) {
    return 'vendor-react'
  }
  if (/[\\/]node_modules[\\/](react-router|react-router-dom)[\\/]/.test(id) || id.includes('@remix-run')) {
    return 'vendor-router'
  }
  if (/[\\/]node_modules[\\/]@tanstack[\\/]/.test(id)) {
    return 'vendor-query'
  }
  if (/[\\/]node_modules[\\/](zustand|use-sync-external-store)[\\/]/.test(id)) {
    return 'vendor-state'
  }
  return 'vendor'
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks,
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    // 票 04：MSW 生命周期接线（组件/模块测试唯一 HTTP 边界，见 src/test/setup.ts）。
    setupFiles: ['./src/test/setup.ts'],
    environmentOptions: {
      jsdom: {
        url: 'http://localhost:3000/',
      },
    },
  },
})
