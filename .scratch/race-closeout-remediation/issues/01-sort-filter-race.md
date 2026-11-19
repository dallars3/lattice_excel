# 01: 排序/筛选竞态修复（核心）

**What to build:** 视图配置面板（排序/筛选/分组）的任何改动保存成功后，记录网格立即按新配置展示——用户改排序方向、切换排序字段、增删筛选条件后**无需反复操作或刷新页面**，表格即时呈现正确结果。压测用例 sf-stress.spec.ts（六条 TC，覆盖 14 行数据、数字/文本、连续 4 次正降切换、跨字段切换、双字段组合、筛选两轮 12 次切换、筛选+排序组合）随修复提交并全部转绿，作为回归证明。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 视图配置保存成功回调（onSaved）按保存结果的数据条件签名失效记录列表查询并触发重取，修复「重取与保存竞态导致首次改动不生效、结果慢一拍」
- [x] 保存请求在途期间不得把旧配置的响应缓存到新签名 key 下（React Query 缓存语义按此校准，必要时取消在途请求）
- [x] 修复覆盖排序、筛选、分组三条路径（同一签名机制）
- [x] 压测用例 sf-stress.spec.ts 提交入库，六条 TC 对部署栈全部转绿（含 14 行数据、数字/文本、空值排最后、并列值次序、连续切换）
- [x] 既有 m2-fixes 17 条与 p0 相关用例保持绿（修复不引入回归）
- [x] TDD：先用压测用例红灯复现（已有两次稳定失败记录），修复后转绿

## Comments

### 2026-11-20 实现（fix/rc-01-sort-filter-race，4303028..85efa17 + 2506a68；评审修复 094dadf）

**修复（2 文件 +31/−5）：** BaseWorkspacePage.tsx 新增 `refetchRecordsAfterViewConfigSave()`——onSaved 中先 `cancelQueries` 取消本表在途记录请求，再 `await invalidateQueries({ queryKey: recordsKeys.table(tableId) })` 按表前缀失效重取（覆盖网格/RecordDrawer/其他视图的全部签名 key）；useViewConfigAutoSave.ts 将 tableId 经 options 传入并在 onSaved 回调第三参透传。保存 API、签名编解码、失败重试 UX 未动；排序/筛选/分组共用同一签名机制故全覆盖。评审修复（094dadf）：onSaved 改为 async 并由保存串行循环 await——spec「失效并等待重取完成」字面达标，同时串行化连击下的 cancel+invalidate 突发（收口期偶发网格停滞疑点）；重取失败仅 console.warn 不反转为保存失败。

**TDD 证据：** 单元级红灯 9ff008e——BaseWorkspacePage.views.test.tsx 门控 PUT 复现「重取先于落库且保存成功后无重取」（2 条用例 failed: expected 2 to be 3 / ≥3）；修复后转绿，vitest 全量 89 文件 1127 用例两轮全绿（合并票 03 前后各一轮）。E2E 红灯基线沿用 web-m2-int 两次全量稳定失败记录（票面允许）。

**sf-stress 入库与用例体修正（2506a68，断言未放宽，均为用例体缺陷按代码验证口径修正）：** 空值统一排最后（compareNullableCells 双向一致，原稿 ASC 空值在前系误写）；TXT1 期望数组补 蓝莓 行；「文本1 等于」操作符不在文本白名单 → 改 数字2 等于 5（同 2 行期望）；「不包含 苹果」11→10（空单元格不命中值操作符，matchesFilter SQL-NULL 语义）；面板打开幂等化 + TC-05/06 面板串行修正 + 读值加 quiescent/toHaveCount 轮询。新增 SF_BASE_URL 环境覆盖（缺省 :8080 不变）。

**验证：** sf-stress 6/6 ×4 连续（含对 :18080 验证栈与最终部署栈）;m2-fixes 17/17；p0 30/31（唯一轮转失败 TC-060-07 冻结列 4px 漂移经 :18081 A/B 证实为更早 UI 批次的既有缺陷，与本票无关；后由收口票 05 根因修复 9a88ebc 一并治愈，p0 现 31/31）。部署口径：前端构建 + go:embed 重编 web_api，并行 :18080 实例验证，全程未动 live :8080（最终部署在票 05 统一重建）。
