// 首页卡片时间展示：毫秒时间戳 → 本地化「YYYY/MM/DD HH:mm」；缺失时间显示占位。
const dateTimeFormatter = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

export function formatModifiedAt(timestamp: number): string {
  if (!Number.isFinite(timestamp) || timestamp <= 0) return '—'
  return dateTimeFormatter.format(new Date(timestamp))
}
