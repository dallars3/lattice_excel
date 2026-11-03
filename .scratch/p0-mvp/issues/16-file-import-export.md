# 16: file + importexport

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** 12 records-write；14 comment-notify

**Status:** done

- [x] 两段式上传：黑名单/20MB/池计量拦截（5002），Confirm 校验完整性
- [x] 导入预览类型识别与首行字段名提示正确；CSV UTF-8/GBK 均可解析
- [x] 部分行失败续行、错误报告可下载、完成通知与审计齐全；导入中新 Base 不可访问
- [x] 当前视图导出按文件命名与语义生成；外部账号在网关被 3002 拒绝
