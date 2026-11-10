'use client'

import { useEffect, useRef, useState } from 'react'
import type { SkinViewer as Viewer } from 'skinview3d'

import { cn } from '../../lib/utils'

/** The visitor asked for less motion (web/design.md §6). */
export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * The single view of a skin (web/design.md §8.1): a 3D player that can walk and be
 * dragged around, or a cape shown from behind. Loaded in the browser only. The canvas is
 * transparent; the theme's viewer.background (--sl-viewer-background) and, by default,
 * the pixel backdrop show through. Stops rendering while off screen; no walking or
 * spinning for visitors who asked for reduced motion.
 */
export function SkinViewer({
  skin,
  cape,
  model = 'auto-detect',
  animation = 'walk',
  autoRotate = false,
  label,
  className,
}: {
  /** URL of the skin texture (/textures/{hash}); null shows only the cape. */
  skin: string | null
  cape?: string | null
  model?: 'default' | 'slim' | 'auto-detect'
  animation?: 'walk' | 'idle' | 'none'
  autoRotate?: boolean
  /** Describes what is shown, for screen readers. */
  label: string
  className?: string
}) {
  const box = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [state, setState] = useState<'loading' | 'ready' | 'failed'>('loading')

  useEffect(() => {
    let viewer: Viewer | undefined
    let disposed = false
    const cleanups: (() => void)[] = []
    void import('skinview3d')
      .then(async ({ SkinViewer, WalkingAnimation, IdleAnimation }) => {
        if (disposed || !canvas.current || !box.current) return
        const { width, height } = box.current.getBoundingClientRect()
        viewer = new SkinViewer({ canvas: canvas.current, width, height, zoom: 0.8, pixelRatio: Math.min(2, window.devicePixelRatio || 1) })
        const still = prefersReducedMotion()
        if (skin) await viewer.loadSkin(skin, { model })
        else viewer.playerObject.skin.visible = false
        if (cape) {
          await viewer.loadCape(cape)
          if (!skin) viewer.playerObject.rotation.y = Math.PI // a cape alone is seen from behind
        }
        if (disposed) return
        if (!still && animation !== 'none') {
          const a = animation === 'walk' ? new WalkingAnimation() : new IdleAnimation()
          a.speed = 0.6
          viewer.animation = a
        }
        viewer.autoRotate = autoRotate && !still
        viewer.autoRotateSpeed = 0.6
        const resize = new ResizeObserver(([e]) => viewer?.setSize(e.contentRect.width, e.contentRect.height))
        resize.observe(box.current)
        const seen = new IntersectionObserver(([e]) => {
          if (viewer) viewer.renderPaused = !e.isIntersecting
        })
        seen.observe(box.current)
        cleanups.push(() => resize.disconnect(), () => seen.disconnect())
        setState('ready')
      })
      .catch(() => !disposed && setState('failed'))
    return () => {
      disposed = true
      cleanups.forEach((f) => f())
      viewer?.dispose()
    }
  }, [skin, cape, model, animation, autoRotate])

  return (
    <div
      ref={box}
      data-slot="skin-viewer"
      data-state={state}
      role="img"
      aria-label={label}
      className={cn('sl-viewer relative h-full w-full bg-(--sl-viewer-background)', className)}
    >
      {state !== 'failed' && (
        <canvas
          ref={canvas}
          className={cn(
            'block h-full w-full cursor-grab transition-opacity duration-300 ease-out active:cursor-grabbing',
            state === 'ready' ? 'opacity-100' : 'opacity-0',
          )}
        />
      )}
    </div>
  )
}
