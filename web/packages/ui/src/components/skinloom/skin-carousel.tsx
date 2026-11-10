'use client'

import { ChevronLeft, ChevronRight, Heart, Pause, Play } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type * as React from 'react'
import type { PlayerObject, SkinViewer as Viewer } from 'skinview3d'

import { avatarUrl } from '../../lib/render'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { fill, useUiLabels } from './labels'
import { prefersReducedMotion } from './skin-viewer'
import { TexturePreview } from './texture-preview'
import { UserAvatar } from './user-avatar'

export interface CarouselSkin {
  /** Texture id: links, heads and the 2D preview shown while 3D loads. */
  id: number
  name: string
  /** URL of the skin texture, /textures/{hash}. */
  skin: string
  model: 'default' | 'slim'
  href: string
  uploader?: { name: string; userId?: number; avatarVersion?: number }
  likes?: number
  /** "经典" / "纤细", already translated. */
  modelLabel?: string
}

const FADE_MS = 320 // --duration-slow
const RETURN_MS = 400
const FACING = -Math.PI / 6 // turned 30° to the viewer's left (design.md §9.5)
const NEIGHBOUR_SCALE = 0.62

/**
 * The home page's showcase (web/design.md §9.5): the latest skins take turns walking in
 * one 3D canvas, the previous and the next standing faded beside them. Every `interval`
 * the next one comes in; hovering or focusing the showcase pauses it; thumbnails jump.
 * Visitors who asked for reduced motion get still poses and no autoplay.
 */
