/**
 * URLs that come from settings, themes or (later) plugins are not trusted to be safe:
 * only same-site paths and http(s) URLs are allowed, everything else (javascript:,
 * data:, protocol-relative //evil) falls back.
 */
export function safeHref(value: unknown, fallback: string): string {
  if (typeof value !== 'string') return fallback
  const v = value.trim()
  if (v.startsWith('/') && !v.startsWith('//') && !v.startsWith('/\\')) return v
  try {
    const u = new URL(v)
    if (u.protocol === 'https:' || u.protocol === 'http:') return u.toString()
  } catch {
    // not an absolute URL
  }
  return fallback
}

/** Like safeHref, for CSS url(): undefined when the value is not acceptable. */
export function safeImageUrl(value: unknown): string | undefined {
  const href = safeHref(value, '')
  return href === '' ? undefined : href
}
