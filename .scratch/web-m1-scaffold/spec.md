# M1 前端工程搭建（web/）

Status: ready-for-agent

## Problem Statement

协作多维表格此前为纯后端仓库（三服务 + 93 条 HTTP 契约 + WS 实时通道），前端需求与技术方案已定稿（《前端需求文档-v0.1》《前端-技术方案-v0.1》），但仓库内没有任何前端工程。需要在仓库根 `web/` 目录内搭建符合已评审技术方案的前端工程底座与网格骨架，作为后续 M2~M5 业务里程碑的唯一承载工程，并确保测试接缝在本里程碑一次性定型（避免后期返工）。

## Solution

在 `web/` 内新建独立前端工程：React 18 + TypeScript + Vite，路由 React Router，客户端状态 Zustand，服务端状态 TanStack Query，样式 CSS Modules + CSS 变量（design token 亮色版、暗色仅留扩展位）。同步落地传输层骨架（httpClient 信封/错误码目录/幂等/游标、realtimeClient 状态机、离线队列）与网格骨架（虚拟滚动窗口、SelectionModel、键盘状态机、CellRenderer 注册表），并按已定接缝配置测试栈：Vitest + RTL + MSW（HTTP 边界为唯一组件测试接缝，WS 经注入式 Fake），Playwright 仅安装配置不跑用例。工程满足试运行约束：构建期开源依赖、运行时零外部服务、产物自托管（无 CDN）。

## User Stories

1. As a 前端开发者, I want `web/` 内有一键可用的标准化工程（安装、dev server、build、test），so that 我能立即开始 M2 开发而不必重新搭环境。
2. As a 前端开发者, I want 工程配置（tsconfig/别名/环境变量）对齐技术方案的目录与技术栈，so that 后续按 feature 目录落代码不产生路径分歧。
3. As a 前端开发者, I want 环境变量 `VITE_API_BASE` / `VITE_WS_URL` / `VITE_NOTICE_TEXT` 开箱可用且默认同源，so that 本地与单机部署零配置可达后端。
4. As a 前端开发者, I want 统一的 design token（颜色/间距/字号/三档行高/圆角/z-index）以 CSS 变量落地，so that 组件不再各自写死样式值，暗色可低成本扩展。
5. As a 前端开发者, I want ui-kit 基础组件最小集（Button/Banner/Toast/EmptyState 等）先行就位，so that 页面与网格骨架演示有统一视觉基底。
6. As a 首页访客, I want 试运行免责声明组件（HealthNotice）已实现并挂入占位页面，so that 合规文案不必每个里程碑重复实现。
7. As a 前端开发者, I want httpClient 统一解析 `{code,message,data}` 信封并抛出类型化 ApiError，so that 业务层不必重复处理协议细节。
8. As a 前端开发者, I want error-catalog 将 13 个错误码落成「文案 + 动作 + 恢复策略」元组，so that 每个组件错误态都从单一事实源取用。
9. As a 前端开发者, I want 写请求自动携带幂等 client_request_id 且重试复用同一 id，so that 断线重传不产生重复提交。
10. As a 前端开发者, I want 游标分页 helper 适配 useInfiniteQuery（cursor/limit/has_more/next_cursor），so that 网格窗口加载无需每处重写。
11. As a 前端开发者, I want realtimeClient 具备 joining/reconnecting/revoked 状态机、心跳与类型化事件分发，so that WS 生命周期逻辑与 UI 解耦。
12. As a 前端开发者, I want realtimeClient 的传输层可注入（Fake transport），so that 协作逻辑可在不连真服务端时单测。
13. As a 前端开发者, I want 离线队列 store 具备「入队—断线只读—恢复重放—冲突转裁决」的状态机，so that ADR 0002 语义在底座阶段定型。
14. As a 前端开发者, I want 网格虚拟滚动骨架支持窗口计算、可变行高实测缓存与冻结列双通道，so that 5 万行性能验证有可跑的基座。
15. As a 前端开发者, I want SelectionModel 与键盘键位状态机（导航/选中/编辑/提交/取消）落地，so that 键盘可达在网格层先行成立。
16. As a 前端开发者, I want CellRenderer/CellEditor 注册表框架与 2~3 个演示实现就位，so that M2 起按字段类型注册即可扩展。
17. As a 前端开发者, I want Query key 工厂统一管理所有服务端数据 key，so that 缓存失效口径全局一致。
18. As a 前端开发者, I want Vitest + RTL + MSW 按已定接缝配置完成并有代表性用例（错误码目录、网格窗口计算、队列状态机），so that M1 交付本身可自动验证。
19. As a 前端开发者, I want Playwright 已安装并出基础配置（不跑用例），so that M5 收口时直接写测试而非先搭框架。
20. As a 前端开发者, I want `vite build` 产物成功且首屏 chunk 数 ≤6（HTTP/1.1 约束），so that 单机部署与首屏性能预算有构造性保证。

## Implementation Decisions

