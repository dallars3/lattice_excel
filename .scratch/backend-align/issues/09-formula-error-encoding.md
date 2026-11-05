# 09: formula-error-encoding（N5 公式错误编码与透传）

**What to build:** 公式错误值以字符串哨兵前缀 `#ERROR:` 后跟原因编码（无原因时仅 `#ERROR`），落库、记录读取、历史读取统一该编码；重算状态（实时帧与服务间响应）增原因字段透传；求值服务已产出原因，生产逻辑不改。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 给定求值错误，落库与读取的值含 `#ERROR:原因`；无原因时为 `#ERROR`
- [ ] 重算完成帧携带原因
- [ ] 非错误值编码不受影响
- [ ] proto 重生成脚本执行通过；编解码与链路测试全绿
- [ ] 文档护栏脚本全绿（RPC 计数保持 111）

## Answer
已落地（N5 错误编码与透传）。落库/读取统一 `#ERROR:<原因>`（无原因 `#ERROR`）：`recompute.go` computeCells 经 `errorCellValue(reason)` 编码、物化表新增 reason（迁移 `000018_calc_reason.{up,down}.sql`）；`calc.proto` GetRecalcStatusResponse.reason=2 与 `webapi.proto` RecalcStatus.reason=4 透传；求值生产逻辑未改（EvalResult.reason 原已产出），帧接线按裁定非本票范围；非错误值不变。Evidence: merge `5b55342`。