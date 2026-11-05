/** 首页域 query key（技术方案第 7 章）：聚合为固定 key，搜索按关键词独立缓存。 */
export const homeKeys = {
  all: ['home'] as const,
  /** H-21 四区聚合。 */
  aggregate: () => ['home', 'aggregate'] as const,
  /** 搜索缓存前缀（写成功后按需失效全部搜索）。 */
  searchScope: () => ['home', 'search'] as const,
  /** H-22 名称搜索。 */
  search: (q: string) => ['home', 'search', q] as const,
}

export type HomeAggregateQueryKey = ReturnType<typeof homeKeys.aggregate>
export type HomeSearchQueryKey = ReturnType<typeof homeKeys.search>
