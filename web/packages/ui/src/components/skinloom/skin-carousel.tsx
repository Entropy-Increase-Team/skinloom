'use client'

import { ArrowUpRight, ChevronLeft, ChevronRight, Heart, Pause, Play } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type * as React from 'react'
import type { PlayerObject, SkinViewer as Viewer } from 'skinview3d'

import { avatarUrl } from '../../lib/render'
import { cn } from '../../lib/utils'
import { Badge } from '../ui/badge'
import { Button, ButtonLink } from '../ui/button'
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

const RETURN_MS = 400
const FACING = -Math.PI / 6 // turned 30° to the viewer's left (design.md §9.5)
const NEIGHBOUR_SCALE = 0.64
const WALK_SPEED = 0.6
const NUM_FMT = new Intl.NumberFormat('en-US')

interface SlotTarget {
  x: number
  y: number
  z: number
  scale: number
  rotY: number
  opacity: number
}

function targetForOffset(offset: number): SlotTarget {
  if (offset === 0) {
    return { x: 0, y: 0, z: 0, scale: 1, rotY: FACING, opacity: 1 }
  }
  if (offset === -1) {
    return { x: -18, y: -16 * (1 - NEIGHBOUR_SCALE), z: -10, scale: NEIGHBOUR_SCALE, rotY: 0.35, opacity: 0.72 }
  }
  if (offset === 1) {
    return { x: 18, y: -16 * (1 - NEIGHBOUR_SCALE), z: -10, scale: NEIGHBOUR_SCALE, rotY: -0.35, opacity: 0.72 }
  }
  const sign = offset < 0 ? -1 : 1
  return { x: sign * 30, y: -16 * 0.5, z: -20, scale: 0.5, rotY: sign * -0.5, opacity: 0 }
}

/** Circular slot offset of item k relative to active index in a ring of n items. */
function slotOffset(k: number, active: number, n: number): number {
  if (n <= 1) return 0
  if (n === 2) return k === active ? 0 : 1
  const d = (((k - active) % n) + n) % n
  if (d === 0) return 0
  if (d === 1) return 1
  if (d === n - 1) return -1
  return d <= n / 2 ? 2 : -2
}

