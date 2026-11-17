# Ctrl+F 视图内搜索未实现（快捷键空操作）

Status: needs-triage
Severity: P2
Source: TC-050-16（UI）

## 复现步骤

1. 打开 Base 工作台，聚焦网格。
2. 按 `Ctrl+F`。

## 实际

无任何搜索框/高亮出现；键盘状态机的 `find` 分支直接 `return`（`GridArea.tsx` case 'find'）。

## 期望

按 AC-REC-04 周边体验 / 技术方案 4.5：Ctrl+F 呼出视图内搜索，对当前已加载数据过滤+高亮，不影响服务端视图配置。

## 证据

- `.scratch/fullflow-test/p1p2-ui-run.log` → `TC-050-16`
- 代码：`web/src/features/workspace/components/GridArea.tsx` 键盘 `case 'find': return`

## 影响

P2 体验项缺失；不影响数据正确性。
