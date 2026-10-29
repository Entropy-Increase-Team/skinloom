/**
 * A user's avatar: the head of the skin they chose, drawn by the server (a grey head
 * when there is none). version changes the URL when the choice changes, so the
 * browser does not keep showing the previous one for the cache time.
 */
export function Avatar({ userId, size, version, cube, className }: { userId: number; size: number; version?: number; cube?: boolean; className?: string }) {
  const src = `/avatar/user/${userId}?size=${size * 2}${cube ? '&3d' : ''}&v=${version ?? 0}` // 2× for sharp pixels on HiDPI screens
  return (
    // eslint-disable-next-line @next/next/no-img-element -- a small server-rendered image, already sized
    <img src={src} width={size} height={size} alt="" className={className} style={{ imageRendering: 'pixelated' }} />
  )
}
