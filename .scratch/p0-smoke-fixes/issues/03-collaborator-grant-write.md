# 03: 协作者授权与可写性（filler 授权 / 条目 id / 9001 瞬态）

**What to build:** 管理者能把协作者设为「可填写」并在添加后立即可正常协作：H-64 授权接受 filler（外部上限仍为 editor）；H-63 协作者条目携带稳定 id 且 username/display_name 为真实值（外部条目不得落用户 id）；H-65/H-66 以该 id 成功调整角色与移除；新协作者加入后立即读写正常，不得出现约 4 分钟窗口的间歇 9001（若判定预热属预期，窗口内必须返回可解释业务码而非 9001）。本票解除冒烟 TC-080-01（可填写数据范围）与 TC-090-08（移除协作者触发 W-15）的阻塞。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 后端：H-64 接受 role=filler（白名单补齐；外部协作者授 manager 仍被拒；编辑器/查看者路径不回归），契约测试锁角色矩阵
- [ ] 后端：H-63 条目新增稳定 id（协作者记录主键），username/display_name 恒为真实用户名/显示名（冒烟证据：外部条目三字段均落 user_id）；H-65/H-66 以条目 id 寻址成功（现以 user_id/username 调用均 1001）
- [ ] 可靠性：新协作者添加后 1 分钟内完成一次 UpdateCell/CreateRecord 成功（冒烟证据：A8/ed2 首写多次 9001 约 4 分钟后自愈；复现链路含时间线）；若根因属缓存预热，窗口内错误码改为可解释业务码并在票 Comments 记录根因
- [ ] 契约测试覆盖上述三点；`npm run test`/build/契约测试/check-docs 全绿
- [ ] 解除阻塞验证：以 API 预置可填写协作者并按 TC-080-01 前置执行；移除协作者触发 W-15 会话收回（WS 对测）

**冒烟证据**：H-64 role=filler/ROLE_FILLER → 1001（editor 正常）；H-63 条目无 id 字段、外部条目 username=user_id；H-66 按 user_id/username 删除均 1001 且列表不变；9001 时间线——首次 9001 至自愈约 4 分钟，期间 owner 写正常。
