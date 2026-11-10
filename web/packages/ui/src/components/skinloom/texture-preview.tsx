import { cn } from '../../lib/utils'
import { previewUrl, previewWidth } from '../../lib/render'

export type TextureType = 'steve' | 'alex' | 'cape'

/**
 * The dual view of a texture (web/design.md §8.1): a skin from the front and the back
 * side by side, or a cape's outer face, rendered by the server. Every preview in a list
 * has the same height, so the figures line up.
 */
export function TexturePreview({
  texture,
  type,
  height,
  alt = '',
  className,
}: {
  /** By id (respects the texture's privacy for the viewer) or by file hash. */
  texture: { id: number } | { hash: string }
  type: TextureType
  /** Displayed height in CSS pixels. */
  height: number
  alt?: string
  className?: string
}) {
  const width = previewWidth(type, height)
  return (
    // eslint-disable-next-line @next/next/no-img-element -- server-rendered and cached by the backend
    <img
      data-slot="texture-preview"
      src={previewUrl(texture, height)}
      width={width}
      height={height}
      alt={alt}
      loading="lazy"
      decoding="async"
      draggable={false}
      className={cn('max-w-full object-contain [image-rendering:pixelated]', className)}
    />
  )
}
