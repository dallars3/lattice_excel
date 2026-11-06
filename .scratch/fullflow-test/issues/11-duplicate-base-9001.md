# 复制 Base 恒返回 9001：workspace 未接线记录复制器

Status: needs-triage
Severity: P1
Source: TC-020-07（API）

## 复现步骤

1. owner 创建含数据表/记录/视图的 Base。
2. `POST /api/v1/bases/{id}/duplicate`。

## 实际

`{"code":9001,"message":"服务暂不可用"}`，未创建副本。

## 期望

按 TC-020-07：结构 + 数据 + 视图复制；评论与历史不复制。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-020-07`（连续多轮稳定复现）
- 代码：`excel_core_service/cmd/main.go` 的 `workspace.NewService(...)` 中 `RecordsDuplicator` 传 `nil`，`DuplicateBase` 在 `service.records == nil` 时直接返回 9001。

## 影响

P1 功能整条不可用；H-25 契约失败，前端「复制 Base」必然报服务不可用。
