// 单表记录容量（票 06 / AC-REC-09 / 5001）：已知已加载达到 5 万行时禁用新增并提示。
// 票 09 起上限与文案统一由 workspace/capacity.ts 提供。
import {
  MAX_RECORDS_PER_TABLE,
  RECORD_LIMIT_MESSAGE,
} from '@/features/workspace/capacity'

export { MAX_RECORDS_PER_TABLE, RECORD_LIMIT_MESSAGE }

export const RECORD_ROW_LIMIT = MAX_RECORDS_PER_TABLE

/** loaded >= 5 万即视为已知达到上限（hasMore 只影响是否还有未加载行，不改变下界）。 */
export function isRecordCapacityReached(loadedCount: number, _hasMore = false): boolean {
  return loadedCount >= RECORD_ROW_LIMIT
}
