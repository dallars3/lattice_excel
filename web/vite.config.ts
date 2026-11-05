import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'

import react from '@vitejs/plugin-react'
import { configDefaults, defineConfig, type Plugin } from 'vitest/config'

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

// MSW service worker 仅 dev 服务（票 09）：apply:'serve' 使本插件不进 build；
// SW 脚本放 src/mocks（经 npx msw init 生成后拷入、随 msw 版本对齐），不进 public/
// ——避免产物包含 mockServiceWorker.js（MSW 不进产物，spec 依赖治理）。
function mswDevWorkerPlugin(): Plugin {
  return {
    name: 'msw-dev-worker',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use('/mockServiceWorker.js', (_req, res) => {
        res.setHeader('Content-Type', 'text/javascript')
        res.end(readFileSync(fileURLToPath(new URL('./src/mocks/mockServiceWorker.js', import.meta.url)), 'utf8'))
      })
    },
  }
}

export default defineConfig({
  plugins: [react(), mswDevWorkerPlugin()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks,
        // 产物带内容 hash 且形如 [name].[hash].js，网关据此识别长缓存资产
        // （excel_web_api/internal/static/handler.go 的 hashedAsset 规则）。
        entryFileNames: 'assets/[name].[hash].js',
        chunkFileNames: 'assets/[name].[hash].js',
        assetFileNames: 'assets/[name].[hash][extname]',
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    // 票 04：MSW 生命周期接线（组件/模块测试唯一 HTTP 边界，见 src/test/setup.ts）。
    setupFiles: ['./src/test/setup.ts'],
    // 票 09：e2e/ 归 Playwright（@playwright/test 的 spec 不能被 vitest 收集）
    exclude: [...configDefaults.exclude, 'e2e/**'],
    environmentOptions: {
      jsdom: {
        url: 'http://localhost:3000/',
      },
    },
  },
})
