// 单元格值编解码（票 05）：接口文档 8.0.6 值编码字典 + H-48 protojson common.Value 包装。
// - decode：响应侧必须处理 `{string_value}` / `{int_value:"7"}` / `{double_value}` /
//   `{date_value_ms:"..."}` / `{list_value:{value_list:[...]}}`（int64 为字符串）以及 null/原始值/数组。
// - encode：请求侧可发普通 JSON（数字、字符串、数组、URL 对象、null），日期为毫秒数字。
// 纯函数、DOM 无关；空值 null 与数字 0 严格区分。

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function toFiniteNumber(raw: unknown): number | null {
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : null
  if (typeof raw === 'string' && raw.trim() !== '') {
    const parsed = Number(raw)
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

function hasValueKind(raw: Record<string, unknown>): boolean {
  return (
    'string_value' in raw ||
    'int_value' in raw ||
    'double_value' in raw ||
    'bool_value' in raw ||
    'date_value_ms' in raw ||
    'list_value' in raw ||
    'value_list' in raw
  )
}

function decodeListValue(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw.map(decodeCellValue)
  if (isRecord(raw)) {
    const list = raw.value_list
    if (Array.isArray(list)) return list.map(decodeCellValue)
  }
  return []
}

/** common.Value 包装 → 普通值；非包装对象/数组递归解码。 */
export function decodeCellValue(raw: unknown): unknown {
  if (raw === null || raw === undefined) return null
  if (Array.isArray(raw)) return raw.map(decodeCellValue)
  if (!isRecord(raw)) return raw
  if (!hasValueKind(raw)) {
    // 空 Value（oneof 未设置）或普通对象（如 url {url,display_text}）
    if (Object.keys(raw).length === 0) return null
    const decoded: Record<string, unknown> = {}
    for (const [key, value] of Object.entries(raw)) {
      decoded[key] = decodeCellValue(value)
    }
    return decoded
  }
  if ('string_value' in raw) {
    return raw.string_value === null || raw.string_value === undefined
      ? null
      : String(raw.string_value)
  }
  if ('int_value' in raw) return toFiniteNumber(raw.int_value)
  if ('double_value' in raw) return toFiniteNumber(raw.double_value)
  if ('date_value_ms' in raw) return toFiniteNumber(raw.date_value_ms)
  if ('bool_value' in raw) return Boolean(raw.bool_value)
  return decodeListValue('list_value' in raw ? raw.list_value : raw.value_list)
}

/** 记录 cells_map：field_id → 普通值（缺失/非法整体按空对象）。 */
export function decodeCellsMap(raw: unknown): Record<string, unknown> {
  if (!isRecord(raw)) return {}
  const cells: Record<string, unknown> = {}
  for (const [fieldId, value] of Object.entries(raw)) {
    cells[fieldId] = decodeCellValue(value)
  }
  return cells
}

/** 普通值 → 请求 JSON：递归克隆，日期保持毫秒数字、URL 保持对象、空值保持 null。 */
export function encodeCellValue(plain: unknown): unknown {
  if (plain === null || plain === undefined) return null
  if (Array.isArray(plain)) return plain.map(encodeCellValue)
  if (isRecord(plain)) {
    const encoded: Record<string, unknown> = {}
    for (const [key, value] of Object.entries(plain)) {
      encoded[key] = encodeCellValue(value)
    }
    return encoded
  }
  if (typeof plain === 'number' && !Number.isFinite(plain)) return null
  return plain
}
