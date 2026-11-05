// 网格元数据（票 05）：人员字段显示映射（H-63）与关联字段目标记录标签（H-41 + H-48 limit=100）。
// 仅当表内存在对应字段类型时由 GridArea 触发；失败一律降级为空映射（显示 user_id / record_id），
// 不阻塞记录主流程。
import { useQuery } from '@tanstack/react-query'

import { http } from '@/core/http'
import { formatNumber, type NumberCellConfig, type PersonInfo } from '@/core/grid'
import { fetchFields, type FieldRecord } from '@/features/workspace/api/field'
import { fetchViews } from '@/features/workspace/api/view'

import { queryRecords } from './api'

export interface PersonDirectory {
  readonly [userId: string]: PersonInfo
}

export interface RelationTargetInfo {
  readonly fields: FieldRecord[]
  readonly labels: Record<string, string>
}

export interface RelationMetadata {
  readonly [tableId: string]: RelationTargetInfo
}

interface RawCollaborator {
  user_id?: string
  username?: string
  display_name?: string
}

interface RawCollaboratorList {
  collaborator_list?: RawCollaborator[]
}

/** H-63 协作者 → user_id 显示映射；失败抛出由调用方降级。 */
export async function fetchPersonDirectory(baseId: string): Promise<PersonDirectory> {
  const raw = await http.get<RawCollaboratorList>(
    `/api/v1/bases/${encodeURIComponent(baseId)}/shares/members`,
  )
  const directory: Record<string, PersonInfo> = {}
  for (const collaborator of raw.collaborator_list ?? []) {
    const userId = collaborator.user_id ?? ''
    if (userId === '') continue
    directory[userId] = {
      name: collaborator.display_name || collaborator.username || userId,
    }
  }
  return directory
}

/** 目标记录主字段展示值（主字段允许 text/number/select/formula）。 */
function primaryLabel(field: FieldRecord, value: unknown): string {
  if (value === null || value === undefined) return ''
  if (field.type === 'number') {
    return formatNumber(
      typeof value === 'number' ? value : Number(value),
      field.configMap as NumberCellConfig,
    )
  }
  if (field.type === 'select') {
    const options = field.configMap.option_list
    if (Array.isArray(options)) {
      const matched = options.find(
        (option) => (option as { id?: unknown }).id === value,
      ) as { name?: string } | undefined
      if (matched?.name !== undefined) return matched.name
    }
  }
  return String(value)
}

/** 单目标表：字段结构 + 默认视图首屏记录（limit=100）→ record_id 主字段标签映射。 */
export async function fetchRelationTarget(
  baseId: string,
  tableId: string,
): Promise<RelationTargetInfo> {
  const [fields, views] = await Promise.all([fetchFields(tableId), fetchViews(tableId)])
  const primary = fields.find((field) => field.isPrimary) ?? fields[0]
  let labels: Record<string, string> = {}
  if (primary !== undefined) {
    try {
      const page = await queryRecords({
        baseId,
        tableId,
        viewId: views[0]?.id,
        limit: 100,
      })
      labels = Object.fromEntries(
        page.items.map((record) => [
          record.id,
          primaryLabel(primary, record.cellsMap[primary.id]) || record.id,
        ]),
      )
    } catch {
      // 记录首屏失败：结构仍可用于引用类型；标签降级为 record_id（渲染层回退）。
      labels = {}
    }
  }
  return { fields, labels }
}

/** 批量目标表元数据：单表失败不影响其他表（allSettled 降级）。 */
export async function fetchRelationMetadata(
  baseId: string,
  tableIds: readonly string[],
): Promise<RelationMetadata> {
  const results = await Promise.allSettled(
    tableIds.map(async (tableId) => [tableId, await fetchRelationTarget(baseId, tableId)] as const),
  )
  const metadata: Record<string, RelationTargetInfo> = {}
  for (const result of results) {
    if (result.status === 'fulfilled') {
      metadata[result.value[0]] = result.value[1]
    }
  }
  return metadata
}

/** 人员显示映射查询：仅在存在人员字段时启用，失败降级空对象。 */
export function usePersonDirectory(baseId: string, enabled: boolean) {
  return useQuery({
    queryKey: ['records', 'members', baseId],
    queryFn: () => fetchPersonDirectory(baseId),
    enabled: enabled && baseId !== '',
    retry: false,
  })
}

/** 关联目标表元数据查询：仅在存在关系/引用字段时启用，失败降级空对象。 */
export function useRelationMetadata(
  baseId: string,
  targetTableIds: readonly string[],
  enabled: boolean,
) {
  const ids = [...new Set(targetTableIds)].sort()
  return useQuery({
    queryKey: ['records', 'targets', baseId, ...ids],
    queryFn: () => fetchRelationMetadata(baseId, ids),
    enabled: enabled && baseId !== '' && ids.length > 0,
    retry: false,
  })
}
