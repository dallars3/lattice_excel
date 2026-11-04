# 01: HTTP 93 面全量接线与别名治理

**What to build:** 用户视角：接口文档 §2 列出的 93 个对客 HTTP 端点都能被网关真实受理并转发到既有 core/worker 服务，任何端点不再出现「未注册/404」；同时在实现中新增的 5 条未入文档路由要么并入文档、要么修正命名成为文档端点的一部分。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] 接口文档 §2 的 93 个 H 端点全部经统一管线注册并可转发到对应业务服务（未实现的后端 RPC 允许按后端真实状态返回，但网关面不得缺席）——新增 RegisterIamRoutes/RegisterAuditRoutes/RegisterWorkspaceRoutes/RegisterStructureRoutes/RegisterCommentRoutes/RegisterShareRoutes 六个注册面 + rpc 客户端（IamEnterpriseRPC/AuditRPC/WorkspaceRPC/StructureRPC/CommentRPC/ShareManageRPC），main.go 全量接线，TestContractHTTPRouteInventory 运行时清单 93 条
- [x] 当前 5 条「额外已注册路由」完成归属治理：并入文档清单并同步接口文档，或改为文档已存在的路径形态，最终 contract 报告「未注册=0、额外=0、文档匹配=93」——5 条为 file/imports/exports 的 /bases/{base_id}/ 前缀变体，已从注册面移除（文档路径形态保留，base 鉴权经 body base_id + checkAccess），contract summary 未注册=0、额外=0、文档匹配=93
- [x] 每一条补上的路由都有对应契约测试（未登录/错误码映射/至少一次成功转发），失败定位带接口号与断言差异——iam_routes_test/audit_routes_test/workspace_routes_test/structure_routes_test/comment_routes_test/share_routes_test 覆盖全部补接路由的三断言
- [x] `contract-test.ps1` 的 HTTP gap 汇总不再出现 H-xx 未注册行，exit 0——全量 contract-test exit 0（三服务 26/26 包 100%），HTTP SUMMARY 已注册=93 文档匹配=93 额外=0

