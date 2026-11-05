# 01: spa-hosting（前端 dist 静态托管）

**What to build:** 网关同源托管前端构建产物：构建期嵌入产物（go:embed），非 API 路径回退入口文档（history 路由），API 与 WS 路由保持优先；嵌入目录内置占位文件保证前端未构建时后端仍可编译；部署入口收敛为单一构建脚本（前端构建 → 拷贝产物 → 后端编译），产物不入库。

**Blocked by:** None (can start immediately)

**Status:** resolved

- [ ] 无前端产物时后端可编译，根路径请求不因缺产物而失败
- [ ] 产物存在时：根路径与任意深链返回入口文档；带内容 hash 的资产返回长缓存头；入口文档短缓存校验
- [ ] API 与 WS 路径不受静态回退影响，行为与改动前一致
- [ ] 构建脚本串起三步流程且产物目录不入库
- [ ] 静态托管经文件系统接口注入实现，内存文件系统单测覆盖深链回退/API 防御/缓存头
- [ ] 文档护栏脚本全绿

## Answer
已落地。网关新增 `excel_web_api/internal/static`（`go:embed all:dist` 占位 + `fs.FS` 注入：深链回退入口文档、hash 资产 `public,max-age=31536000,immutable`、入口 `no-cache`；API/WS/健康检查优先于 `/` 回退）；`cmd/main.go` 挂 `static.Mount`；根 `scripts/build-all.ps1` 串「前端构建→拷贝→后端编译」（`-SkipFrontend` 可用）；`web/vite.config.ts` 产物命名带 hash；产物目录不入库（.gitignore）。测试：`internal/static` + 网关全量绿；go build/test、check-docs 全绿。Evidence: merge `dad3adc`。