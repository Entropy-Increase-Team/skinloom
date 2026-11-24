/** Formats a byte count into B / KB / MB / GB / TB with locale-aware digits. */
export function formatBytes(bytes: number, locale = 'en'): string {
  const safe = Number.isFinite(bytes) && bytes > 0 ? bytes : 0
  const units = ['B', 'KB', 'MB', 'GB', 'TB'] as const
  let i = 0
  let v = safe
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v)} ${units[i]}`
}
