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

/**
 * Where to go after signing in: only a path on this site (an attacker-supplied
 * ?next=https://evil would otherwise turn the login page into an open redirect), and
 * never back to the auth pages themselves.
 */
export function safeNext(value: unknown): string {
  if (typeof value !== 'string') return '/'
  const v = value.trim()
  if (!v.startsWith('/') || v.startsWith('//') || v.startsWith('/\\') || /[\u0000-\u001f]/.test(v)) return '/'
  if (v === '/setup' || v.startsWith('/setup?') || v.startsWith('/auth/')) return '/'
  return v
}

/** Like safeHref, for CSS url(): undefined when the value is not acceptable. */
export function safeImageUrl(value: unknown): string | undefined {
  const href = safeHref(value, '')
  return href === '' ? undefined : href
}