- **工程位置与形态**：仓库根 `web/` 独立前端工程（用户指定），不侵入三服务 Go 模块构建；`.gitignore` 覆盖 `node_modules/` 与 `dist/`。
- **技术栈锁定**（对齐技术方案第 2 章）：React 18 + TypeScript 5 + Vite 5 + React Router 6；Zustand（瞬态态）、TanStack Query（服务端态）；CSS Modules + CSS 变量；Vitest + RTL + MSW（Playwright 仅配置）。除已锁定清单外**不引入**新框架（如状态机库、UI 组件库、样式框架）。
- **分层与模块划分**（对齐技术方案第 3/4 章）：UI 层（Pages / Feature components / ui-kit）→ 交互状态层（Zustand store：selection / editSession / presence / offlineQueue / ui）→ 领域数据层（query-keys + services）→ 传输层（httpClient / realtimeClient / offlineQueue）。核心可复用模块（http/realtime/grid/offline）按 core 划分，业务按 feature 划分。
- **httpClient 契约**：统一信封解析、code===0 透传；13 个错误码（1001/1002/2001/2002/3001/3002/4001/4002/5001/5002/5003/9001/9002）映射为 error-catalog 元组（文案/动作/恢复）；写操作强制 client_request_id；游标分页 helper 面向 useInfiniteQuery；会话票据走同源 Cookie（Bearer 备用）。
- **realtimeClient 状态机**（接口文档 W-01~W-16 为消息事实源）：`idle → connecting → joined(snapshot) → reconnecting（指数退避，replay_request 带 last_seq）→ revoked`；心跳 15s；事件统一转类型化消息分发；**传输层接口注入**（生产为 WS 实现，测试为 Fake）。
- **离线队列语义**（ADR 0002）：断线只读、在途项保留、重连先补增量再顺序重放、冲突转 4001 三动作裁决、失败内容不丢失。（M1 实现 store 状态机与假实现，真网络联动在 M3。）
- **网格骨架**（ADR 0001）：行虚拟化（三档行高估算 + 实测缓存、上下 overscan）；冻结列与非冻结列双通道渲染；列宽拖拽预留表级配置通道；帧预算 16ms 常量；loadedRanges 以 LRU 上限管理。
- **键盘模型**：键位表（方向/Enter/Tab/Shift+Tab/F2/双击/Esc/Delete/Ctrl+Z/C/V/X/F）落为纯函数状态机，UI 与模型分离。
- **主题策略**：亮色为默认；全部样式值走 CSS 变量；仅预留 `[data-theme="dark"]` 扩展位，**不实现**暗色与 i18n 框架。
- **构建约束**：manualChunks 控制首屏并发 chunk ≤6（对齐结论 #4：网关无 HTTP/2）；无任何 CDN/外部字体/第三方 SDK 引用。
- **E2E 后置**（本轮已确认）：Playwright 安装 + 配置就绪，不写用例、不纳入 M1 验收。

## Testing Decisions

- **好测试标准**：只测外部行为，不测实现细节——网格测「给定数据窗口与滚动偏移，输出可见行集合」；队列测「给定状态与事件，期望迁移目标」；组件经 MSW 测「给定错误码响应，界面上出现约定文案与动作」。
- **接缝**（本轮已确认）：MSW HTTP 边界为组件测试唯一边界；WS 经 Fake transport 注入；纯逻辑（网格窗口、队列状态机、键位状态机、错误码目录）直接单测。
- **测试栈范围**：Vitest（单测，表驱动风格，沿用后端 Go 表驱动测试惯例）+ RTL（组件）+ MSW（握手 93 接口的一个子集：M1 用 3~5 条代表性接口与 13 个错误码即可）；Playwright 仅配置。
- **覆盖底线（M1）**：error-catalog 全 13 码映射、网格窗口计算边界（顶部/中部/底部/分组行混合）、离线队列全状态迁移、键位状态机主路径、HealthNotice 三处文案。
- **完成定义**：`npm run test` 全绿；`npm run build` 成功且 chunk 数 ≤6；`npm run dev` 可启动并渲染占位页。

## Out of Scope

- M2~M5 业务功能（数据表/字段/记录/视图/协作联动/评论/分享/导入导出/移动端）与其真网络联动（真 WS 收发、真实后端联调）。
- 11 种字段类型渲染器/编辑器的完整实现（M1 仅注册表框架 + 2~3 个演示）。
- Playwright 用例编写与 E2E 验收（M5 收口）。
- 视觉像素规范、暗色主题、i18n 运行时框架、无障碍（P0 不承诺）。
- 埋点上报端点与真实上报（后端项，对齐结论 #2；M1 不实现 tracker）。
- 前端 dist 的网关静态托管嵌入（后端项，对齐结论 #1；前端仅保证产物可用）。
- P1 全部能力（看板/画册/日历/甘特/表单/仪表盘/通知 WS 推送等）。
- 新增任何文档（README/设计稿）——M1 不产出新文档，全部依据既有三份事实源。

## Further Notes

- 唯一事实源：《前端需求文档-v0.1.md》《前端-技术方案-v0.1.md》《前后端对齐结论-v0.1.md》；接口契约以《接口文档-v0.6.md》为准。M1 不新造术语，沿用 [GLOSSARY.md](../GLOSSARY.md)。
- 依赖治理：允许构建期 npm 依赖；禁止运行时外部服务（无 CDN、无统计/错误上报 SDK）；同源部署预期（对齐结论 #1/#4）。
- 网格骨架验收时以需求文档附录 D 的 AC-VIEW-07、AC-NFR-01、AC-NFR-03 为性能与键盘目标的行文依据，但通过性验证后置（M2 起有真实数据与页面）。
- 落地完成后应运行 `docs\tools\check-docs.ps1` 保持仓库围栏全绿（.gitignore 等文件不产生 md 副作用即可）。