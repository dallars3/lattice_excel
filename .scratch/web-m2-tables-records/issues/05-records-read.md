# 05: 记录读取与 11 类单元格渲染

**What to build:** 打开工作台后网格能按当前表与当前视图加载真实记录，并随滚动按游标继续取页；11 种字段类型按各自规则正确显示值。没有记录、筛选无结果、全部字段被隐藏、加载失败时都有明确空态或错误态。底栏展示已加载记录数。

**Blocked by:** 04（字段结构管理与字段面板）

**Status:** resolved

- [x] 记录查询携带表与视图标识，按游标与 20/100 条限制取页；滚动到已加载区间末尾时自动取下一页
- [x] 网格纵向虚拟滚动复用既有窗口计算，滚动过程不出现整表重渲染卡顿
- [x] 多行文本单行省略且悬停可见全文；数字按字段格式渲染（千分位/百分比/货币、0–4 位小数）且空值与 0 显示不同
- [x] 单选与多选渲染带颜色的选项标签；日期按三种格式渲染；人员渲染头像与显示名（多人并列、停用成员灰显）
- [x] 附件渲染文件名与缩略图；超链接渲染显示文本并在新窗口打开（编辑态不跳转）
- [x] 公式渲染计算结果，计算中显示占位，错误值渲染 `#ERROR` 且悬停显示原因；关联渲染目标记录主字段值并可跳转；引用按被引用字段类型渲染，多条并列
- [x] 空表、筛选无结果、全部字段被隐藏、请求失败四类场景各呈约定空态/错误态，写失败不丢内容
- [x] 底栏展示「已加载 n 条」，仍有下一页时显示「n+」；不展示虚构的总数
- [x] 组件测试经 MSW 覆盖游标分页、各类渲染器取值边界与四类空态/错误态

## Answer

### 实现

- **值编解码 / 记录 API**：`features/records/cell-codec.ts` 解码 `common.Value` 包装（int64 字符串→number，null 与 0 严格区分）；`features/records/api.ts` 实现 H-48 游标查询与 H-49 单记录，归一 `{id,tableId,cellsMap,createdBy,createdAt,version}`；`query-keys/records.ts` 增补 `table/listFor/record` 且保留 M1 `list/page`。
- **数据源**：`features/records/useRecords.ts` 用 `useInfiniteQuery`，首页 limit=20、后续 100，`getNextPageParam` 推进；导出 `rows/pages/hasMore/isFetchingNextPage/fetchNextPage/isError/error/refetch`。
- **11 类渲染器**：`core/grid/renderers/` 新增 date/person/attachment/url/formula/relation/lookup/multiselect，扩展 number 格式与 select 后端 `option_list` 适配；`registerM2Cells` + `M2_CELL_DEFINITIONS` 注册 11 类，M1 demo 注册不受影响。日期/数字格式、formula `#ERROR` 哨兵、URL 协议校验、颜色名映射均为纯函数。
- **网格接线**：`GridArea` 使用真实记录 + `computeWindow`/`RowHeightModel('medium')`/`LoadedRangeCache`；单一滚动容器（纵向虚拟 + 横向普通滚动）、sticky 表头、默认冻结主字段、160px 默认列宽；滚动到已加载末尾自动取下一页；800×600 jsdom 兜底。人员显示经 H-63，关联标签经目标表 H-41+H-48(limit=100)，引用按目标字段类型复用渲染器；`onOpenRelation` 回传 Base 工作台并切换 `?table=`。
- **状态与底栏**：空表「从第一行开始」、筛选无结果（预留 `hasActiveFilters/onClearFilters`）、全部隐藏（预留 `allFieldsHidden/onRestoreFields`）、请求失败（文案+重试）、无字段「请先新增字段」；`WorkspaceFooter` 展示「已加载 n 条 / n+ 条」。

### 文件

- 新增：`web/src/features/records/{cell-codec,api,useRecords,metadata}.ts`、渲染器 `attachment/date/formula/lookup/multiselect/person/relation/url/format-date/m2.module.css`、`GridArea.module.css` 及对应测试。
- 修改：`core/grid/{index,registry}.ts`、`renderers/{index,select,format-number}.*`、`features/workspace/{BaseWorkspacePage,components/GridArea,WorkspaceFooter,FieldHeaderRow,FieldPanels.module.css}`、`query-keys/records.ts`、既有相关测试的 H-48 默认 mock。

### 测试 / 构建

- `npm run test`：61 文件 / **765 用例全绿**（基线 657）。
- `npm run build`：`tsc --noEmit` + Vite 构建成功。

### 偏差与 G 项

- G-05-1：契约无附件元数据读取接口，附件文件名保持 `{files}` 注入口；当前网格仅以 file_id 兜底（缩略图走 `/api/v1/files/{id}/thumb`），文件名映射随票 08 抽屉/上传接线补齐。
- G-05-2：H-63 协作者无停用标记，停用态由渲染器 `{users.disabled}` 支持，当前网络映射恒为 false；未知成员显示 user_id 并灰显。
- 分组/筛选/显隐/行高三档与编辑交互按票面留票 06/07，本票只提供 props 与默认 medium 行高。
