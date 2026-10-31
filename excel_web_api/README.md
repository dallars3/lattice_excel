# excel_web_api

唯一公网入口（HTTP `/api/v1` + WebSocket `/ws`），**不持有 MySQL 连接**；权限校验统一在网关完成（`share.CheckAccess`）。

- 分层：`rpc/`（core/worker client 封装）、`redis/`（限速/缓存/PubSub）、`deal/`（通用）、`internal/`（gateway/realtime 业务）、`configs/`。
- 端口：8080（见 `configs/web_api_config.yaml`）。