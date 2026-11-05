# 08: 记录详情抽屉与附件字段

**What to build:** 用户点击行首展开按钮后，右侧出现记录详情抽屉，可在顶部编辑主字段，并按字段顺序查看与编辑当前视图隐藏字段在内的全部字段（公式与引用只读并标注）；可用「上一条 / 下一条」按当前视图顺序切换；附件字段在抽屉内可预览缩略图、上传、下载、重命名、删除。抽屉展开时网格仍可交互。

**Blocked by:** 06（记录写入）

**Status:** resolved

- [x] 点击行首展开按钮打开右侧抽屉（约 480px），不遮挡网格交互；关闭后回到网格原选中位置
- [x] 抽屉顶部主字段可编辑，字段区按字段顺序展示全部字段（含当前视图隐藏字段）
- [x] 公式与引用字段在抽屉内只读并有明确标注
- [x] 「上一条 / 下一条」按当前视图顺序切换；目标不在已加载区间时自动补拉后再切换
- [x] 附件字段可上传（单文件 ≤20MB、单记录 ≤10 个，超限或黑名单拦截有提示）、下载、重命名、删除，并展示缩略图
- [x] 抽屉内编辑沿用同一提交与冲突语义，失败保留输入
- [x] 组件测试经 MSW 覆盖抽屉打开/关闭、全字段渲染、上下条切换（含补拉）、附件增删改与超限提示

## Answer

**实现**
- 新增 `web/src/features/record/RecordDrawer.tsx` 与 `RecordDrawer.module.css`：右侧固定 480px、无遮罩；打开时网格不卸载，关闭后保留 GridArea 选区。
- 抽屉优先 H-49（`recordsKeys.record(recordId)`）拉最新记录，失败回退 `useRecords` 已加载行；与网格共享同一 `recordsKeys.listFor(tableId, viewId, signature)`。
- 字段区展示当前表全部字段（含视图隐藏字段）；顶部主字段与 8 类字段复用票 06 编辑器，公式/引用只读并标注「只读」；提交走 H-51 并携带当前 version，失败保留输入；4001 复用 `RecordConflictDialog` 三动作，4002 保留内容可复制。
- 上一条/下一条按已加载视图顺序切换；末行 `has_more` 时循环 `fetchNextPage` 直到命中或没有更多。
- 新增 `web/src/features/records/file-api.ts`：H-77 multipart（`file` + `usage=attachment`、query `base_id`、`credentials:'include'`、统一信封解析）、H-78/H-79 直链 helper、H-80 PATCH、H-81 DELETE（后两者 `attachClientRequestId:false`）。
- 附件字段：缩略图、下载链接、上传、重命名、删除；客户端预检单文件 ≤20MB、单记录 ≤10 个；5002 黑名单/超限提示；成功后经 H-51 追加/移除 `file_id` 列表并维护版本。
- `workspaceStore` 新增 `fileMetaMap` 与 `setFileMeta/removeFileMeta`；GridArea 附件渲染配置合并该映射，上传后网格即时显示文件名。
- `BaseWorkspacePage` 持有 `drawerRecordId`，接线 `GridArea.onOpenDrawer`；表/视图切换时关闭抽屉；viewer 与 filler 编辑他人记录时抽屉只读。

**文件**
- 新增：`web/src/features/record/RecordDrawer.tsx`、`RecordDrawer.module.css`、`RecordDrawer.test.tsx`、`web/src/features/records/file-api.ts`、`web/src/features/workspace/BaseWorkspacePage.drawer.test.tsx`
- 修改：`web/src/features/workspace/BaseWorkspacePage.tsx`、`components/GridArea.tsx`、`web/src/stores/workspace.ts`、`stores/index.ts`、`components/GridArea.records.test.tsx`、`stores/workspace.test.ts`、`BaseWorkspacePage.views.test.tsx`（仅补充工具栏就绪等待，消除既有用例竞态）

**测试**
- `npm run test`：72 文件 / 930 用例全绿（基线 913 + 新增 17）。
- `npm run build`：tsc + vite 构建成功，首屏 chunk 数不变。

**偏差与 G 项**
- H-80 实现为 `renameFile(fileId, displayName, baseId?)`，抽屉调用时传 `base_id`：网关 `fileBaseId` 对 SessionRequired 的路由需要 query 才能完成 Base 授权，纯两参调用会 3001。
- 附件显示名无独立读取契约：优先会话内 `fileMetaMap`，缺失时回退 `file_id`；未新增 G 项。
- 写成功采用本地缓存落值后 `refetchType:'none'` 标记单记录与记录列表失效，避免抽屉/网格双重立即重拉。
- 按 spec 边界未做评论/动态 tab、分享与导出。
