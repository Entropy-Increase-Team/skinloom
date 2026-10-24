'use client'

import { useEffect, useRef, useState } from 'react'
import { inferModelType, loadSkinToCanvas } from 'skinview-utils'

import { capeFront, drawParts, FIGURE, frontParts, type SkinModel } from '@/lib/skin'

/**
 * A flat front view of a skin, or the outer face of a cape, drawn in the browser from
 * the texture at src (a /textures/{hash} URL, or a local file being uploaded).
 * Legacy 64×32 skins are converted to the modern layout first.
 */
export function TexturePreview({
  src,
  type,
  height = 160,
  className,
}: {
  src: string
  type: 'steve' | 'alex' | 'cape'
  height?: number
  className?: string
}) {
  const ref = useRef<HTMLCanvasElement>(null)
  const [failed, setFailed] = useState(false)
  const figure = type === 'cape' ? FIGURE.cape : FIGURE.skin
  const scale = Math.max(1, Math.floor(height / figure.h))

  useEffect(() => {
    const img = new Image()
    img.decoding = 'async'
    img.onload = () => {
      const ctx = ref.current?.getContext('2d')
      if (!ctx) return
      ctx.clearRect(0, 0, ctx.canvas.width, ctx.canvas.height)
      if (type === 'cape') {
        drawParts(ctx, img, img.naturalWidth, [capeFront], scale)
        return
      }
      const skin = document.createElement('canvas')
      loadSkinToCanvas(skin, img)
      const model: SkinModel = type === 'alex' || inferModelType(skin) === 'slim' ? 'alex' : 'steve'
      drawParts(ctx, skin, skin.width, frontParts(model), scale)
    }
    img.onerror = () => setFailed(true)
    img.src = src
  }, [src, type, scale])

  if (failed) return <div className={className} style={{ height }} />
  return (
    <canvas
      ref={ref}
      width={figure.w * scale}
      height={figure.h * scale}
      className={className}
      style={{ imageRendering: 'pixelated' }}
      aria-hidden
    />
  )
}
