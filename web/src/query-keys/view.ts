/** 视图域 query key（M2）：按表缓存视图列表；删表时清理对应视图查询（票 03）。 */
export const viewKeys = {
  all: ['view'] as const,
  listScope: () => ['view', 'list'] as const,
  /** H-41 当前表的视图列表。 */
  list: (tableId: string) => ['view', 'list', tableId] as const,
}

export type ViewListQueryKey = ReturnType<typeof viewKeys.list>
