# 01: 公共地基：base/迁移/config/errno/健康检查

**What to build:** 见下验收标准对应的端到端行为（用户视角垂直切片：迁移/实现/HTTP 或 gRPC 接口/测试四条路径全打通）。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 三服务各自构建并启动成功，读取各自的 *_config 配置
- [x] 迁移框架对空库顺序执行且可重复运行（幂等）
- [x] 统一错误信封与 1001/2001/3001/3002/4001/4002/5001/5002/5003/9001 的映射存在契约断言
- [x] HTTP /healthz、/readyz 与 gRPC 健康检查可被探通
