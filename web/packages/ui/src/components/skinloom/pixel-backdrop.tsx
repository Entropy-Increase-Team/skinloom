import type * as React from 'react'

import { cn } from '../../lib/utils'

/**
 * The 8px checkerboard every skin and cape preview stands on (web/design.md §8): it shows
 * the transparent parts of a texture, like an image editor. Only behind previews.
 * Themes restyle it through .sl-pixel-backdrop and the pixel-a / pixel-b tokens.
 */
export function PixelBackdrop({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="pixel-backdrop" className={cn('sl-pixel-backdrop flex items-center justify-center overflow-hidden rounded-lg', className)} {...props} />
}
