# 可填写协作者编辑他人记录返回 4002 而非 3001

Status: needs-triage
Severity: P0（错误码契约偏差；数据未被修改）
Source: TC-080-04（API）

## 复现步骤

1. owner 在 Base 中创建记录 R。
2. 将 filler 加入 Base（role=可填写）并登录。
3. filler 直接调用：
   `PUT /api/v1/records/{R}/cells/{field_id}?base_id={base}`，body `{"value":88,"expect_version":<当前版本>}`。

## 实际

- 返回 `{"code":4002,"message":"对象已被删除"}`（HTTP 409）。
- 记录值未被修改（权限拦截本身生效）。

## 期望

按 TC-080-04 / AC-PERM-03：越权写返回 `3001`（HTTP 403），数据不变。

## 证据

- API 执行结果：`viewerUpdate=3001, fillerDeleteTable=3001, fillerEditOther=4002`
- 结果文件：`.scratch/fullflow-test/p0-api-results.json`
- 代码路径：`excel_core_service/internal/records` 对 `ROW_SCOPE_OWN` 下他人记录统一映射 `CodeDeletedResource`。

## 影响

前端全局错误桥按 4001/3001 区分「冲突」与「权限变更」；返回 4002 会被当作对象已删除处理，无法触发「权限已变更，页面已转为只读」提示。需确认契约口径：若刻意用 4002 隐藏存在性，应更新 AC/前端映射。
