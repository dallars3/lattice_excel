# 03: 删除 Base 级联清理

**What to build:** 可管理者删除含数据表与记录的 Base 时真正删除成功，Base 从首页消失、所有接口不可再访问，记录/历史/评论/链接/结构一并清理且无孤儿数据。

**Blocked by:** None (can start immediately)

Status: done

- [x] 含数据表与记录的 Base 删除返回成功信封，首页各分区不再出现
- [x] 删除后该 Base 的数据表、记录、视图、字段查询均返回「对象已删除」，不可再写入
- [x] 记录、记录历史、操作日志、计算物化行、评论、分享链接、数据表结构全部清理
- [x] 重复删除同一 Base 幂等；无数据表的 Base 直接删除成功
- [x] 清理失败时不提交 Base 删除，并返回明确错误，不再以 9001 掩盖
- [x] TC-020-03 UI 与 API 均通过；TC-030-03 删除表语义不回归
- [x] 包级测试覆盖清理适配器接线、失败不提交、幂等

## Comments

根因：`cmd/main.go` 构造 workspace 服务时 records/comment 清理 seam 传 `nil, nil`，含数据表的 Base 删除恒返回 9001；评论表只有 `record_id` 没有 Base/表归属，原顺序「记录 → 评论」在记录硬删后会让评论失去关联。

改动：
- `base/records_cleanup.go`：新增 `RecordsRepository.DeleteBaseRows`，单事务清理 `excel_records`（含软删行）、`excel_records_histories`、`excel_calc_materialized`、`excel_calc_jobs`，并按 `request_json` 可靠归属清理 `excel_records_oplog`（`table_id`、`record_id` 命中，或 `record_id_list` 全部属于被删记录）；无法归属的行保留，不盲删整表。
- `base/comment_cleanup.go`：新增 `CommentRepository.DeleteBaseComments`，经 `excel_records JOIN excel_structure_tables` 按 Base 删除评论（含软删记录上的评论）；无法解析的孤儿评论保留。
- `internal/records/base_rows_deleter.go`、`internal/comment/base_comments_deleter.go`：两个薄适配器实现 workspace seam。
- `internal/workspace/delete_base.go`：清理顺序改为「评论 → 记录 → 链接 → 结构 → Base 行」；任一清理失败立即返回明确错误且不删 Base；Base 不存在返回空信封成功（重复删除幂等）；新增两个 deleter 的必填校验。
- `cmd/main.go`：接线两个适配器，records/comment 仓库复用同一实例。
- 测试：workspace harness 改为真实 MySQL 适配器，新增级联零残留、失败保留 Base、重复删除、无表 Base 删除成功用例；records/comment 包新增适配器清理与归属边界测试（孤儿评论/跨 Base 评论保留）。

验证：`go test -p 1 ./...` 全绿（base/records/comment/workspace 等 13 个包）；`go build ./...` 通过；`docs\tools\check-docs.ps1` 退出码 0。

遗留：本轮之前已被硬删除记录留下的孤儿评论无法经现有 schema 归属，保留不动（需运维按数据审计单独处理）；操作日志只清理可归属行，剩余幂等缓存行按原过期窗口自然淘汰。

