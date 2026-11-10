import { avatarUrl } from '../../lib/render'
import { cn } from '../../lib/utils'

/**
 * A head from a skin, drawn by the server: a user's chosen avatar or a texture's face.
 * Square with a small radius, never a circle: Minecraft heads are pixel blocks
 * (web/design.md §5).
 */
export function UserAvatar({
  of,
  size = 24,
  cube = false,
  alt = '',
  className,
}: {
  /** A user (version changes when they pick another avatar) or a texture. */
  of: { user: number; version?: number } | { texture: number }
  size?: 16 | 20 | 24 | 28 | 32 | 36 | 40 | 48 | 64
  /** The head as a cube seen from above instead of the flat face. */
  cube?: boolean
  alt?: string
  className?: string
}) {
  return (
    // eslint-disable-next-line @next/next/no-img-element -- a small server-rendered image, already sized
    <img
      data-slot="user-avatar"
      src={avatarUrl(of, size, cube)}
      width={size}
      height={size}
      alt={alt}
      decoding="async"
      className={cn('shrink-0 rounded-sm bg-muted [image-rendering:pixelated]', className)}
    />
  )
}
