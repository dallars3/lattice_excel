/** 视图域 query key（M2）：按表缓存视图列表；删表时清理对应视图查询（票 03）。 */
export const viewKeys = {
  all: ['view'] as const,
  listScope: () => ['view', 'list'] as const,
  /** H-41 当前表的视图列表。 */
  list: (tableId: string) => ['view', 'list', tableId] as const,
  /** H-46/H-47 视图配置前缀（票 04 删除字段后按视图清理失效条件）。 */
  configScope: () => ['view', 'config'] as const,
  config: (viewId: string) => ['view', 'config', viewId] as const,
}

export type ViewListQueryKey = ReturnType<typeof viewKeys.list>
