/**
 * URLs of the images the backend renders (internal/httpapi/render.go). The server only
 * renders a few sizes and snaps other requests to them, so we ask for a listed size at
 * twice the displayed one, for sharp pixels on HiDPI screens.
 */

/** Heights /preview serves (render.PreviewHeights). */
export const PREVIEW_HEIGHTS = [64, 100, 128, 150, 200, 256, 300, 400, 512] as const
/** Sizes /avatar serves (render.AvatarSizes). */
export const AVATAR_SIZES = [16, 24, 32, 48, 64, 96, 100, 128, 256, 512] as const

/** The smallest listed size at least `want`, or the largest one. */
export function snapUp(want: number, sizes: readonly number[]): number {
  return sizes.find((s) => s >= want) ?? sizes[sizes.length - 1]
}

/**
 * The dual view of a skin (front and back side by side, 36:32) or the outer face of a
 * cape (10:16), by texture id or by file hash, for display at `height` CSS pixels.
 */
export function previewUrl(texture: { id: number } | { hash: string }, height: number): string {
  const h = snapUp(height * 2, PREVIEW_HEIGHTS)
  return 'id' in texture ? `/preview/${texture.id}?height=${h}` : `/preview/hash/${encodeURIComponent(texture.hash)}?height=${h}`
}

/** Width of a preview at `height`: skins 36:32, capes 10:16 (render.SkinPreview, CapePreview). */
export function previewWidth(type: 'steve' | 'alex' | 'cape', height: number): number {
  return Math.round(type === 'cape' ? (height * 10) / 16 : (height * 36) / 32)
}

/**
 * A head (the face of the skin), for a user's chosen avatar or for a texture, at `size`
 * CSS pixels. `version` changes the URL when a user picks another avatar.
 */
export function avatarUrl(of: { user: number; version?: number } | { texture: number }, size: number, cube = false): string {
  const s = snapUp(size * 2, AVATAR_SIZES)
  const base = 'user' in of ? `/avatar/user/${of.user}?size=${s}&v=${of.version ?? 0}` : `/avatar/${of.texture}?size=${s}`
  return cube ? `${base}&3d` : base
}
