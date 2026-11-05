/** 数据表域 query key（M2）：按 Base 缓存表列表，结构写成功后失效整个 list 前缀。 */
export const tableKeys = {
  all: ['table'] as const,
  /** 表列表前缀（结构写成功后按 Base 精确失效或整域失效）。 */
  listScope: () => ['table', 'list'] as const,
  /** H-29 当前 Base 的表列表。 */
  list: (baseId: string) => ['table', 'list', baseId] as const,
}

export type TableListQueryKey = ReturnType<typeof tableKeys.list>
