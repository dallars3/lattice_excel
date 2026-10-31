# proto 契约（唯一事实源）

与《接口文档 v0.6》《整体技术方案 v1.3》联动；任何接口变更须三处同步。

| 包（excel.{module}.v1） | 服务 | RPC 数 | 对应接口文档 |
|---|---|---|---|
| common | —（值/Principal/LinkContext/ErrorDetail/EventHeader） | — | §8.0 |
| iam | IamService | 31 | §4.1/§8.3.1 |
| share | ShareService | 12 | §4.2/§8.3.2 |
| audit | AuditService | 3 | §4.3/§8.3.3 |
| workspace | WorkspaceService | 8 | §4.4/§8.3.4 |
| structure | StructureService | 20 | §4.5/§8.3.5 |
| records | RecordsService | 10 | §4.6/§8.3.6 |
| calc | CalcService | 3 | §4.7/§8.3.7 |
| comment | CommentService | 6 | §4.8/§8.3.8 |
| file | FileService | 7 | §4.9/§8.3.9 |
| importexport | ImportExportService | 6 | §4.10/§8.3.10 |
| notify | NotifyService | 4 | §4.11/§8.3.11 |
| scheduler | 无对外服务（仅内部任务消息） | 0 | §4.12 |
| webapi | 无 gRPC 服务（WS 帧契约，protojson over WS） | 0 | excel_web_api 技术方案 §7.1 |

合计 RPC：110（webapi/scheduler 无服务，仅契约消息）。

命名基线（来自《整体技术方案 v1.3》§10.1）：包 `excel.{module}.v1`；字段 snake_case；数组 `_list`、kv `_map`；运营者 `admin`、企业管理员 `manager`、操作发起人 `operator`；链接上下文 `LinkContext`。

构建（需安装 buf / protoc-gen-go / protoc-gen-go-grpc）：

```sh
cd proto
buf lint
buf generate   # 生成到 ../gen，go_package 见各文件 option
```

初稿说明：`google.protobuf.Struct` 用于 config_map/payload_map/detail_map/schema_map/row_context_map；cells_map 使用 `excel.common.v1.Value` 承载类型化单元格值。