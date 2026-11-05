/** 字段域 query key（M2）：按表缓存字段列表，结构写成功后失效该表字段前缀。 */
export const fieldKeys = {
  all: ['field'] as const,
  listScope: () => ['field', 'list'] as const,
  /** H-34 当前表的字段列表。 */
  list: (tableId: string) => ['field', 'list', tableId] as const,
}

export type FieldListQueryKey = ReturnType<typeof fieldKeys.list>
