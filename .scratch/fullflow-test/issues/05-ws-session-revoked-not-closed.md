# WS session_revoked 后服务端未终止连接

Status: needs-triage
Severity: P0
Source: TC-090-08（WS 协议级）

## 复现步骤

1. owner 将外部协作者 ext 加入 Base（role=editor），ext 建立 WS 连接并 join。
2. owner 调用 `DELETE /api/v1/shares/members/{ext_user_id}`，body `{"base_id":"..."}`。
3. ext 侧观察帧与连接状态。

## 实际

- ext 在 1s 内收到 `{"session_revoked":{"reason":"perm_changed"}}`。
- 等待 3s 后连接仍未关闭（`closed=false`），客户端仍可继续收发。

## 期望

按 W-15 / TC-090-08：收到 `session_revoked` 且连接被服务端终止。

## 证据

- API/WS 执行结果：`addExt=0, remove=0, frame={"reason":"perm_changed"}, closed=false`
- 代码：`excel_web_api/internal/realtime/subscribers.go::handleShareChanged` 仅 `sendToUser` 入队帧，未调用 `client.close()`。

## 影响

P0：被移除协作者的连接保持存活，若后续帧不再校验权限，将形成越权数据通道。需在推送吊销帧后关闭该用户全部连接（或按会话粒度关闭）。
