'use client'

import { Feather, Footprints, Image as ImageIcon, Pause, Play, Rotate3d, RotateCcw, RotateCw, User, Zap } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import type { SkinViewer as Viewer } from 'skinview3d'

import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { useUiLabels } from './labels'

/** The visitor asked for less motion (web/design.md §6). */
export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

export type SkinViewerAnimation = 'walk' | 'run' | 'fly' | 'idle' | 'none'
export type SkinViewerBackground = 'transparent' | 'white' | 'gray' | 'black'

type ActiveAnimation = Exclude<SkinViewerAnimation, 'none'>

interface ViewerLibs {
  WalkingAnimation: typeof import('skinview3d').WalkingAnimation
  RunningAnimation: typeof import('skinview3d').RunningAnimation
  FlyingAnimation: typeof import('skinview3d').FlyingAnimation
  IdleAnimation: typeof import('skinview3d').IdleAnimation
}

const BG_HEX: Record<Exclude<SkinViewerBackground, 'transparent'>, string> = {
  white: '#ffffff',
  gray: '#6c757d',
  black: '#000000',
}

function createAnimation(libs: ViewerLibs, mode: ActiveAnimation) {
  const anim =
    mode === 'run'
      ? new libs.RunningAnimation()
      : mode === 'fly'
        ? new libs.FlyingAnimation()
        : mode === 'idle'
          ? new libs.IdleAnimation()
          : new libs.WalkingAnimation()
  anim.speed = mode === 'run' ? 0.75 : 0.6
  return anim
}

function drawDualFigure2d(canvas: HTMLCanvasElement, source: HTMLImageElement, isCape: boolean, model: 'default' | 'slim' | 'auto-detect', loadSkinToCanvas: typeof import('skinview-utils').loadSkinToCanvas, inferModelType: typeof import('skinview-utils').inferModelType) {
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const scale = 8
  if (isCape) {
    canvas.width = 10 * scale
    canvas.height = 16 * scale
    const u = source.naturalWidth / 64
    ctx.clearRect(0, 0, canvas.width, canvas.height)
    ctx.imageSmoothingEnabled = false
    ctx.drawImage(source, 1 * u, 1 * u, 10 * u, 16 * u, 0, 0, 10 * scale, 16 * scale)
    return
  }
  const normalized = document.createElement('canvas')
  loadSkinToCanvas(normalized, source)
  const slim = model === 'slim' || (model === 'auto-detect' && inferModelType(normalized) === 'slim')
  const a = slim ? 3 : 4
  const u = normalized.width / 64
  canvas.width = 36 * scale
  canvas.height = 32 * scale
  ctx.clearRect(0, 0, canvas.width, canvas.height)
  ctx.imageSmoothingEnabled = false

  const drawFigure = (ox: number, parts: readonly { sx: number; sy: number; w: number; h: number; dx: number; dy: number }[]) => {
    for (const p of parts) {
      ctx.drawImage(normalized, p.sx * u, p.sy * u, p.w * u, p.h * u, (ox + p.dx) * scale, p.dy * scale, p.w * scale, p.h * scale)
    }
  }
  const front = [
    // base
    { sx: 8, sy: 8, w: 8, h: 8, dx: 4, dy: 0 },
    { sx: 20, sy: 20, w: 8, h: 12, dx: 4, dy: 8 },
    { sx: 44, sy: 20, w: a, h: 12, dx: 4 - a, dy: 8 },
    { sx: 36, sy: 52, w: a, h: 12, dx: 12, dy: 8 },
    { sx: 4, sy: 20, w: 4, h: 12, dx: 4, dy: 20 },
    { sx: 20, sy: 52, w: 4, h: 12, dx: 8, dy: 20 },
    // overlay
    { sx: 40, sy: 8, w: 8, h: 8, dx: 4, dy: 0 },
    { sx: 20, sy: 36, w: 8, h: 12, dx: 4, dy: 8 },
    { sx: 44, sy: 36, w: a, h: 12, dx: 4 - a, dy: 8 },
    { sx: 52, sy: 52, w: a, h: 12, dx: 12, dy: 8 },
    { sx: 4, sy: 36, w: 4, h: 12, dx: 4, dy: 20 },
    { sx: 4, sy: 52, w: 4, h: 12, dx: 8, dy: 20 },
  ]
  const back = [
    // base
    { sx: 24, sy: 8, w: 8, h: 8, dx: 4, dy: 0 },
    { sx: 32, sy: 20, w: 8, h: 12, dx: 4, dy: 8 },
    { sx: 48 + a, sy: 20, w: a, h: 12, dx: 12, dy: 8 },
    { sx: 40 + a, sy: 52, w: a, h: 12, dx: 4 - a, dy: 8 },
    { sx: 12, sy: 20, w: 4, h: 12, dx: 8, dy: 20 },
    { sx: 28, sy: 52, w: 4, h: 12, dx: 4, dy: 20 },
    // overlay
    { sx: 56, sy: 8, w: 8, h: 8, dx: 4, dy: 0 },
    { sx: 32, sy: 36, w: 8, h: 12, dx: 4, dy: 8 },
    { sx: 48 + a, sy: 36, w: a, h: 12, dx: 12, dy: 8 },
    { sx: 56 + a, sy: 52, w: a, h: 12, dx: 4 - a, dy: 8 },
    { sx: 12, sy: 36, w: 4, h: 12, dx: 8, dy: 20 },
    { sx: 12, sy: 52, w: 4, h: 12, dx: 4, dy: 20 },
  ]
  drawFigure(0, front)
  drawFigure(20, back)
}

