# 06: row-unsolved-badge（N2 行级评论角标）

**What to build:** 记录页响应顶层增「记录 id → 未解决评论数」映射；每页一次批量聚合（已打开状态评论、命中页内记录集），无逐行查询；单记录主体结构不动。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 页内记录返回未解决计数映射，与评论数据分布一致
- [ ] 无评论或全部已解决时映射为空或全 0，行为稳定
- [ ] 聚合一次完成（不存在按行查询）
- [ ] proto 重生成脚本执行通过；服务层测试覆盖正常路径与无评论边界
- [ ] 文档护栏脚本全绿（RPC 计数保持 111）

## Answer
已落地（N2）。`records.proto` QueryViewResponse 增 `map<string,int32> unsolved_comment_count_map = 4`；`base/comment.go` 增 `UnsolvedCounts` 单次 `COUNT GROUP BY record_id`（status=OPEN），每页一次聚合无 N+1；口径：未解决>0 的记录才出 key；单记录主体结构不动。Evidence: merge `5faae49`。