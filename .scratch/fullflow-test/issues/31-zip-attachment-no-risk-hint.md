# 压缩包附件可上传但无风险提示

Status: needs-triage
Severity: P2
Source: TC-070-04（UI）

## 实际

- `.exe` 上传被拒（符合预期）；
- `.zip` 上传成功并出现在附件列表，但界面无任何「压缩包不解压查杀/存在风险」提示。

## 期望

按 4.3 附件矩阵：压缩包允许上传但不解压查杀，需向用户提示风险。

## 证据

- `.scratch/fullflow-test/p1p2-ui-run.log` → `TC-070-04`
- 代码检索：`RecordDrawer` 无 zip/风险提示文案

## 影响

P2 安全告知缺失。
