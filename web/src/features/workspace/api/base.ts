// 工作台 Base 事实源（契约缺口 G：无 GET /bases/{id}）：
// 深链 /base/:id 用 H-21 聚合的四列表去重查找；四区任一命中即可，跨区重复取首次命中。
import { fetchHome, type BaseSummary } from '@/features/home/api'

/** 按 id 在 H-21 四列表中查找 Base；不存在（或无权限）返回 null。 */
export async function fetchBaseById(baseId: string): Promise<BaseSummary | null> {
  const home = await fetchHome()
  const lists = [home.recentList, home.createdList, home.sharedList, home.starList]
  for (const list of lists) {
    const hit = list.find((base) => base.id === baseId)
    if (hit !== undefined) return hit
  }
  return null
}
