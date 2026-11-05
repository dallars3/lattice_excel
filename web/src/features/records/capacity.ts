// 单表记录容量（票 06 / AC-REC-09 / 5001）：已知已加载达到 5 万行时禁用新增并提示。

export const RECORD_ROW_LIMIT = 50000

/** loaded >= 5 万即视为已知达到上限（hasMore 只影响是否还有未加载行，不改变下界）。 */
export function isRecordCapacityReached(loadedCount: number, _hasMore = false): boolean {
  return loadedCount >= RECORD_ROW_LIMIT
}

export const RECORD_LIMIT_MESSAGE = '已达单表 5 万行上限，请联系管理者'
