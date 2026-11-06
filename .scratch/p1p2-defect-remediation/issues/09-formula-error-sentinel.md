# 09: 公式错误哨兵

**What to build:** 公式计算失败时不再阻断记录/单元格写入，而是按字段落 `#ERROR:<原因>`；列表、单条、历史读取一致，UI 展示 `#ERROR` 与 hover 原因；只有保存时校验类错误（字段不存在/类型不匹配/环）才拒绝保存。对应缺陷 #17。

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] 除数为零时记录创建/更新成功，公式单元格为 `#ERROR:除数为零`。
- [ ] H-48、H-49、H-55 读到同一哨兵值。
- [ ] 自引用、字段不存在、类型不匹配仍在保存时被拒。
- [ ] UI 网格显示 `#ERROR`，hover 展示原因。
