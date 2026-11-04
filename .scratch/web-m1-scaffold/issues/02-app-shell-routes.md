# 02: app 装配与路由骨架（providers + HealthNotice）

**What to build:** 应用骨架装配：登录/首页/Base 工作台三条占位路由可导航、刷新不白屏；QueryClient 与 Zustand stores 挂载于根；试运行免责声明组件（HealthNotice）在登录、注册/首页、Base 三处强制渲染。此后各里程碑功能落地于该骨架之上。

**Blocked by:** 01: 工程底座

**Status:** ready-for-agent

- [ ] `/login`、`/home`、`/base/{id}` 三条路由可达、可互相导航，history 模式整站回退可用
- [ ] 任意路由直接刷新不白屏（占位内容保持在位）
- [ ] providers 装配完成：QueryClient 挂根；声明的 Zustand stores 可被任意组件消费到初始状态
- [ ] HealthNotice 组件在登录页、首页、Base 工作台三处渲染试运行免责声明（文案来自环境变量）
- [ ] 装配层可测试（一条路由渲染冒烟用例绿）