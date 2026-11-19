# 04: N2 人员目录并集端点 + 权限裁剪

**What to build:** 人员字段候选目录的聚合口径升级为「Base 外部协作者 ∪ 企业成员 ∪ 所有者」：内部用户调用返回企业全员+协作者并集；外部协作者调用仅返回该 Base 协作者集合（不泄露企业全员名单）。响应形状与现有协作者列表对齐（user_id/username/display_name，显示名缺省回落用户名），前端目录函数只改数据源不改映射逻辑——owner-only 企业的人员字段立即可选所有者与企业成员。

**Blocked by:** 01（测试库隔离护栏）。

**Status:** done

- [x] owner-only 企业：人员目录含所有者本人（user_id/username/display_name 形状正确）
- [x] 有企业成员的企业：内部用户目录含全部企业成员（去重：同一人既是所有者又是成员不重复）
- [x] Base 另有外部协作者时：内部用户目录为协作者∪企业成员并集
- [x] 外部协作者调用：目录仅含该 Base 协作者集合，不含企业全员名单
- [x] 网格与抽屉的人员编辑器使用新目录后可选可提交（组件级/MSW 用例），前端映射逻辑零改动
- [x] 成员被移除后：目录不再包含该成员；存量值渲染沿用既有兜底语义（联动票 04 已建立的口径）
- [x] Go 集成用例覆盖四种视角（owner-only/有成员/有协作者/外部协作者）；前端 MSW 用例覆盖目录切换

## Comments

**实现记录（2026-11-19，编排者关票）**
- 分支 `fix/ad-04-person-directory-union`，实现提交 `330f192`，并入集成分支 `fix/web-m2-grid-fixes`（合并提交 `af9a222`）
- 全量验证：core go test 15 包全绿（测试库隔离生效）+ excel_web_api go test 全绿 + 前端 npm test **1124 用例**全绿
- 端点组织：share 域新增 RPC `ListPersonDirectory`（share 已持 Base→企业解析/协作者/身份解析/调用者裁决全部缝，最小新增），路由 `GET /api/v1/bases/{base_id}/shares/person-directory`（BaseAuth ACTION_VIEW）；响应复用 Collaborator 条目消息（形状与协作者列表对齐，前端映射零改动）；不动既有 GET /shares/members（协作管理语义+MANAGE_SHARE 原样，兼容红线）；proto RPC 计数 112→113 随契约护栏同步更新
- 聚合与裁剪：internal=principal 企业==Base 企业 → 企业全员（active 成员×active 账号+OwnerID）∪协作者；否则仅协作者集合；身份目录不向外部视角泄露授权元数据
- 事项备忘：首次 generate-proto 因 shell cwd 重置在主工作区 gen/ 重写了 gen/go.mod（生成物未动、无 git 污染，已核实），随后以 -Root 正确生成
