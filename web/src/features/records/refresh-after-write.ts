// 写后整行刷新（票 06 / BUG-05 前端部分）：记录写链成功后拉取 H-49 单记录，
// 用服务端整行（含重算后的公式/引用列）回灌本地缓存。后端写路径同步重算，写后即读必得新值；
// 不扩展后端响应结构（proto 扩展留待 M3 与 WS 一并评估）。
// 拉取失败静默降级：写成功 UI 不回滚、不弹错，下次列表拉取自愈。
import { fetchRecord, type RecordRow } from './api'

/** 逐条拉取整行并回调合并；单条失败静默忽略，其余条目不受影响。 */
export async function refreshRecordRows(
  baseId: string,
  recordIds: readonly string[],
  onRow: (row: RecordRow) => void,
): Promise<void> {
  await Promise.all(
    recordIds.map(async (recordId) => {
      try {
        onRow(await fetchRecord(recordId, baseId))
      } catch {
        // 静默降级：整行刷新失败不影响写成功状态，下次拉取自愈。
      }
    }),
  )
}
