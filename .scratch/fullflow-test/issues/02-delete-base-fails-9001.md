# 删除 Base 恒失败：H-26 返回 9001，Base 未被删除

Status: needs-triage
Severity: P0
Source: TC-020-03（UI 冒烟 + API 复核）

## 复现步骤

1. 以 owner 身份创建含默认表的 Base。
2. 首页卡片菜单 →「删除」→ 确认「确认删除」。
3. 观察返回码与首页列表；或直接 API：
   `DELETE /api/v1/bases/{base_id}`，body `{"confirm":true}`。

## 实际

- 接口返回 `{"code":9001,"message":"服务暂不可用"}`。
- Base 仍存在于首页（最近访问 + 我创建的），数据库 `excel_workspace_bases` 记录未删除。
- UI 弹窗保持打开，卡片不消失。

## 期望

按 TC-020-03 / H-26：二次确认后 Base 从列表移除，API 侧不可再访问。

## 证据

- Playwright：`TC-020-03 | 删除 Base 强提示`（确认后 15s 内卡片仍为 2 个，来自两个分区）
- API 诊断：`DELETE /api/v1/bases/01M6QJYVP3D8NAAAFW4XKARFK5` → 503 code 9001；随后 `GET /tables` 仍返回该表。
- 代码：`excel_core_service/cmd/main.go` 第 163 行将 workspace 的 records/comment 清理 seam 传 `nil`；`internal/workspace/delete_base.go` 在有表时走 `recordDeleter == nil` → `CodeServiceUnavailable`。

## 影响

P0 阻塞：任何含表的 Base 都无法删除，且错误码误导为服务不可用。需接线 records/comments 清理适配器（或改为明确业务错误并保证幂等）。
