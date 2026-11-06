# 全流程 P0 冒烟执行报告

Status: ready-for-human

| | |
|---|---|
| 执行日期 | 2026-11-06 |
| 被测环境 | 单机部署 `http://127.0.0.1:8080`（三服务 + MySQL + Redis 均在线，`/readyz` = ok） |
| 用例来源 | `.scratch/fullflow-test/test-cases.md`（全部 P0，共 49 条） |
| 执行方式 | API/WS：`.scratch/fullflow-test/p0-api.mjs`；UI：`web/e2e/p0.spec.ts`（Playwright Chromium） |
| 结论 | **39 PASS / 10 FAIL**，失败项已按 tracker 规范落 `.scratch/fullflow-test/issues/01~09` |

## 1. 结果总览

| 模块 | 用例数 | PASS | FAIL | 说明 |
|---|---:|---:|---:|---|
| T00 部署与同源可达 | 4 | 4 | 0 | 入口、深链回退、健康检查、API 路由优先 |
| T01 认证与会话 | 7 | 6 | 1 | 登录成功缺通知铃铛/用户信息 |
| T02 首页与 Base 管理 | 4 | 3 | 1 | 删除 Base 恒 9001 |
| T03 数据表管理 | 3 | 3 | 0 | 新表默认结构由前端兜底（后端缺口另记 #09） |
| T04 字段 | 6 | 6 | 0 | 主字段保护、文本/数字/单选、公式环检测均通过 |
| T05 记录与网格 | 6 | 6 | 0 | 三入口新增、三通道编辑、三键提交、删除撤销均通过 |
| T06 视图 | 2 | 2 | 0 | CRUD 与冻结列通过 |
| T07 抽屉 | 1 | 1 | 0 | 长文本全文可见 |
| T08 权限矩阵 | 4 | 3 | 1 | 数据范围/只读/入口显隐通过；filler 越权码 4002 |
| T09 WS | 3 | 1 | 2 | join/snapshot 通过；seq 缺失、吊销不断连 |
| T10 冲突与幂等 | 2 | 2 | 0 | 4001 冲突、幂等重发通过 |
| T13 分享链接 | 2 | 0 | 2 | 链接会话 3001；重置后旧链仍命中缓存 |
| T14 导入导出 | 3 | 0 | 3 | worker 调 core 缺 `x-row-scope` |
| T16 错误码 | 2 | 2 | 0 | 2001/2002（含 5 次锁定）通过 |
| **合计** | **49** | **39** | **10** | P0 阻塞发布 |

## 2. 失败项清单

