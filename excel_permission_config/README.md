# excel_permission_config

角色→权限静态策略的独立配置仓库（对应《整体技术方案 v1.3》鉴权设计）。

- `permissions.json`：角色与可执行动作的静态映射；由 `excel_core_service` 启动时加载、运行期热更新。
- 用户↔Base 的动态授权关系（协作者、链接角色）不在此维护，仍由 MySQL 持有（PRD FR-09）。