/** A short "Browser · OS" label for a User-Agent string, for the signed-in devices list. */
export function describeUA(ua: string): string {
  const browser =
    [
      [/Edg\//, 'Edge'],
      [/OPR\//, 'Opera'],
      [/Firefox\//, 'Firefox'],
      [/Chrome\//, 'Chrome'],
      [/Safari\//, 'Safari'],
      [/curl\//, 'curl'],
    ].find(([re]) => (re as RegExp).test(ua))?.[1] ?? ''
  const os =
    [
      [/Windows/, 'Windows'],
      [/Android/, 'Android'],
      [/iPhone|iPad|iOS/, 'iOS'],
      [/Mac OS X|Macintosh/, 'macOS'],
      [/Linux/, 'Linux'],
    ].find(([re]) => (re as RegExp).test(ua))?.[1] ?? ''
  return [browser, os].filter(Boolean).join(' · ') || ua.slice(0, 40) || '?'
}