| 用例 | 现象 | 缺陷 |
|---|---|---|
| TC-010-04 | 首页顶栏无通知铃铛、无用户信息 | [#01](issues/01-home-missing-notification-bell.md) |
| TC-020-03 | 删除 Base 返回 9001，Base 未删除 | [#02](issues/02-delete-base-fails-9001.md) |
| TC-080-04 | filler 编辑他人记录返回 4002（期望 3001），数据未变 | [#03](issues/03-filler-write-other-4002.md) |
| TC-090-02 | `cell_changed` 广播无 seq（恒 0 被省略） | [#04](issues/04-ws-cell-changed-missing-seq.md) |
| TC-090-08 | 收到 `session_revoked` 后连接未终止 | [#05](issues/05-ws-session-revoked-not-closed.md) |
| TC-130-01 | 链接注册会话访问 Base 3001；UI 注册后落 `/home` | [#06](issues/06-share-link-access-denied.md) |
| TC-130-02 | 重置链接后旧 token 仍 302 到 Base（缓存未清） | [#07](issues/07-share-link-reset-cache.md) |
| TC-140-01 | xlsx 导入 3 行全部失败（缺 x-row-scope） | [#08](issues/08-worker-missing-row-scope-import-export.md) |
| TC-140-02 | csv 导入 3 行全部失败（同因） | [#08](issues/08-worker-missing-row-scope-import-export.md) |
| TC-140-09 | 导出恒 1001（同因） | [#08](issues/08-worker-missing-row-scope-import-export.md) |

## 3. 逐条结果

### T00 部署与同源可达

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-000-01 | PASS | `/` 200，登录页与明文 HTTP 免责声明可见，`#root` 非空 |
| TC-000-02 | PASS | `/home`、`/base/{id}`、任意深链均 200 + `#root`；深链未登录落登录页 |
| TC-000-03 | PASS | `/healthz` 200；`/readyz` 200（body=ok，聚合依赖状态） |
| TC-000-04 | PASS | 未登录 `GET /api/v1/auth/me` → 401 + code 2001（JSON 信封，非 index.html） |

### T01 认证与会话

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-010-01 | PASS | 注册页提示「注册即为你新建一个企业」；注册后落 `/home`，me 含 enterprise |
| TC-010-02 | PASS | 用户名过短/非法字符、纯数字密码均 1001 |
| TC-010-03 | PASS | 重复用户名 1002「用户名已存在/对象重名」 |
| TC-010-04 | FAIL | 登录成功但无通知铃铛/用户信息（#01） |
| TC-010-05 | PASS | 错误密码提示「用户名或密码错误」；无忘记密码入口；展示「如遗忘密码，账号将无法使用」 |
| TC-010-07 | PASS | 会话失效后写操作弹「登录状态已失效」，重登后留在 `/base/{id}` |
| TC-010-08 | PASS | 退出登录回 `/login`，旧会话 `me` → 2001 |

### T02 首页与 Base 管理

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-020-01 | PASS | 星标/我创建的/与我共享分区内容正确，星标置顶 |
| TC-020-02 | PASS | 新建 Base 落工作台，默认 1 表 1 视图 + 主字段 |
| TC-020-03 | FAIL | 强提示存在；确认删除后返回 9001，卡片与数据库记录均保留（#02） |
| TC-020-04 | PASS | 外部账号仅见「与我共享」，无新建/最近/星标分区 |

### T03 数据表管理

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-030-01 | PASS（附注） | UI 建表后可见标签、默认主字段与视图；由 `bootstrapTableDefaults` 前端补建。后端 H-30 直接返回空表（#09，P1） |
| TC-030-02 | PASS | 1 表时「删除表」禁用并提示「至少保留一张表」；API 直删返回 4003 |
| TC-030-03 | PASS | 删除非最后一张表成功；记录查询返回 4002 |

### T04 字段

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-040-01 | PASS | UI 新增「金额」列即时出现；API 字段 3→7 |
| TC-040-02 | PASS | 主字段菜单仅重命名/配置；无删除/改类型/复制；改名后仍第一列且保持主字段；API 删除被拒（1001） |
| TC-040-04 | PASS | 10001 字符提交被拦「内容超出上限 10000 字符」，编辑器保留 |
| TC-040-05 | PASS | 非法字符不可录入；1234.567 → 1,234.57（2 位小数 + 千分位） |
| TC-040-06 | PASS | 选项带色渲染；关闭 allow_create 后无匹配提示「无匹配选项，请联系表格管理员添加」 |
| TC-040-12 | PASS | 自引用公式 `{循环公式}` 创建被拦，对话框保留并显示错误 |

### T05 记录与网格

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-050-01 | PASS | 底栏「+ 新增记录」/首行幽灵行/末行 ArrowDown 三入口均可新增，焦点落主字段编辑器 |
| TC-050-02 | PASS | 单击仅选中；双击/F2/Enter/直接打字均进入编辑 |
| TC-050-03 | PASS | Enter 提交、Tab 提交、Esc 取消不落盘 |
| TC-050-04 | PASS | 文本/数字/日期/单选/URL 值经 H-49 读回一致（含毫秒时间戳与 URL 对象编码） |
| TC-050-09 | PASS | 删除提示「已删除 1 条 [撤销]」；7s 内撤销恢复；超 8s 撤销入口消失 |
| TC-050-12 | PASS | 非法 URL 提交显示行内错误，原输入 `ftp://invalid` 保留 |

### T06 视图

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-060-01 | PASS | 新建/重命名/复制（名称「视图B 的副本」）/删除成功，列表即时更新 |
| TC-060-07 | PASS | 冻结到第 3 列后前三列表头 `position: sticky`；横向滚动 400px 首列位置不变 |

### T07 抽屉

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-070-01 | PASS | 抽屉内长文本（320 字）全文可见，未被单元格省略截断 |

### T08 权限矩阵

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-080-01 | PASS | filler 仅见自己创建的 1 条记录，页眉「仅显示你创建的记录」，新增入口可见 |
| TC-080-02 | PASS | viewer 无新增记录/新增字段/新建视图/新建数据表入口，可读网格 |
| TC-080-03 | PASS（部分） | manager/editor/filler/viewer 的分享、更多、结构入口抽样一致；导入/导出/分享弹窗属 M4，未实现（已标注） |
| TC-080-04 | FAIL | viewer/删表均 3001；filler 编辑他人记录返回 4002（#03） |

### T09 WS

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-090-01 | PASS（附注） | 双连接均收到 snapshot，后加入者成员列表 2 人，先加入者收到 presence；`seq` 字段因 0 值被 protojson 省略（见 #04） |
| TC-090-02 | FAIL | 帧 ≤1s 到达、值/version 正确，但无 `seq`（#04） |
| TC-090-08 | FAIL | 收到 `perm_changed` 吊销帧，但 3s 内连接未关闭（#05） |

### T10 冲突与幂等

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-100-01 | PASS | 同 version 后提交者 4001，返回 `their_value=111, their_version=4` |
| TC-100-02 | PASS | 同 client_request_id 重发两次，库中仅 1 条记录 |

### T13 分享链接

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-130-01 | FAIL | 匿名 `/s/{token}` → 302 `/register?link_token=…` 正确；注册成功但 `GET /bases/{id}/tables` 3001；UI 注册后落 `/home`（#06） |
| TC-130-02 | FAIL | reset 返回新 URL；旧 token 仍 302 `/bases/{id}`，未出现失效页（#07） |

### T14 导入导出

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-140-01 | FAIL | 上传/预览（2 列）/建任务均成功；3 行全部失败，H-84 原因为 `x-row-scope metadata is required`（#08） |
| TC-140-02 | FAIL | 同上（CSV） |
| TC-140-09 | FAIL | 视图配置写入成功；`POST /exports` 返回 1001，worker 任务记录同因错误（#08） |

### T16 错误码

| 用例 | 结果 | 证据摘要 |
|---|---|---|
| TC-160-01 | PASS | 无会话写接口 → 401 + 2001 |
| TC-160-02 | PASS | 前 5 次 2002「用户名或密码错误」，第 6 次「失败次数过多，请稍后再试」 |

## 4. 复现与产物

```powershell
# API / WS（Node 22；首次执行前在 .scratch/fullflow-test 安装 ws）
cd .scratch\fullflow-test
npm install ws --no-audit --no-fund
& E:\node\node.exe p0-api.mjs

# UI（Playwright + Chromium）
cd ..\..\web
npx playwright test e2e/p0.spec.ts --workers=1 --reporter=list --timeout=180000
```

| 产物 | 路径 |
|---|---|
| API/WS 结果 JSON | `.scratch/fullflow-test/p0-api-results.json` |
| API/WS 执行器 | `.scratch/fullflow-test/p0-api.mjs` |
| 导入夹具 | `.scratch/fullflow-test/fixtures/import.csv`、`import.xlsx` |
| UI 用例 | `web/e2e/p0.spec.ts` |
| UI 失败上下文 | `web/test-results/`（已归档到 `.scratch/fullflow-test/evidence/ui/`） |

## 5. 备注与口径

- 本轮 UI 用例只覆盖 M2 已有界面；导入/导出向导、分享弹窗、冲突三动作弹窗、离线横幅、通知中心等 M3~M5 界面未实现，涉及用例按「可测半边」执行并在结果中注明。
- TC-080-03 的「导入/导出/分享弹窗」子项因 UI 未实现未验证，其余入口显隐与 5.1 矩阵一致。
- TC-030-01 判 PASS 依据 UI 用例；H-30 后端默认结构缺口作为 P1 契约问题单独跟踪（#09），不改变本用例结论。
- TC-090-01 判 PASS 依据 join/snapshot/presence 主链路；`seq` 缺省问题归入 TC-090-02 缺陷 #04。
- 注册/登录有限速（10/min、20/min），复跑时如遇 5003 需等待滑窗；UI 用例已内置重试。

## 6. 修复后复跑（2026-11-06，集成分支 fix/fullflow-test-fixes）

| 套件 | 结果 |
|---|---|
| API/WS（p0-api.mjs） | 29/29 PASS |
| UI（web/e2e/p0.spec.ts，Chromium） | 31/31 PASS |
| contract-test.ps1 | exit 0（core 14/14、worker 8/8、web 6/6，go vet 全绿） |
| check-docs.ps1 | exit 0（fail=0） |

本报告第 1~5 节为修复前基线（39 PASS / 10 FAIL），缺陷已按 issues/01~07 修复并复跑全绿。
