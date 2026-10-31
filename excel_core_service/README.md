# excel_core_service

业务主域（gRPC 仅 localhost）。承载 8 模块：iam / share / audit / workspace / structure / records / calc / comment。

- 分层：`base/`（DB）、`redis/`、`bus/`（EventBus）、`deal/`、`internal/`（模块业务）、`configs/`。
- 权限策略：启动加载独立仓库 `excel_permission_config` 的 JSON 并热更。