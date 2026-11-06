// 通知域接口（H-74）：事实源 excel_web_api/internal/gateway/notify_routes.go。
// 通知中心列表/已读操作仍属 M4，本期只消费未读计数。
import { http } from '@/core/http'

/** H-74 data 形状：protojson 省略零值，无未读时可能为 {}。 */
interface RawUnreadCount {
  count?: number
}

/** H-74 未读计数：缺失/非法值一律按 0 处理，顶栏只做只读展示。 */
export async function fetchUnreadCount(): Promise<number> {
  const raw = await http.get<RawUnreadCount | null>('/api/v1/notifications/unread-count')
  const count = raw?.count ?? 0
  return Number.isFinite(count) && count > 0 ? count : 0
}