/**
 * The home page's showcase (web/design.md §9.5): all skins live in one 3D canvas.
 * The active skin stands in the centre and walks while the previous and next skins stand
 * on its left and right; switching glides 1-2-3 -> 2-3-1 -> 3-1-2 inside the same scene.
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
  const [playing, setPlaying] = useState(true)
  const [held, setHeld] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [ready, setReady] = useState(false)
  const [visible, setVisible] = useState(true)
  const indexRef = useRef(0)
  const playingRef = useRef(true)
  const stage = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const detailLinkRef = useRef<HTMLAnchorElement | null>(null)
  const n = items.length
  const safeIndex = n > 0 ? Math.min(index, n - 1) : 0

  indexRef.current = safeIndex
  playingRef.current = playing

  useEffect(() => {
    if (prefersReducedMotion()) {
      setPlaying(false)
      playingRef.current = false
    }
  }, [])

  const go = useCallback((i: number) => {
    if (n <= 0) return
    setIndex(((i % n) + n) % n)
  }, [n])

  // Auto-advance when visible, playing, and not paused by hover/focus or active 3D dragging.
  const running = visible && playing && !held && !dragging && n > 1
  useEffect(() => {
    if (!running) return
    const id = setTimeout(() => go(safeIndex + 1), interval)
    return () => clearTimeout(id)
  }, [running, safeIndex, interval, go])

  const skinsKey = items.map((x) => `${x.id}:${x.skin}:${x.model}`).join('|')

  // Single 3D scene holding a PlayerObject for each item, smoothly gliding between slots.
  useEffect(() => {
    if (n === 0) return
    let disposed = false
    let viewer: Viewer | undefined
    let animFrame = 0
    const cleanups: (() => void)[] = []

    void Promise.all([import('skinview3d'), import('three'), import('skinview-utils')]).then(
      async ([{ SkinViewer, PlayerObject }, { CanvasTexture, Mesh, NearestFilter, Raycaster, Vector2 }, { loadSkinToCanvas }]) => {
        if (disposed || !canvas.current || !stage.current) return
        const { width, height } = stage.current.getBoundingClientRect()
        viewer = new SkinViewer({
          canvas: canvas.current,
          width,
          height,
          zoom: 0.88,
          fov: 38,
          pixelRatio: Math.min(2, window.devicePixelRatio || 1),
        })
        // Allow vertical page scroll on touch devices instead of letting OrbitControls trap swipes.
        canvas.current.style.touchAction = 'pan-y'
        viewer.controls.enableZoom = false
        // Hide the default single playerObject; we manage all n PlayerObjects in playerWrapper.
        viewer.playerObject.visible = false

        const loadImage = (url: string) =>
          new Promise<HTMLImageElement>((resolve, reject) => {
            const img = new Image()
            img.onload = () => resolve(img)
            img.onerror = reject
            img.src = url
          })

        const applyOpacity = (p: PlayerObject, opacity: number) => {
          const isTransparent = opacity < 0.99
          p.traverse((o) => {
            if (o instanceof Mesh) {
              const isOuter = o.name === 'outer'
              for (const m of [o.material].flat()) {
                // Outer overlay (hat, jacket, sleeves) must stay transparent for alpha cutouts;
                // inner limbs keep depthWrite on so arms never bleed through the chest.
                m.transparent = isOuter || isTransparent
                m.opacity = opacity
                m.depthWrite = isOuter ? !isTransparent : true
              }
            }
          })
        }

        const players: {
          obj: PlayerObject
          cur: SlotTarget
          lastOpacity: number
        }[] = []

        for (let k = 0; k < n; k++) {
          const p = new PlayerObject()
          p.cape.visible = false
          const init = targetForOffset(slotOffset(k, indexRef.current, n))
          p.position.set(init.x, init.y, init.z)
          p.scale.setScalar(init.scale)
          p.rotation.y = init.rotY
          p.visible = init.opacity > 0.01
          applyOpacity(p, init.opacity)
          viewer.playerWrapper.add(p)
          players.push({ obj: p, cur: { ...init }, lastOpacity: init.opacity })
        }

        const loadedTextures: InstanceType<typeof CanvasTexture>[] = []
        cleanups.push(() => {
          loadedTextures.forEach((tex) => tex.dispose())
          for (const entry of players) {
            entry.obj.traverse((o) => {
              if (o instanceof Mesh) {
                o.geometry?.dispose?.()
                for (const m of [o.material].flat()) m?.dispose?.()
              }
            })
          }
        })
        await Promise.all(
          items.map(async (item, k) => {
            try {
              const img = await loadImage(item.skin)
              if (disposed) return
              const c = document.createElement('canvas')
              loadSkinToCanvas(c, img)
              const tex = new CanvasTexture(c)
              tex.magFilter = NearestFilter
              tex.minFilter = NearestFilter
              loadedTextures.push(tex)
              const p = players[k]!.obj
              p.skin.map = tex
              p.skin.modelType = item.model
              applyOpacity(p, players[k]!.cur.opacity)
              players[k]!.lastOpacity = players[k]!.cur.opacity
            } catch {
              // ignore broken texture
            }
          }),
        )
        if (disposed) return

        // Snap players cleanly to whichever index is active when textures finish loading.
        for (let k = 0; k < n; k++) {
          const init = targetForOffset(slotOffset(k, indexRef.current, n))
          players[k]!.cur = { ...init }
          const p = players[k]!.obj
          p.position.set(init.x, init.y, init.z)
          p.scale.setScalar(init.scale)
          p.rotation.y = init.rotY
          p.visible = init.opacity > 0.01
          applyOpacity(p, init.opacity)
          players[k]!.lastOpacity = init.opacity
        }

        // Animation loop: smoothly interpolates slot positions & runs walking motion on the active player.
        let lastTime = performance.now()
        let walkTime = 0
        let inView = true
        let isDragging = false
        let revealTimer = 0
        const tick = (now: number) => {
          animFrame = 0
          if (disposed || !inView || document.hidden) return
          const dt = Math.min(0.1, (now - lastTime) / 1000)
          lastTime = now
          if (playingRef.current) {
            walkTime += dt * WALK_SPEED
          }
          const damp = 1 - Math.exp(-10 * dt)
          const dampFade = isDragging ? 1 - Math.exp(-18 * dt) : damp
          const activeIdx = indexRef.current

          for (let k = 0; k < n; k++) {
            const state = players[k]!
            const p = state.obj
            const offset = slotOffset(k, activeIdx, n)
            const target = targetForOffset(offset)
            const targetOpacity = isDragging && offset !== 0 ? 0 : target.opacity
            state.cur.x += (target.x - state.cur.x) * damp
            state.cur.y += (target.y - state.cur.y) * damp
            state.cur.z += (target.z - state.cur.z) * damp
            state.cur.scale += (target.scale - state.cur.scale) * damp
            state.cur.rotY += (target.rotY - state.cur.rotY) * damp
            state.cur.opacity += (targetOpacity - state.cur.opacity) * dampFade

            p.position.set(state.cur.x, state.cur.y, state.cur.z)
            p.scale.setScalar(state.cur.scale)
            p.rotation.y = state.cur.rotY
            p.visible = state.cur.opacity > 0.02
            if (!p.visible) {
              state.lastOpacity = 0
            } else if (
              Math.abs(state.cur.opacity - state.lastOpacity) > 0.005 ||
              (targetOpacity >= 0.99 && state.lastOpacity < 0.99)
            ) {
              applyOpacity(p, state.cur.opacity)
              state.lastOpacity = state.cur.opacity
            }

            if (k === activeIdx) {
              // Walking animation on the centre character (matching skinview3d WalkingAnimation).
              const tWalk = walkTime * 8
              p.skin.leftLeg.rotation.x = Math.sin(tWalk) * 0.5
              p.skin.rightLeg.rotation.x = Math.sin(tWalk + Math.PI) * 0.5
              p.skin.leftArm.rotation.x = Math.sin(tWalk + Math.PI) * 0.5
              p.skin.rightArm.rotation.x = Math.sin(tWalk) * 0.5
              p.skin.leftArm.rotation.z = Math.cos(walkTime * 3) * 0.03 + 0.03
              p.skin.rightArm.rotation.z = Math.cos(walkTime * 3 + Math.PI) * 0.03 - 0.03
              p.skin.head.rotation.y = Math.sin(walkTime * 2) * 0.15
              p.skin.head.rotation.x = Math.sin(walkTime * 1.5) * 0.06
            } else {
              // Neighbours smoothly return their limbs to a calm standing pose.
              p.skin.leftLeg.rotation.x *= 1 - damp
              p.skin.rightLeg.rotation.x *= 1 - damp
              p.skin.leftArm.rotation.x *= 1 - damp
              p.skin.rightArm.rotation.x *= 1 - damp
              p.skin.leftArm.rotation.z *= 1 - damp
              p.skin.rightArm.rotation.z *= 1 - damp
              p.skin.head.rotation.y *= 1 - damp
              p.skin.head.rotation.x *= 1 - damp
            }
          }
          animFrame = requestAnimationFrame(tick)
        }
        const syncVisibility = () => {
          const active = inView && !document.hidden
          setVisible(active)
          if (viewer) viewer.renderPaused = !active
          if (active) {
            if (!animFrame && !disposed) {
              lastTime = performance.now()
              animFrame = requestAnimationFrame(tick)
            }
          } else if (animFrame) {
            cancelAnimationFrame(animFrame)
            animFrame = 0
          }
        }
        if (!document.hidden) animFrame = requestAnimationFrame(tick)

        // Dragging hides side candidates while orbiting; letting go returns camera home then fades them back in.
        const home = viewer.camera.position.clone()
        let raf = 0
        const onStart = () => {
          cancelAnimationFrame(raf)
          window.clearTimeout(revealTimer)
          isDragging = true
          setDragging(true)
        }
        const onEnd = () => {
          setDragging(false)
          window.clearTimeout(revealTimer)
          revealTimer = window.setTimeout(() => {
            isDragging = false
          }, Math.round(RETURN_MS * 0.65))
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
        viewer.controls.addEventListener('start', onStart)
        viewer.controls.addEventListener('end', onEnd)

        // Clicking a left or right neighbour character in the 3D scene selects it.
        let downPos: { x: number; y: number } | null = null
        const onPointerDown = (e: PointerEvent) => {
          if (e.button !== 0) return
          downPos = { x: e.clientX, y: e.clientY }
        }
        const onPointerUp = (e: PointerEvent) => {
          if (e.button !== 0 || !downPos || !viewer || !canvas.current) return
          const dist = Math.hypot(e.clientX - downPos.x, e.clientY - downPos.y)
          downPos = null
          if (dist > 6) return
          const rect = canvas.current.getBoundingClientRect()
          const pointer = new Vector2(((e.clientX - rect.left) / rect.width) * 2 - 1, -((e.clientY - rect.top) / rect.height) * 2 + 1)
          const raycaster = new Raycaster()
          raycaster.setFromCamera(pointer, viewer.camera)
          for (let k = 0; k < n; k++) {
            if (!players[k]!.obj.visible) continue
            if (raycaster.intersectObject(players[k]!.obj, true).length > 0) {
              if (k === indexRef.current) {
                detailLinkRef.current?.click()
              } else {
                setIndex(k)
              }
              break
            }
          }
        }
        const el = canvas.current
        el.addEventListener('pointerdown', onPointerDown)
        el.addEventListener('pointerup', onPointerUp)

        const resize = new ResizeObserver(([e]) => viewer?.setSize(e.contentRect.width, e.contentRect.height))
        resize.observe(stage.current)
        const seen = new IntersectionObserver(([e]) => {
          inView = e.isIntersecting
          syncVisibility()
        })
        seen.observe(stage.current)
        document.addEventListener('visibilitychange', syncVisibility)
        cleanups.push(
          () => cancelAnimationFrame(animFrame),
          () => cancelAnimationFrame(raf),
          () => window.clearTimeout(revealTimer),
          () => el.removeEventListener('pointerdown', onPointerDown),
          () => el.removeEventListener('pointerup', onPointerUp),
          () => resize.disconnect(),
          () => seen.disconnect(),
          () => document.removeEventListener('visibilitychange', syncVisibility),
        )
        setReady(true)
      },
    )
    return () => {
      disposed = true
      cleanups.forEach((f) => f())
      viewer?.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [n, skinsKey])

  if (n === 0) return null
  const current = items[safeIndex]!

  return (
    <section
      data-slot="skin-carousel"
      aria-roledescription="carousel"
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocusCapture={() => setHeld(true)}
      onBlurCapture={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setHeld(false)
      }}
      className={cn('sl-pixel-backdrop relative flex flex-col overflow-hidden rounded-2xl border p-4 sm:p-5', className)}
    >
      <div className="relative z-10 flex items-center gap-2 max-sm:hidden">
        {label && (
          <span className="inline-flex items-center gap-1.5 rounded-md border bg-card px-2.5 py-1 text-xs text-muted-foreground">
            {label}
            <span className="font-mono tabular-nums text-faint-foreground">{fill(t.position, { current: safeIndex + 1, total: n })}</span>
          </span>
        )}
        <span className="flex-1" />
        <Button
          variant="outline"
          size="icon-sm"
          onClick={() => setPlaying((p) => !p)}
          aria-label={playing ? t.pause : t.play}
          title={playing ? t.pause : t.play}
          aria-pressed={!playing}
        >
          {playing ? <Pause /> : <Play />}
        </Button>
        {n > 1 && (
          <>
            <Button variant="outline" size="icon-sm" onClick={() => go(safeIndex - 1)} aria-label={t.previous} title={t.previous}>
              <ChevronLeft />
            </Button>
            <Button variant="outline" size="icon-sm" onClick={() => go(safeIndex + 1)} aria-label={t.next} title={t.next}>
              <ChevronRight />
            </Button>
          </>
        )}
      </div>

      <div ref={stage} aria-live={running ? 'off' : 'polite'} className="relative min-h-40 flex-1 sm:min-h-56">
        {!ready && (
          <div className="absolute inset-0 flex items-end justify-center pb-4">
            <TexturePreview texture={{ id: current.id }} type={current.model === 'slim' ? 'alex' : 'steve'} height={200} className="max-h-full w-auto" />
          </div>
        )}
        <canvas
          ref={canvas}
          role="img"
          aria-label={current.name}
          className={cn('absolute inset-0 block size-full cursor-grab transition-opacity duration-300 ease-out active:cursor-grabbing', ready ? 'opacity-100' : 'opacity-0')}
        />
      </div>

      {/* Caption bar: clickable skin card link + clickable author filter chip. */}
      <div className="relative z-10 flex flex-col items-center gap-2 pt-2 pb-3 text-center max-sm:hidden">
        <div className="flex flex-wrap items-center justify-center gap-2">
          <ButtonLink
            ref={detailLinkRef}
            variant="secondary"
            size="sm"
            className="h-8 gap-1.5 rounded-lg border bg-card px-3 text-sm font-medium shadow-xs hover:bg-accent"
            render={<LinkAs href={current.href} />}
          >
            <span className="max-w-56 truncate">{current.name}</span>
            {current.modelLabel && <Badge variant="secondary">{current.modelLabel}</Badge>}
            <ArrowUpRight className="size-3.5 text-muted-foreground" aria-hidden />
          </ButtonLink>
        </div>
        <div className="flex flex-wrap items-center justify-center gap-2 text-caption text-muted-foreground">
          {current.uploader && (
            <ButtonLink
              variant="ghost"
              size="xs"
              className="h-6 gap-1.5 rounded-md px-2 text-caption text-muted-foreground hover:bg-card hover:text-foreground"
              render={<LinkAs href={`/skinlib?q=${encodeURIComponent(current.uploader.name)}`} />}
            >
              {current.uploader.userId !== undefined && (
                <UserAvatar of={{ user: current.uploader.userId, version: current.uploader.avatarVersion }} size={16} />
              )}
              <span>{current.uploader.name}</span>
            </ButtonLink>
          )}
          {current.likes !== undefined && (
            <span className="inline-flex items-center gap-1 rounded-md bg-card/70 px-2 py-0.5 tabular-nums">
              <Heart className="size-3.5 fill-primary text-primary" aria-hidden />
              <span className="sr-only">{t.likes}</span>
              {NUM_FMT.format(current.likes)}
            </span>
          )}
        </div>
      </div>

      {n > 1 && (
        <div className="relative z-10 flex justify-center gap-2.5 max-sm:hidden">
          {items.map((item, i) => (
            <button
              key={item.id}
              type="button"
              aria-current={i === safeIndex}
              aria-label={item.name}
              onClick={() => go(i)}
              className="group flex cursor-pointer flex-col items-center gap-1.5 rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            >
              <img
                src={avatarUrl({ texture: item.id }, 36)}
                alt=""
                width={36}
                height={36}
                className={cn(
                  'rounded-sm border bg-card transition-[opacity,border-color] duration-150 [image-rendering:pixelated]',
                  i === safeIndex ? 'border-2 border-primary' : 'opacity-55 group-hover:opacity-100',
                )}
              />
              <span className="h-[3px] w-9 overflow-hidden rounded-full bg-muted">
                {i === safeIndex && (
                  <span
                    key={`${safeIndex}-${running}`}
                    className="block h-full origin-left rounded-full bg-primary"
                    style={running ? { animation: `sl-carousel-progress ${interval}ms linear forwards` } : { transform: 'scaleX(0.35)' }}
                  />
                )}
              </span>
            </button>
          ))}
        </div>
      )}
      <div className="relative z-10 flex items-center justify-between gap-2 pt-2 sm:hidden">
        <ButtonLink
          variant="secondary"
          size="xs"
          className="h-8 pointer-coarse:h-10 min-w-0 max-w-[54%] gap-1 rounded-md border bg-card/90 px-2.5 text-xs font-medium shadow-xs"
          render={<LinkAs href={current.href} />}
        >
          <span className="truncate">{current.name}</span>
          <ArrowUpRight className="size-3 shrink-0 text-muted-foreground" aria-hidden />
        </ButtonLink>
        {n > 1 && (
          <div className="flex shrink-0 items-center gap-1">
            <Button
              variant="outline"
              size="icon-sm"
              onClick={() => go(safeIndex - 1)}
              aria-label={t.previous}
              title={t.previous}
              className="bg-card/90"
            >
              <ChevronLeft />
            </Button>
            <div className="flex items-center px-0.5">
              {items.map((item, i) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => go(i)}
                  aria-label={item.name}
                  aria-current={i === safeIndex}
                  className="flex h-8 pointer-coarse:h-10 cursor-pointer items-center px-1.5 pointer-coarse:px-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50 rounded-sm"
                >
                  <span
                    className={cn(
                      'h-1.5 rounded-full transition-[width,background-color] duration-200',
                      i === safeIndex ? 'w-4 bg-primary' : 'w-1.5 bg-input',
                    )}
                  />
                </button>
              ))}
            </div>
            <Button
              variant="outline"
              size="icon-sm"
              onClick={() => go(safeIndex + 1)}
              aria-label={t.next}
              title={t.next}
              className="bg-card/90"
            >
              <ChevronRight />
            </Button>
          </div>
        )}
      </div>
    </section>
  )
}