export function SkinCarousel({
  items,
  interval = 6000,
  label,
  linkAs: LinkAs = 'a',
  className,
}: {
  items: CarouselSkin[]
  interval?: number
  /** A short title in the top-left corner, e.g. 本站最新上传. */
  label?: React.ReactNode
  /** The link component, e.g. Next's Link; a plain <a> by default. */
  linkAs?: React.ElementType
  className?: string
}) {
  const t = useUiLabels()
  const [index, setIndex] = useState(0)
  const [shown, setShown] = useState(0) // what the canvas and caption show; lags `index` during the fade
  const [phase, setPhase] = useState<'in' | 'out'>('in')
  const [playing, setPlaying] = useState(true)
  const [held, setHeld] = useState(false) // hover or keyboard focus inside
  const [ready, setReady] = useState(false)
  const reduced = useRef(false)
  const stage = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const three = useRef<{ viewer: Viewer; neighbours: [PlayerObject, PlayerObject]; load: (i: number) => Promise<void> } | null>(null)
  const n = items.length

  useEffect(() => {
    reduced.current = prefersReducedMotion()
    if (reduced.current) setPlaying(false)
  }, [])

  const go = useCallback((i: number) => setIndex(((i % n) + n) % n), [n])

  // Auto-advance. The progress bar is a CSS animation of the same length, restarted by its key.
  const running = playing && !held && n > 1
  useEffect(() => {
    if (!running) return
    const id = setTimeout(() => go(index + 1), interval)
    return () => clearTimeout(id)
  }, [running, index, interval, go])

  // Fade out, swap the skins, fade in.
  useEffect(() => {
    if (index === shown) return
    setPhase('out')
    const id = setTimeout(async () => {
      await three.current?.load(index)
      setShown(index)
      setPhase('in')
    }, FADE_MS)
    return () => clearTimeout(id)
  }, [index, shown])

  // One viewer for the three players; the current one walks.
  useEffect(() => {
    if (n === 0) return
    let disposed = false
    let viewer: Viewer | undefined
    const cleanups: (() => void)[] = []
    void Promise.all([import('skinview3d'), import('three'), import('skinview-utils')]).then(
      async ([{ SkinViewer, WalkingAnimation, PlayerObject }, { CanvasTexture, NearestFilter, SRGBColorSpace, Mesh }, { loadSkinToCanvas }]) => {
        if (disposed || !canvas.current || !stage.current) return
        const { width, height } = stage.current.getBoundingClientRect()
        viewer = new SkinViewer({ canvas: canvas.current, width, height, zoom: 0.9, fov: 38, pixelRatio: Math.min(2, window.devicePixelRatio || 1) })
        viewer.controls.enableZoom = false
        // Only the current player turns; the neighbours keep their places on the left and right.
        viewer.playerObject.rotation.y = FACING
        if (!reduced.current) {
          const walk = new WalkingAnimation()
          walk.speed = 0.6
          viewer.animation = walk
        }

        // Neighbours: smaller, behind, faded, standing still.
        const neighbours = [new PlayerObject(), new PlayerObject()] as [PlayerObject, PlayerObject]
        neighbours.forEach((p, side) => {
          // Scaled about its middle, a smaller player floats: lower it so all feet share a floor.
          p.position.set(side === 0 ? -19 : 19, -16 * (1 - NEIGHBOUR_SCALE), -10)
          p.scale.setScalar(NEIGHBOUR_SCALE)
          p.rotation.y = side === 0 ? 0.35 : -0.35 // turned a little towards the middle
          p.cape.visible = false
          p.traverse((o) => {
            if (o instanceof Mesh) {
              for (const m of [o.material].flat()) Object.assign(m, { transparent: true, opacity: 0.28, depthWrite: false })
            }
          })
          viewer!.playerWrapper.add(p)
        })

        const images = new Map<string, Promise<HTMLImageElement>>()
        const image = (url: string) => {
          if (!images.has(url)) {
            images.set(
              url,
              new Promise((resolve, reject) => {
                const img = new Image()
                img.onload = () => resolve(img)
                img.onerror = reject
                img.src = url
              }),
            )
          }
          return images.get(url)!
        }
        const dress = async (p: PlayerObject, item: CarouselSkin) => {
          const c = document.createElement('canvas')
          loadSkinToCanvas(c, await image(item.skin))
          const tex = new CanvasTexture(c)
          tex.magFilter = NearestFilter
          tex.minFilter = NearestFilter
          tex.colorSpace = SRGBColorSpace
          p.skin.map?.dispose()
          p.skin.map = tex
          p.skin.modelType = item.model
        }
        const load = async (i: number) => {
          const v = viewer
          if (!v) return
          const at = (k: number) => items[(((i + k) % n) + n) % n]
          await Promise.all([
            v.loadSkin(at(0).skin, { model: at(0).model }),
            ...(n > 1 ? [dress(neighbours[0], at(-1)), dress(neighbours[1], at(1))] : []),
          ]).catch(() => undefined)
          neighbours.forEach((p) => (p.visible = n > 1))
        }
        three.current = { viewer, neighbours, load }
        await load(0)
        if (disposed) return

        // Dragging turns the players; letting go turns them back.
        const home = viewer.camera.position.clone()
        let raf = 0
        const back = () => {
          const from = viewer!.camera.position.clone()
          const start = performance.now()
          const step = (now: number) => {
            const k = Math.min(1, (now - start) / RETURN_MS)
            const e = 1 - (1 - k) ** 3
            viewer!.camera.position.lerpVectors(from, home, e)
            viewer!.camera.lookAt(0, 0, 0)
            if (k < 1) raf = requestAnimationFrame(step)
          }
          raf = requestAnimationFrame(step)
        }
        viewer.controls.addEventListener('end', back)
        const resize = new ResizeObserver(([e]) => viewer?.setSize(e.contentRect.width, e.contentRect.height))
        resize.observe(stage.current)
        const seen = new IntersectionObserver(([e]) => {
          if (viewer) viewer.renderPaused = !e.isIntersecting || document.hidden
        })
        seen.observe(stage.current)
        cleanups.push(() => cancelAnimationFrame(raf), () => resize.disconnect(), () => seen.disconnect())
        setReady(true)
      },
    )
    return () => {
      disposed = true
      cleanups.forEach((f) => f())
      three.current = null
      viewer?.dispose()
    }
    // The items list is fixed for a page view; a new list remounts the component.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [n])

  if (n === 0) return null
  const current = items[shown]
  const pressed = (fn: () => void) => () => {
    fn()
    setPlaying(false) // choosing by hand stops the autoplay; the play button resumes it
  }

  return (
    <section
      data-slot="skin-carousel"
      aria-roledescription="carousel"
      className={cn('sl-pixel-backdrop relative flex flex-col overflow-hidden rounded-2xl border p-4 sm:p-5', className)}
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocusCapture={() => setHeld(true)}
      onBlurCapture={(e) => !e.currentTarget.contains(e.relatedTarget as Node) && setHeld(false)}
    >
      <div className="relative z-10 flex items-center gap-2">
        {label && (
          <span className="inline-flex items-center gap-1.5 rounded-md border bg-card px-2.5 py-1 text-xs text-muted-foreground">
            {label}
            <span className="font-mono text-faint-foreground">{fill(t.position, { current: shown + 1, total: n })}</span>
          </span>
        )}
        <span className="flex-1" />
        {n > 1 && (
          <>
            <Button variant="outline" size="icon-sm" onClick={() => setPlaying((p) => !p)} aria-label={running ? t.pause : t.play} aria-pressed={!playing}>
              {playing ? <Pause /> : <Play />}
            </Button>
            <Button variant="outline" size="icon-sm" onClick={pressed(() => go(index - 1))} aria-label={t.previous}>
              <ChevronLeft />
            </Button>
            <Button variant="outline" size="icon-sm" onClick={pressed(() => go(index + 1))} aria-label={t.next}>
              <ChevronRight />
            </Button>
          </>
        )}
      </div>

      <div
        ref={stage}
        aria-live={running ? 'off' : 'polite'}
        className={cn(
          'relative min-h-56 flex-1 transition-[opacity,translate] duration-(--duration-slow) ease-out',
          phase === 'out' ? '-translate-x-6 opacity-0' : 'translate-x-0 opacity-100',
        )}
      >
        {!ready && (
          <div className="absolute inset-0 flex items-end justify-center pb-4">
            <TexturePreview texture={{ id: current.id }} type={current.model === 'slim' ? 'alex' : 'steve'} height={200} />
          </div>
        )}
        <canvas ref={canvas} role="img" aria-label={current.name} className={cn('absolute inset-0 block size-full cursor-grab active:cursor-grabbing', ready ? 'opacity-100' : 'opacity-0')} />
      </div>

      <div className={cn('relative z-10 flex flex-col items-center gap-1.5 pt-3 pb-4 text-center transition-opacity duration-(--duration-slow)', phase === 'out' && 'opacity-0')}>
        <LinkAs href={current.href} className="text-lg font-medium underline-offset-4 hover:underline">
          {current.name}
        </LinkAs>
        <div className="flex items-center gap-2 text-caption text-muted-foreground">
          {current.uploader && (
            <span className="inline-flex items-center gap-1.5">
              {current.uploader.userId !== undefined && <UserAvatar of={{ user: current.uploader.userId, version: current.uploader.avatarVersion }} size={16} />}
              {current.uploader.name}
            </span>
          )}
          {current.modelLabel && <span aria-hidden>·</span>}
          {current.modelLabel}
          {current.likes !== undefined && (
            <>
              <span aria-hidden>·</span>
              <span className="inline-flex items-center gap-1 tabular-nums">
                <Heart className="size-3.5" aria-label={t.likes} />
                {current.likes}
              </span>
            </>
          )}
        </div>
      </div>

      {n > 1 && (
        <div className="relative z-10 flex justify-center gap-2.5 max-sm:hidden">
          {items.map((item, i) => (
            <button
              key={item.id}
              type="button"
              aria-current={i === index}
              aria-label={item.name}
              onClick={pressed(() => go(i))}
              className="group flex flex-col items-center gap-1.5 rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <img
                src={avatarUrl({ texture: item.id }, 36)}
                alt=""
                width={36}
                height={36}
                className={cn(
                  'rounded-md border bg-card transition-[opacity,border-color] duration-150 [image-rendering:pixelated]',
                  i === index ? 'border-2 border-primary' : 'opacity-55 group-hover:opacity-100',
                )}
              />
              <span className="h-[3px] w-9 overflow-hidden rounded-full bg-muted">
                {i === index && (
                  <span
                    key={`${index}-${running}`}
                    className="block h-full origin-left rounded-full bg-primary"
                    style={running ? { animation: `sl-carousel-progress ${interval}ms linear forwards` } : { transform: 'scaleX(0.35)' }}
                  />
                )}
              </span>
            </button>
          ))}
        </div>
      )}
      {n > 1 && (
        <div className="relative z-10 flex justify-center gap-1.5 sm:hidden" aria-hidden>
          {items.map((item, i) => (
            <span key={item.id} className={cn('h-1.5 rounded-full transition-colors duration-200', i === index ? 'w-4 bg-primary' : 'w-1.5 bg-input')} />
          ))}
        </div>
      )}
    </section>
  )
}
