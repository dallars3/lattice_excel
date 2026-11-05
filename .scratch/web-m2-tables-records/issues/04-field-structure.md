# 04: 字段结构管理与字段面板

**What to build:** 用户在表头看到按位置排列的字段，主字段固定第一列且不可隐藏、删除、拖动；可管理角色能从末列「＋」新增 11 种字段类型中的任意一种，并为其配置类型专有参数；可重命名字段、查看悬停描述、复制字段（仅配置）、在类型转换前看到影响预览、在删除字段前看到影响面确认。选项类字段可维护选项（增删改、排序、颜色、允许录入时新建开关）。

**Blocked by:** 03（Base 工作台骨架与数据表管理）

**Status:** resolved

- [x] 表头按字段 position 渲染；主字段固定第一列，隐藏/删除/拖动入口对其均不可用，其类型仅限多行文本/数字/单选/公式
- [x] 新增字段可选择 11 种类型，名称 1–30 字符且同表内不可重名；描述 ≤500 字符并在悬停表头时显示
- [x] 字段配置面板可按类型编辑：数字（整数/小数 0–4 位/百分比/货币/千分位）、日期（是否含时间与三种格式）、人员（是否多选）、单选与多选（选项列表 + 允许录入新建，默认关闭）、公式（表达式）、关联（目标表与是否多条）、引用（关联字段 + 目标字段）
- [x] 更改字段类型前先展示受影响记录数与将清空值数量，确认后生效；公式/关联/引用三类字段不提供互转入口
- [x] 删除字段前展示记录数、依赖公式数、依赖引用数并二次确认；被视图条件引用的字段删除后条件自动移除并有轻提示
- [x] 复制字段仅复制配置不复制数据
- [x] 选项维护支持增删改与拖拽排序、调色板自动分配且可改色；删除已使用选项需二次确认；允许录入新建关闭时，录入无匹配选项提示「联系表格管理员添加」且不可新建
- [x] 字段数达到 100 上限时新增被拒并提示容量上限
- [x] 组件测试经 MSW 覆盖新增、重命名、配置、转换预览、删除影响面、复制、选项维护与 5001 上限

## Answer

**实现**

- 字段 API：`features/workspace/api/field.ts` 覆盖 H-34 列表、H-35 新建、H-36 重命名、H-37 更新配置、H-38 转换（confirm=false 预览 / confirm=true 提交）、H-39 删除、H-40 选项维护、H-92 复制；全部写请求 `attachClientRequestId:false`，H-39 body `{confirm:true}`，H-92 无请求体。Field 归一化把 `FIELD_TYPE_*` 枚举名转小写类型，`config_map` 保留普通对象，`description` 保留。
- 字段类型与配置纯逻辑：`features/workspace/types/field-config.ts` 提供 11 类类型清单与标签、各类型默认配置、主字段允许类型（text/number/select/formula）、§8.5.5 转换矩阵（公式/关联/引用不参与任何方向）、选项调色板（blue/green/orange/purple/red/cyan/yellow/pink/gray/brown）与 `resolveOptionEntry`（关闭 allow_create 时返回「无匹配选项，请联系表格管理员添加」且不新建）。
- query key：新增 `query-keys/field.ts` 的 `fieldKeys.list(tableId)` 并导出；扩展 `viewKeys.configScope/config`。字段写成功失效字段列表；转换/删除/复制再失效记录（`recordsKeys.all`）、视图列表与视图配置前缀。
- 工作台接线：`BaseWorkspacePage` 按当前表加载 H-34，字段加载失败/重试与切换表重载由 query key 隔离；`canManageStructure` 控制新增/删除/转换，`canEditFields`（edit_view=allow）控制重命名/配置/复制/选项。
- 字段表头：`components/field/FieldHeaderRow.tsx` 按 position 排序并把主字段固定首列；字段名 `title` 悬停展示描述（H-34 返回值优先、会话内新建描述兜底）；主字段仅重命名/字段配置，可编辑角色隐藏删除/更改类型，可填写/可查看不出现表头菜单；末列「＋」仅可管理可见，字段数 100 时禁用并提示容量上限。
- 字段面板：新增/重命名/配置/转换/删除/选项维护均拆为独立组件。新增对话框 11 类型 + 名称 1–30/同表重名 + 描述 ≤500；公式/关联/引用因后端强制必填参数，在新增时提供表达式/目标表/关联与目标字段的最小初始配置。配置面板按类型渲染数字/日期/人员/公式/关联/引用参数，单选多选复用选项维护；文本/附件/超链接显示「该类型暂无可配置参数」。
- 转换与删除：转换先 confirm=false 预览 `affected_count/cleared_count`，确认后 confirm=true；删除对话框展示「当前已加载 N 条（另有更多未加载）」、依赖公式数、依赖引用数（均由当前字段列表推导），确认后删除并逐视图读取 H-46 原始配置、用 `removeFieldFromViewConfig` 清理隐藏/冻结/筛选/排序/分组/field_width_map，再 H-47 全量写回，最后用 Toast 提示「已移除 N 个视图中的失效条件」。
- 描述兜底：H-35 网关不接受 description，新建时前端把描述写入 `stores/workspace.ts` 的 `fieldDescriptions`（按 fieldId），表头悬停优先取本地值；请求体不发送 description。`app/providers.tsx` 挂载 `ToastProvider`，并为未挂 Provider 的测试场景新增 `useOptionalToast`。

