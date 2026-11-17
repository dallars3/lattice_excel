# 07: 公式表达式创建校验（BUG-05 后端部分）

**What to build:** 创建/修改公式字段时后端校验表达式：未知函数、未知字段引用拒绝并返回参数错误（文案含原因）；合法函数清单以 calc 引擎函数注册表为唯一事实源（连接函数为 CONCATENATE，不存在 CONCAT）；前端新建/编辑字段弹窗把校验错误行内展示。存量字段的 #ERROR 哨兵行为保持不变。

**Blocked by:** None (can start immediately).

**Status:** done

- [x] 创建含未知函数（如 CONCAT）的公式字段被拒绝，提示含「未知函数: CONCAT」
- [x] 创建引用不存在字段的公式被拒绝，提示含字段名
- [x] 合法表达式创建成功并正常计算，结果在网格正确呈现
- [x] 后端 Go 表驱动覆盖未知函数/未知引用/合法表达式/空白边界
- [x] 前端新建/编辑字段弹窗行内展示校验错误，不弹全局横幅
- [x] 既有 #ERROR 单元格 hover 原因能力回归通过

## Comments

**实现记录（2026-11-18）**
- 分支 `fix/wm2f-07-07-formula-expression-validation`，实现提交 `e59651e`，并入集成分支 `fix/web-m2-grid-fixes`（合并提交 `84d993c`）
- 前端全量 `npm test`：90 文件 / **1098 用例全绿**（新增 errorText/FieldCreateDialog/FieldConfigPanel 用例）；Go：三模块 go build 全过，calc/structure 等 11 包 ok
- 要点：calc 新增 ValidateFormulaExpression（函数清单唯一事实源=functionRegistry，未知函数/语法错误带原因）；structure 的 CreateField/DuplicateField/UpdateFieldConfig 三写路径接入校验，表达式错误以 1001+原因透传；前端 apiErrorDetailText 优先信封 message、字段对话框行内 alert 展示，1001 不进全局横幅
- 副作用归位：UpdateFieldConfig 内联错误映射与 CreateField 同口径后，选项重名从 1001 归位 1002（与契约附录 B 一致）
- 环境备注：本地全量 go test 中 calc 消费用例超时系本机 live stack 占用同一 Redis Stream 所致（stash 干净树复现同样失败），与本票无关
