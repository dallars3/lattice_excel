# H-40 维护选项恒返回 9001

Status: needs-triage
Severity: P2
Source: TC-040-21（API）

## 复现步骤

1. 对已存在单选字段，`PUT /api/v1/fields/{id}/options`，body 为当前 `option_list` 全量回写（不新增任何值）。

## 实际

`{"code":9001,"message":"服务暂不可用"}`；带重复名称的写入同样 9001。

## 期望

按 H-40：全量替换选项应成功；同名（去空格/忽略大小写）应归一而不是失败。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-040-21`
- `.scratch/fullflow-test/probe2.mjs`：owner 会话 baseline 写入即 9001
- 代码：`structure.UpdateOptions` 直接返回 `repository.UpdateField` 的原始错误（未映射），最终呈现 9001。

## 影响

字段「维护选项」UI 与选项改名/调色/排序均不可用；P1 字段配置链路存在断点。
