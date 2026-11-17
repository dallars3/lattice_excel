# 重置分享链接后旧 token 仍被缓存命中并 302 到 Base

Status: needs-triage
Severity: P0
Source: TC-130-02（API）

## 复现步骤

1. owner 开启「任何人」链接，得到旧 token 与 URL。
2. 调用 `POST /api/v1/bases/{id}/link/reset` 获取新 URL。
3. 立即用旧 token 访问 `/s/{old_token}`（带链接会话 Cookie/Token）。

## 实际

- 旧 token 仍返回 `302 Location: /bases/{base_id}`（命中 Redis 中未清除的 token→base 映射）。
- 期望的失效页 `/err?kind=share-link-unavailable` 不出现。

## 期望

按 TC-130-02 / AC-SHARE-04：重置后旧链立即失效（失效页或 403）。

## 证据

- API 执行：`reset=0, newDiff=true, oldVisit=302 /bases/01M6..., staleSession=code=3001 after 0s`
- 结果文件：`.scratch/fullflow-test/p0-api-results.json` → `TC-130-02`
- 根因线索：`ShareLinkHandler.resolveCached` 先查 Redis；`ResetLink` 未同步删除旧 token 缓存键（TTL 55s）。会话侧旧链接已 3001（与 #06 同一放行缺陷）。

## 影响

P0：撤销/重置外链不能即时生效，存在最长 55s 的继续访问窗口；与 AC-SHARE-04「旧链立即失效」冲突。
