# 外部链接账号的租户边界：Base 级授权，结构只读不要求企业成员

持分享链接注册的账号没有企业归属（`Principal.enterprise_id` 为空、`internal=false`）。本决策把这类账号在结构域的租户边界定义为「数据表/视图所属 Base 的企业」，即租户标识取表行自身的 `enterprise_id`，Base 级授权继续由网关 `share.CheckAccess` 完成；结构写接口仍要求企业成员或可管理角色。

## 背景

修复前结构域的所有接口统一走 `memberPrincipal`：用 `principal.enterprise_id` 查询企业成员角色，为空即返回 3001。分享链接注册的外部账号因此无法读取自己有权访问的数据表、字段、视图与视图配置（P0 冒烟 T13）。记录域此前已按同一问题完成改造：外部协作者（`enterprise_id` 为空）改用不带企业过滤的 `TableByIDCollaborator`，租户标识取表行自身企业。

## 考虑过的替代方案

- **只对链接会话特判**：需要结构域感知 `link_context` 并解析 token，重复一次网关鉴权，且与记录域口径分裂。
- **继续要求企业成员**：链接访客永远拿不到结构数据，P0 用例无法通过。
- **把 principal 的 `enterprise_id` 改写成 Base 所属企业**：会让 core 把外部账号误判为内部成员，破坏写边界与审计口径，风险高于收益。

## 决策

- 只读接口（`ListTables`/`ListFields`/`ListViews`/`GetViewConfig`）对 `enterprise_id` 为空的账号不再要求企业成员角色；「有企业归属」的账号仍校验成员角色，行为不变。
- 外部账号的数据表查询不带企业过滤（`TablesByBaseCollaborator`/`TableByIDCollaborator`），租户标识取表行自身的 `enterprise_id`；越权访问由网关在 Base 粒度用 `CheckAccess` 拦截。
- 结构写接口（建表/改表/删表/字段与视图变更）继续使用 `memberPrincipal` + 企业过滤的 `TableByID`，不放宽。
- 与记录域既有外部协作者处理保持同一口径，避免两域租户语义再次漂移。

## 后果

- 分享链接访客只要链接有效且网关判定可查看，即可读取该 Base 的结构；链接被重置/关闭后由网关与分享域的 token 校验立即失效。
- 结构域不再能独立证明「调用方属于该企业」，租户隔离依赖网关 `CheckAccess` 在每个 Base 路由上执行；新增结构只读接口必须走 `readPrincipal`，写接口不得复用。
- 外部账号若知道其它 Base 的 `table_id`，在缺少 Base 级网关校验的 session-only 路由上仍可能读到结构；该风险与记录域现状一致，后续若收敛应在网关补齐 Base 上下文，而不是在 core 重新引入企业成员假设。
