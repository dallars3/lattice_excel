# 02: 导入 Sheet 校验与失败清理

**What to build:** 用户选择第 N 个 Sheet 时，若 N 越界立即得到 1001 且不创建导入任务；导入中的 Base 对他人不可访问；导入失败或客户端断连时自动清理半成品 Base，不留下可访问空壳；同一文件修正映射后可重试。对应缺陷 #22、#25。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 对 2 个 Sheet 的文件传 `sheet_index=5` 返回 1001，且无导入任务产生。
- [ ] 部分失败导入返回行级报告（行号/列名/原值/结果），ok/fail 计数正确。
- [ ] 20k 行导入过程中客户端中断后，任务转 failed 且半成品 Base 被清理或保持不可访问。
- [ ] 同一 `file_id` 修正映射后可重试成功，无需重新上传。