**文件**

- 新增：`web/src/features/workspace/api/{field.ts,field.test.ts,view-config-raw.ts,view-config-raw.test.ts}`、`web/src/features/workspace/types/{field-config.ts,field-config.test.ts}`、`web/src/query-keys/field.ts`、`web/src/features/workspace/components/field/{FieldHeaderRow,FieldCreateDialog,FieldRenameDialog,FieldConfigPanel,FieldConvertDialog,FieldDeleteDialog,OptionsEditor,ConfigFields}.tsx`、`web/src/features/workspace/components/field/{impact.ts,FieldPanels.module.css}`、`web/src/features/workspace/components/{GridArea.tsx,GridArea.test.tsx}`。
- 修改：`web/src/features/workspace/BaseWorkspacePage.tsx` 与其 `BaseWorkspacePage.test.tsx`、`web/src/query-keys/{index.ts,view.ts}`、`web/src/stores/workspace.ts` 与其测试、`web/src/ui-kit/toast/Toast.tsx` 与 `ui-kit/index.ts`、`web/src/app/providers.tsx`、既有工作台测试的 H-34 mock（`app/router.test.tsx`、`features/auth/LoginPage.test.tsx`、`features/home/HomePage.test.tsx`）。

**测试**

- `npm run test`：54 文件 / 657 用例全绿（票 03 基线 604，新增 53）。
- `npm run build`：TypeScript + Vite 构建成功，首屏 5 个 chunk（≤6）。

**偏差与契约缺口**

- G：H-35 网关只解码 name/type/config_map，不接受 description，描述写入通道未接线；前端用会话内 `fieldDescriptions` 兜底，刷新后描述丢失（票面允许的 G 项）。
- G：H-39 网关强制 `confirm=true`，无法取删除前预览；删除影响面用「当前已加载记录数（has_more 标注）+ 当前字段列表推导的依赖公式/引用数」兜底，记录数由 GridArea 属性注入（票 05 接真实加载数）。
- 契约出参形状以 proto 为准：H-38 预览为扁平的 `affected_count/cleared_count`；H-39 为扁平 `record_count/dependent_formulas/dependent_lookups`（接口文档示例的 `impact` 嵌套未采用）。
- 后端新增字段重名返回 1001（非 1002），前端在提交前做同表重名预校验以避免发送。
- 为满足后端必填校验，新增对话框对公式/关联/引用三类提供最小初始配置输入（其余类型仍只要求名称与描述）；配置面板可后续再改。
- 本票未实现记录读取/单元格渲染与写入（票 05/06）、视图 CRUD 与配置面板（票 07）、字段宽度拖拽与隐藏/拖动入口（后续票）。

