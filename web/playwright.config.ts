import { defineConfig } from '@playwright/test'

// Playwright 基础配置（票 09）：仅安装 + 配置就绪，可加载可列出（无用例要求）——
// spec Out of Scope：不写 E2E 用例、不纳入 M1 验收，用例编写随 M5 收口。
// 默认不起 webServer（不下载浏览器、不依赖网络）；M5 起按需启用：
//   webServer: { command: 'npm run dev', url: 'http://localhost:5173/', reuseExistingServer: true }
// 并执行 npx playwright install chromium 安装浏览器。
export default defineConfig({
  testDir: './e2e',
  reporter: [['list']],
  use: {
    baseURL: 'http://localhost:5173/',
  },
})
