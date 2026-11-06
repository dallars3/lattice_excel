# 公式计算错误阻断写入：应落 #ERROR 哨兵而非返回 1001

Status: needs-triage
Severity: P1
Source: TC-040-13（UI/API 观测）、TC-050-04 相关

## 复现步骤

1. 表中存在公式字段 `10/{数量}`。
2. 新建记录或把某行 `数量` 改为 0。

## 实际

写入请求返回：

```json
{"code":1001,"message":"参数缺失或非法","data":{"formula_errors":{"<field_id>":"除数为零"}}}
```

记录/单元格未落库，网格保持编辑态；公式单元格不会出现 `#ERROR`。

## 期望

按接口文档 §8.5.4 / TC-040-13：计算失败落库哨兵字符串 `#ERROR:<原因>`，网格/详情/历史读取一致，hover 展示原因；写入本身应成功。

## 证据

- `.scratch/fullflow-test/p1p2-api-results.json` → `TC-100-05` 等（含 0 值时写入异常）
- `.scratch/fullflow-test/probe.mjs` 复现输出：`{"code":1001,...,"formula_errors":{...:"除数为零"}}`
- UI 侧 `TC-040-13` 无法通过（见 p1p2-ui 结果）

## 影响

- 用户无法录入会导致公式报错的数据（例如分母暂为空/为 0 的中间态），与「公式错误只影响该格」的产品语义冲突；
- 历史/导入/批量写入同样被阻断。