function resetPlayerPose(viewer: Viewer, backEquip: 'cape' | 'elytra', skin: string | null, cape: string | null) {
  viewer.playerObject.position.set(0, 0, 0)
  viewer.playerObject.rotation.set(0, !skin && cape ? Math.PI : 0, 0)
  viewer.playerObject.resetJoints()
  viewer.playerObject.backEquipment = cape ? backEquip : null
}

/**
 * Interactive skin & cape viewer (web/design.md §8.1, §10.3): lazy-loads skinview3d in
 * the browser, supports 3D/2D mode switching, walk/run/fly/idle animations, pause/resume,
 * auto-rotate, view reset, and transparent/solid background switching.
 */
export function SkinViewer({
  skin,
  cape = null,
  previewSrc,
  model = 'auto-detect',
  animation = 'walk',
  autoRotate = false,
  controls = true,
  label,
  className,
}: {
  /** URL of the skin texture (/textures/{hash}); null shows only the cape. */
  skin: string | null
  cape?: string | null
  /** Server-rendered 2D front+back (or cape) preview URL shown in 2D mode without exposing the raw texture file. */
  previewSrc?: string
  model?: 'default' | 'slim' | 'auto-detect'
  animation?: SkinViewerAnimation
  autoRotate?: boolean
  /** Show the interactive 3D/2D, animation, playback, reset, and background controls. */
  controls?: boolean
  /** Describes what is shown, for screen readers. */
  label: string
  className?: string
}) {
  const t = useUiLabels()
  const box = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const canvas2d = useRef<HTMLCanvasElement>(null)
  const viewerRef = useRef<Viewer | null>(null)
  const libsRef = useRef<ViewerLibs | null>(null)
  const visibleRef = useRef(true)
  const initializedAnimRef = useRef<ActiveAnimation | null>(null)

  const initialAnim: ActiveAnimation = animation === 'none' ? 'idle' : animation
  const [state, setState] = useState<'loading' | 'ready' | 'failed'>('loading')
  const [viewMode, setViewMode] = useState<'3d' | '2d'>('3d')
  const [animMode, setAnimMode] = useState<ActiveAnimation>(initialAnim)
  const [paused, setPaused] = useState<boolean>(animation === 'none')
  const [rotating, setRotating] = useState<boolean>(autoRotate)
  const [bg, setBg] = useState<SkinViewerBackground>('transparent')
  const [backEquip, setBackEquip] = useState<'cape' | 'elytra'>('cape')

  const animRef = useRef(animMode)
  const pausedRef = useRef(paused)
  const rotatingRef = useRef(rotating)
  const bgRef = useRef(bg)
  const backEquipRef = useRef(backEquip)
  const viewModeRef = useRef(viewMode)
  animRef.current = animMode
  pausedRef.current = paused
  rotatingRef.current = rotating
  bgRef.current = bg
  backEquipRef.current = backEquip
  viewModeRef.current = viewMode

  // Apply user's reduced-motion preference on mount (without causing SSR hydration mismatch).
  useEffect(() => {
    if (prefersReducedMotion()) {
      setPaused(true)
      setRotating(false)
    }
  }, [])

  // Reset initial defaults if the initial props change.
  useEffect(() => {
    const still = prefersReducedMotion()
    setAnimMode(animation === 'none' ? 'idle' : animation)
    setPaused(animation === 'none' || still)
    setRotating(autoRotate && !still)
    setBackEquip('cape')
  }, [animation, autoRotate])

  useEffect(() => {
    let viewer: Viewer | undefined
    let disposed = false
    const cleanups: (() => void)[] = []
    setState('loading')

    void import('skinview3d')
      .then(async ({ SkinViewer: ViewerClass, WalkingAnimation, RunningAnimation, FlyingAnimation, IdleAnimation }) => {
        if (disposed || !canvas.current || !box.current) return
        const libs: ViewerLibs = { WalkingAnimation, RunningAnimation, FlyingAnimation, IdleAnimation }
        libsRef.current = libs

        const { width, height } = box.current.getBoundingClientRect()
        viewer = new ViewerClass({
          canvas: canvas.current,
          width,
          height,
          zoom: 0.8,
          pixelRatio: Math.min(2, window.devicePixelRatio || 1),
        })
        // Allow vertical page scroll on touch devices instead of letting OrbitControls trap swipes.
        canvas.current.style.touchAction = 'pan-y'
        if (skin) await viewer.loadSkin(skin, { model })
        else viewer.playerObject.skin.visible = false
        if (cape) {
          await viewer.loadCape(cape, { backEquipment: backEquipRef.current })
          if (!skin) viewer.playerObject.rotation.y = Math.PI
        }
        if (disposed) {
          viewer.dispose()
          return
        }

        viewerRef.current = viewer
        viewer.autoRotateSpeed = 0.6
        viewer.background = bgRef.current === 'transparent' ? null : BG_HEX[bgRef.current]
        resetPlayerPose(viewer, backEquipRef.current, skin, cape)
        const a = createAnimation(libs, animRef.current)
        a.paused = pausedRef.current
        viewer.animation = a
        initializedAnimRef.current = animRef.current
        viewer.autoRotate = rotatingRef.current && !pausedRef.current

        const syncPaused = () => {
          if (viewer) viewer.renderPaused = !visibleRef.current || document.hidden || viewModeRef.current === '2d'
        }
        const resize = new ResizeObserver(([e]) => viewer?.setSize(e.contentRect.width, e.contentRect.height))
        resize.observe(box.current)
        const seen = new IntersectionObserver(([e]) => {
          visibleRef.current = e.isIntersecting
          syncPaused()
        })
        seen.observe(box.current)
        document.addEventListener('visibilitychange', syncPaused)
        cleanups.push(
          () => resize.disconnect(),
          () => seen.disconnect(),
          () => document.removeEventListener('visibilitychange', syncPaused),
        )
        setState('ready')
      })
      .catch(() => {
        if (viewer) {
          viewer.dispose()
          viewer = undefined
        }
        viewerRef.current = null
        if (!disposed) setState('failed')
      })

    return () => {
      disposed = true
      viewerRef.current = null
      initializedAnimRef.current = null
      cleanups.forEach((f) => f())
      viewer?.dispose()
    }
  }, [skin, cape, model])

  // Sync animation type (and clear leftovers such as Running bounce or Flying pitch).
  useEffect(() => {
    const viewer = viewerRef.current
    const libs = libsRef.current
    if (!viewer || !libs || state !== 'ready') return
    if (initializedAnimRef.current === animMode) {
      initializedAnimRef.current = null
      return
    }
    initializedAnimRef.current = null
    resetPlayerPose(viewer, backEquipRef.current, skin, cape)
    const a = createAnimation(libs, animMode)
    a.paused = pausedRef.current
    viewer.animation = a
  }, [animMode, state, skin, cape])

  // Pause/resume without resetting animation progress.
  useEffect(() => {
    const viewer = viewerRef.current
    if (!viewer || state !== 'ready') return
    if (viewer.animation) viewer.animation.paused = paused
    viewer.autoRotate = rotating && !paused
  }, [paused, rotating, state])

  // Sync WebGL scene background.
  useEffect(() => {
    const viewer = viewerRef.current
    if (!viewer || state !== 'ready') return
    viewer.background = bg === 'transparent' ? null : BG_HEX[bg]
  }, [bg, state])

  // Sync cape vs elytra.
  useEffect(() => {
    const viewer = viewerRef.current
    if (!viewer || state !== 'ready' || !cape) return
    viewer.playerObject.backEquipment = backEquip
  }, [backEquip, cape, state])

  // Pause WebGL render loop while 2D view is shown (preserving pose when switching back).
  useEffect(() => {
    const viewer = viewerRef.current
    if (!viewer || state !== 'ready') return
    viewer.renderPaused = !visibleRef.current || document.hidden || viewMode === '2d'
  }, [viewMode, state])

  const flatTexture = skin ?? cape

  const show2d = viewMode === '2d' || state === 'failed'

  // Fallback client-side front+back 2D preview canvas when no server previewSrc is provided (e.g. upload preview).
  useEffect(() => {
    if (!show2d || previewSrc || !flatTexture) return
    let cancelled = false
    void import('skinview-utils').then(({ loadSkinToCanvas, inferModelType }) => {
      if (cancelled || !canvas2d.current) return
      const img = new Image()
      img.onload = () => {
        if (!cancelled && canvas2d.current) {
          drawDualFigure2d(canvas2d.current, img, !skin && Boolean(cape), model, loadSkinToCanvas, inferModelType)
        }
      }
      img.src = flatTexture
    })
    return () => {
      cancelled = true
    }
  }, [show2d, previewSrc, flatTexture, skin, cape, model])

  const resetState = useCallback(() => {
    const still = prefersReducedMotion()
    const nextPaused = animation === 'none' || still
    const nextRotating = autoRotate && !still
    setViewMode('3d')
    setAnimMode(initialAnim)
    setPaused(nextPaused)
    setRotating(nextRotating)
    setBg('transparent')
    setBackEquip('cape')

    const viewer = viewerRef.current
    const libs = libsRef.current
    if (viewer && libs) {
      viewer.resetCameraPose()
      viewer.controls.target.set(0, 0, 0)
      viewer.controls.update()
      viewer.playerWrapper.rotation.set(0, 0, 0)
      viewer.background = null
      resetPlayerPose(viewer, 'cape', skin, cape)
      const a = createAnimation(libs, initialAnim)
      a.paused = nextPaused
      viewer.animation = a
      viewer.autoRotate = nextRotating && !nextPaused
      viewer.renderPaused = !visibleRef.current
    }
  }, [animation, autoRotate, initialAnim, skin, cape])

  const animOptions: { id: ActiveAnimation; label: string; icon: React.ReactNode }[] = [
    { id: 'walk', label: t.walk, icon: <Footprints aria-hidden /> },
    { id: 'run', label: t.run, icon: <Zap aria-hidden /> },
    { id: 'fly', label: t.fly, icon: <Feather aria-hidden /> },
    { id: 'idle', label: t.idle, icon: <User aria-hidden /> },
  ]
  const bgOptions: { id: SkinViewerBackground; label: string }[] = [
    { id: 'transparent', label: t.bgTransparent },
    { id: 'white', label: t.bgWhite },
    { id: 'gray', label: t.bgGray },
    { id: 'black', label: t.bgBlack },
  ]

  return (
    <div
      ref={box}
      data-slot="skin-viewer"
      data-state={state}
      data-view={show2d ? '2d' : '3d'}
      style={bg === 'transparent' ? undefined : { backgroundColor: BG_HEX[bg] }}
      className={cn(
        'sl-viewer relative h-full w-full overflow-hidden rounded-[inherit]',
        bg === 'transparent' && 'bg-(--sl-viewer-background)',
        className,
      )}
    >
      {state !== 'failed' && (
        <canvas
          ref={canvas}
          role="img"
          aria-label={label}
          aria-hidden={viewMode === '2d' || undefined}
          className={cn(
            'block h-full w-full cursor-grab transition-opacity duration-200 ease-out active:cursor-grabbing',
            state === 'ready' && viewMode === '3d' ? 'opacity-100' : 'pointer-events-none opacity-0',
          )}
        />
      )}

      {show2d && (previewSrc || flatTexture) && (
        <div className="absolute inset-0 flex items-center justify-center px-6 pt-16 pb-14 sm:px-12">
          {previewSrc ? (
            // eslint-disable-next-line @next/next/no-img-element -- server-rendered front+back preview (never exposes raw skin PNG)
            <img
              src={previewSrc}
              alt={label}
              draggable={false}
              className="size-full object-contain select-none [image-rendering:pixelated]"
            />
          ) : (
            <canvas
              ref={canvas2d}
              role="img"
              aria-label={label}
              className="size-full object-contain select-none [image-rendering:pixelated]"
            />
          )}
        </div>
      )}

      {state === 'failed' && !previewSrc && !flatTexture && (
        <div className="absolute inset-0 flex items-center justify-center p-6 text-center text-sm text-muted-foreground">
          {t.previewUnavailable}
        </div>
      )}

      {controls && (
        <>
          <div className="pointer-events-none absolute inset-x-3 top-3 z-10 flex flex-wrap items-center justify-between gap-2">
            <div className="pointer-events-auto inline-flex items-center gap-0.5 rounded-lg border bg-card/90 p-0.5 shadow-xs backdrop-blur-xs">
              <Button
                type="button"
                size="xs"
                className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                variant={!show2d ? 'secondary' : 'ghost'}
                aria-pressed={!show2d}
                disabled={state === 'failed'}
                onClick={() => setViewMode('3d')}
              >
                <Rotate3d aria-hidden />
                <span>{t.mode3d}</span>
              </Button>
              <Button
                type="button"
                size="xs"
                className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                variant={show2d ? 'secondary' : 'ghost'}
                aria-pressed={show2d}
                onClick={() => setViewMode('2d')}
              >
                <ImageIcon aria-hidden />
                <span>{t.mode2d}</span>
              </Button>
            </div>

            <div className="pointer-events-auto flex flex-wrap items-center gap-1.5">
              {!show2d && (
                <div className="inline-flex items-center gap-0.5 rounded-lg border bg-card/90 p-0.5 shadow-xs backdrop-blur-xs">
                  {animOptions.map((opt) => (
                    <Button
                      key={opt.id}
                      type="button"
                      size="xs"
                      className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                      variant={animMode === opt.id ? 'secondary' : 'ghost'}
                      aria-pressed={animMode === opt.id}
                      aria-label={opt.label}
                      title={opt.label}
                      onClick={() => {
                        setAnimMode(opt.id)
                        setPaused(false)
                      }}
                    >
                      {opt.icon}
                      <span className="max-sm:hidden">{opt.label}</span>
                    </Button>
                  ))}
                </div>
              )}

              <div className="inline-flex items-center gap-0.5 rounded-lg border bg-card/90 p-0.5 shadow-xs backdrop-blur-xs">
                {!show2d && (
                  <>
                    <Button
                      type="button"
                      size="icon-xs"
                      className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                      variant="ghost"
                      aria-pressed={paused}
                      aria-label={paused ? t.play : t.pause}
                      title={paused ? t.play : t.pause}
                      onClick={() => setPaused((p) => !p)}
                    >
                      {paused ? <Play /> : <Pause />}
                    </Button>
                    <Button
                      type="button"
                      size="icon-xs"
                      className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                      variant={rotating ? 'secondary' : 'ghost'}
                      aria-pressed={rotating}
                      aria-label={t.autoRotate}
                      title={t.autoRotate}
                      onClick={() => {
                        setRotating((r) => !r)
                        setPaused(false)
                      }}
                    >
                      <RotateCw />
                    </Button>
                    {cape && (
                      <Button
                        type="button"
                        size="icon-xs"
                        className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                        variant={backEquip === 'elytra' ? 'secondary' : 'ghost'}
                        aria-pressed={backEquip === 'elytra'}
                        aria-label={t.backEquipment}
                        title={t.backEquipment}
                        onClick={() => setBackEquip((b) => (b === 'cape' ? 'elytra' : 'cape'))}
                      >
                        <Feather />
                      </Button>
                    )}
                  </>
                )}
                <Button
                  type="button"
                  size="icon-xs"
                  className="relative pointer-coarse:after:absolute pointer-coarse:after:-inset-y-2 pointer-coarse:after:inset-x-0"
                  variant="ghost"
                  aria-label={t.resetView}
                  title={t.resetView}
                  onClick={resetState}
                >
                  <RotateCcw />
                </Button>
              </div>
            </div>
          </div>

          <div className="pointer-events-none absolute inset-x-3 bottom-3 z-10 flex justify-center sm:justify-start">
            <div
              role="group"
              aria-label={t.background}
              className="pointer-events-auto inline-flex items-center gap-1 rounded-full border bg-card/90 px-2 py-1 shadow-xs backdrop-blur-xs pointer-coarse:gap-2"
            >
              {bgOptions.map((opt) => (
                <button
                  key={opt.id}
                  type="button"
                  aria-pressed={bg === opt.id}
                  aria-label={opt.label}
                  title={opt.label}
                  onClick={() => setBg(opt.id)}
                  className={cn(
                    'relative flex size-6 cursor-pointer items-center justify-center rounded-full transition-transform hover:scale-105 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring after:absolute after:-inset-2 after:content-[""] pointer-coarse:after:-inset-2.5',
                    bg === opt.id && 'ring-2 ring-primary ring-offset-1 ring-offset-card',
                  )}
                >
                  <span
                    aria-hidden
                    className={cn('block size-4 rounded-full border border-border', opt.id === 'transparent' && 'sl-pixel-backdrop')}
                    style={opt.id === 'transparent' ? undefined : { backgroundColor: BG_HEX[opt.id] }}
                  />
                </button>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}
