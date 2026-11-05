/** Base 域 query key（M2）：详情/成员等后续票按需扩展；首页聚合与搜索走 home 域。 */
export const baseKeys = {
  all: ['base'] as const,
  detail: (baseId: string) => ['base', baseId] as const,
}

export type BaseDetailQueryKey = ReturnType<typeof baseKeys.detail>
