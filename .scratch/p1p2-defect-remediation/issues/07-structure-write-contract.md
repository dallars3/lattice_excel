# 07: 结构写入契约收口

**What to build:** 表/字段/视图/选项重名统一返回 1002；单选/多选选项维护（增删改色/排序）全量回写成功且同名归一；字段描述可持久化并跨会话返回。对应缺陷 #14、#15、#24。

**Blocked by:** None (can start immediately)

**Status:** done

- [x] 表/字段/视图/选项重名返回 1002，参数缺失/非法仍为 1001。
- [x] H-40 选项基线全量回写成功；重复名称（去空格、忽略大小写）不新增选项。
- [x] H-35 带 `description` 创建字段成功，字段列表跨会话返回该描述。
- [x] UI 选项维护与字段描述入口可用，失败时展示中文错误而非 9001。

## Comments

已收口。表/字段/视图/选项重名统一 1002、H-40 同名归一/基线回写/id 稳定、H-35 网关解码转发；根因含 requireUpdatedStructureRow（no-op UPDATE ≠ 不存在）。前端 field.ts 发送 description 属 M3+ 界面波次。
