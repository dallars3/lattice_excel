# 01: 实时通道修复（事件序号 + 会话吊销关闭）

**What to build:** 协作参与者收到的变更帧与快照都带单调递增的持久序号，断线重连能按序号补齐；被移除协作者或被关闭链接的访客在收到吊销帧后连接由服务端关闭，且不影响其他用户与其它 Base。

**Blocked by:** None (can start immediately)

Status: done

- [x] 记录写入产生的实时事件在发布时携带持久单调序号：事件头部序号为 0 时用 outbox 行的持久自增序号填充，已有非零值保持不变
- [x] 单元格变更、快照、结构变更、记录删除帧显式携带序号（含 0）与空列表字段，帧形状稳定
- [x] 双连接加入同一 Base/数据表：后加入者快照含两人，先加入者收到在场帧；快照序号与后续帧单调一致（TC-090-01）
- [x] 单元格更新后 ≤1s 广播至另一连接，值与版本正确且序号单调递增（TC-090-02）
- [x] 断线期间产生 N 条变更，重连按最后序号请求补齐，收到缺失增量且无缺口、无重复
- [x] 移除协作者后，目标用户收到 `perm_changed` 吊销帧且连接被关闭；其他用户连接保持（TC-090-08）
- [x] 关闭链接后，仅该 Base 的链接会话收到 `link_closed` 并断开；内部成员与其它 Base 连接不受影响
- [x] 重复吊销事件幂等，不重复关闭、不报错
- [x] 包级测试覆盖 outbox 序号填充、帧序列化、吊销后关闭；相关 P0 用例全绿

## Comments

**根因**

- 业务事件（记录写入、calc、share、iam）入队时 `seq` 常为 0，`BusStore.ClaimUnpublished` 又不读取 outbox 行 `id`，转发发布时 `seq` 原样为 0；实时 Hub 的 `frameSequence` 只能看到 0，快照与后续变更帧无法判断先后，断线补齐失去水位。
- `protojson` 默认省略零值：`seq=0` 与空 `member_list` 直接从线上消失，帧形状随数据变化。
- `handleLinkClosed` 向整个 Base 的所有房间广播且从不关闭连接；`handleShareChanged` 定向发帧同样不关闭；而 `client.close()` 会立即关闭底层连接，可能丢掉刚入队的 `session_revoked`。

**改动**

- `excel_core_service/base/bus.go`：`ClaimUnpublished` SELECT 增加 `id`；扫描后若事件头 `seq==0` 则用行 id 填充。只做内存填充、不回写 DB：与 `OutboxEventsSince` 的 `seq>0 ? seq : id` 口径天然一致，且省一次写入。
- `excel_web_api/internal/realtime/framecodec.go`：`marshalFrame` 后按帧类型补 `seq`（含 0），`snapshot` 补空 `member_list: []`；不开启 `EmitUnpopulated`，避免未设置的 message 字段变成 `null`。
- `excel_web_api/internal/realtime/client.go`：新增 `revoke()` + `closeAfterFlush`，写协程刷出吊销帧且队列排空后再关闭连接；重复调用只投递一次。
- `excel_web_api/internal/realtime/subscribers.go`：`link_closed` 只吊销 `principal.link_context.base_id` 匹配该 Base 的连接；`perm_changed` 吊销 `hub.users[user_id]` 下全部在线连接。
- 测试：`base/bus/outbox_test.go`（序号填充 + 与 `OutboxEventsSince` 一致性）、`realtime/framecodec_test.go`、`realtime/hub_framecodec_test.go`（原始 JSON 形状）、`realtime/hub_join_test.go`（baseSeq 最大值与单调）、`realtime/hub_revocation_test.go`（关闭/定向/幂等）。`share/link_events_test.go` 与 `calc/recompute_test.go` 中「转发后 seq 必须为 0」的旧断言改为「>0」，与新的发布序号语义对齐。

**验证**

- `excel_core_service`：`go test -p 1 ./... -count=1` → 全部 ok（本机常驻的 `excel-core-service` 会抢占 outbox 行，跑全量前需临时停掉；仓库 contract-test 同样使用 `-p 1`）。
- `excel_web_api`：`go test ./... -count=1` → 全部 ok。
- `powershell -ExecutionPolicy Bypass -File docs\tools\check-docs.ps1` → 退出码 0。
