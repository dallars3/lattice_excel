# 07: 视图管理与全量视图配置

**What to build:** 用户能管理视图并真正用视图改变数据呈现：新建、重命名、复制、删除视图（每表至少保留一个）；通过面板设置最多 5 条 AND 筛选（操作符按字段类型给出）、最多 3 级排序（空值排最后）、单字段分组并折叠、字段显隐、冻结列、三档行高与字段宽度拖拽。所有配置即时保存为全量替换，工具栏按钮显示已启用条件数徽标；条件变更时游标重置并全量重拉。

**Blocked by:** 05（记录读取与 11 类单元格渲染）

**Status:** resolved

- [x] 视图列表可切换；新建（1–30 字符、同表不可重名）、重命名、复制、删除可用；仅剩一个视图时删除入口禁用
- [x] 筛选最多 5 条且条件间为 AND，第 6 条被拒并提示上限；操作符按字段类型白名单给出（文本/数字/单选/多选/日期/人员/附件/关联/公式）
- [x] 排序最多 3 级、每级可选升降序，空值统一排最后；第 4 级被拒并提示
- [x] 分组仅支持单字段（单选/多选/人员/日期按日/周/月），组可折叠，组头仅显示组名与折叠控件、不显示全量记录数（按 N1 裁定）
- [x] 可隐藏任意非主字段、设置冻结到任意列、切换紧凑/中等/宽松行高；字段宽度可拖拽且限制 40–2000px
- [x] 所有配置改动即时提交全量视图配置并成功回读；工具栏按钮显示已启用筛选/排序/分组条件数
- [x] 筛选、排序、分组变更后清空已加载区间并从首个游标重拉，不拼接旧条件的数据
- [x] 全部字段被隐藏时提示「所有字段均已隐藏」并提供恢复显隐入口
- [x] 筛选无结果时展示空态与清除筛选按钮
- [x] 组件测试经 MSW 覆盖视图 CRUD、配置上限、即时保存载荷、条件变更重置游标、组头无计数与宽度持久化

## Answer

实现：新增 `web/src/features/workspace/types/{view-config,view-config-codec,view-config-validation}.ts`（内部模型、protojson 编解码、common.Value 包装、40–2000 宽度 clamp、筛选/排序/分组上限与按字段类型操作符白名单、数据条件签名）；`api/view.ts` 补齐 H-42~H-47（写请求均 `attachClientRequestId:false`，H-45/44 无 body，H-47 body `{view_config}`）；`hooks/useViewConfigAutoSave.ts` 本地即时更新 + 串行提交 + 连续改动合并 + 成功回读/失败保留与重试（经 trackWrite）；`components/view/ViewPanels.tsx` 筛选/排序/分组/显隐面板（含冻结到第 N 列与三档行高）；`ViewBar.tsx` 视图 CRUD/校验/菜单与条件数徽标；`BaseWorkspacePage.tsx` 接 H-46 加载与 CRUD 重拉/自动切换；`GridArea.tsx` 按配置过滤字段、冻结/列宽/行高、组头内联渲染（N1 无计数）与折叠、空态接线；`FieldHeaderRow.tsx` 列宽拖拽手柄；`view-grouping.ts` 分组标签纯逻辑；`query-keys/records.ts` + `useRecords.ts` 将条件签名纳入查询 key，条件变更从空游标重拉。

测试：新增 `view-config-codec.test.ts`(11)、`view.test.ts` 扩展(7)、`view-grouping.test.ts`(6)、`BaseWorkspacePage.views.test.tsx`(20)；并为既有 Base 工作台/路由/登录/首页用例补 H-46 配置 mock。`npm run test` 913 用例全绿（70 文件），`npm run build` 成功（5 chunks）。

偏差 / G：AC-VIEW-04 组头计数按 N1 裁定不显示；分组降级为分组字段第一排序键。冻结列表为空即不冻结，未加载配置时沿用票 05 默认冻结首列；字段宽度默认 160（N4）。H-42 按接口文档携带 `type:"table"`。筛选为全量即时保存，未填完的条件会先落库、后续编辑覆盖；列表操作符空值时省略 value。详情抽屉/附件与权限异常态属票 08/09，未覆盖。无新增 G 项。
