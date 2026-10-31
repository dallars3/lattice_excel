# excel_worker_service

异步与 IO 域（gRPC 仅 localhost + 本地任务循环）。承载 4 模块：file / importexport / notify / scheduler。

- 分层：`base/`、`redis/`、`bus/`、`rpc/`（core client）、`deal/`、`internal/`、`configs/`。
- 附件：本地磁盘池（默认 5GB，R2 实测后确认）。