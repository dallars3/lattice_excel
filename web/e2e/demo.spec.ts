import { expect, test } from '@playwright/test'

// M1 占位（spec Out of Scope：不写 E2E 用例、不纳入验收）：仅使 `npx playwright test --list`
// 以 0 用例正常退出（空 testDir 会以 "No tests found" 非零退出）；真实用例随 M5 收口补齐。
// fixme = 永不实际执行（即便将来浏览器已安装也不会启动浏览器、不依赖网络）。
test.fixme('占位：E2E 用例随 M5 收口补齐', async () => {
  expect(true).toBe(true)
})
