# 04: ws-notification（通知 WS 推送，P1）

**What to build:** WS 帧契约新增通知消息（编号顺延 W-16 后，含通知 id、类型、所属 Base、发生时间）；hub 订阅 worker 已发布的 `notify.created` 事件，按用户 id 寻址推送；帧无序号、不进快照与重放；通知生产侧零改动；30 秒轮询保留为兜底。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 目标用户在线（任意表房间）收到通知帧，其他用户不收
- [ ] 同一用户多个连接均收到
- [ ] 通知帧不参与快照与重放（补发回放不含该帧；无 seq 语义）
- [ ] 离线错过不补发，重连后由拉取接口兜底
- [ ] proto 重生成脚本执行通过；hub 测试以内存事件总线发事件断言收帧（仿既有 hub 广播测试）
- [ ] 文档护栏脚本全绿

## Answer
已落地。`webapi.proto` Frame oneof 新增 `Notification notification = 18`（W-17；17 号已被 Ack 占用故顺延，字段 notification_id/type/base_id/occurred_at_ms，无 seq）；`Hub` 增 users 索引按 user_id 定向（多连接均收）；subscribers 订阅 `notify.created`；`isReplayableFrame`/`frameSequence` 对通知帧保持 false/无 seq，不进快照与重放；离线不补发、30s 轮询兜底不变；`redis/reddot.go` 加注释保持不接线。测试：内存总线发事件断言收帧。Evidence: merge `cea1aaf`。