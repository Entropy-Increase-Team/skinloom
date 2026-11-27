'use client'

import { Pause, Play, Sparkles, Upload } from 'lucide-react'
import type * as React from 'react'
import { useEffect, useRef, useState } from 'react'
import type { SkinViewer as Viewer } from 'skinview3d'

import { cn } from '../../lib/utils'
import { Button, ButtonLink } from '../ui/button'
import { useUiLabels } from './labels'
import { prefersReducedMotion } from './skin-viewer'

type PresetId = 'classic' | 'slim'

interface Preset {
  id: PresetId
  model: 'default' | 'slim'
  label: string
  modelLabel: string
}

/**
 * Generates a crisp, original 64x64 Minecraft skin texture on an offscreen canvas
 * inspired by SkinLoom's indigo-and-gold loom palette. Works completely offline.
 */
function buildDemoSkinCanvas(preset: PresetId): HTMLCanvasElement {
  const c = document.createElement('canvas')
  c.width = 64
  c.height = 64
  const ctx = c.getContext('2d')!
  ctx.imageSmoothingEnabled = false

  const fill = (x: number, y: number, w: number, h: number, color: string) => {
    ctx.fillStyle = color
    ctx.fillRect(x, y, w, h)
  }

  const isClassic = preset === 'classic'
  const skinBase = isClassic ? '#f1c29b' : '#f5ccb0'
  const skinShade = isClassic ? '#dfa980' : '#e3b393'
  const hair = isClassic ? '#27314a' : '#4a2f22'
  const hairLight = isClassic ? '#374569' : '#634130'
  const eye = isClassic ? '#3057a3' : '#b87d1a'
  const tunic = isClassic ? '#3057a3' : '#2a3856'
  const tunicDark = isClassic ? '#24437e' : '#1e2940'
  const accent = isClassic ? '#edb345' : '#f2c15a'
  const accentDark = '#c98e25'
  const pants = isClassic ? '#1f283b' : '#2e3b55'
  const boots = '#161b26'

  // Head base (0..31, 0..15)
  fill(0, 0, 32, 16, skinBase)
  fill(8, 0, 8, 8, hair)
  fill(9, 1, 6, 3, hairLight)
  fill(24, 8, 8, 8, hair)
  fill(0, 8, 8, 4, hair)
  fill(16, 8, 8, 4, hair)
  fill(8, 8, 8, 2, hair)
  fill(9, 10, 2, 1, hair)
  fill(14, 10, 2, 1, hair)
  fill(9, 12, 1, 1, '#ffffff')
  fill(10, 12, 1, 1, eye)
  fill(13, 12, 1, 1, eye)
  fill(14, 12, 1, 1, '#ffffff')
  fill(11, 14, 2, 1, skinShade)

  // Head overlay: woven headband
  fill(40, 9, 8, 1, accent)
  fill(32, 9, 8, 1, accent)
  fill(48, 9, 8, 1, accent)
  fill(56, 9, 8, 1, accent)
  fill(43, 9, 2, 1, accentDark)

  // Torso base
  fill(16, 16, 24, 16, tunic)
  fill(20, 20, 8, 1, tunicDark)
  fill(23, 20, 2, 2, skinBase)
  fill(20, 28, 8, 2, accent)
  fill(23, 28, 2, 2, accentDark)
  fill(20, 23, 8, 1, tunicDark)
  fill(32, 28, 8, 2, accent)
  fill(35, 22, 2, 5, accent)

  // Torso overlay: woven jacket lapel
  fill(20, 36, 2, 10, tunicDark)
  fill(26, 36, 2, 10, tunicDark)
  fill(21, 38, 1, 7, accent)
  fill(26, 38, 1, 7, accent)

  // Right & left arms
  const armW = isClassic ? 16 : 14
  fill(40, 16, armW, 16, tunic)
  fill(40, 27, armW, 5, skinBase)
  fill(40, 26, armW, 1, accent)
  fill(32, 48, armW, 16, tunic)
  fill(32, 59, armW, 5, skinBase)
  fill(32, 58, armW, 1, accent)

  // Right & left legs
  fill(0, 16, 16, 16, pants)
  fill(0, 28, 16, 4, boots)
  fill(16, 48, 16, 16, pants)
  fill(16, 60, 16, 4, boots)

  return c
}

const RETURN_MS = 420
const FACING = -0.48 // ~27.5° 3/4 portrait angle facing viewer's left
const TARGET_Y = 5.5 // upper-body framing: head, torso, and arms above the waist
const CAMERA_POS = [0, 14.5, 37] as const

export interface DemoSkinShowcaseProps {
  label: string
  hint: string
  classicLabel: string
  slimLabel: string
  classicModelLabel: string
  slimModelLabel: string
  uploadLabel: string
  uploadHref: string
  linkAs?: React.ElementType
  className?: string
}

/**
 * Interactive 3D fallback showcase rendered in the Hero split layout when the site has
 * no public skins yet. Renders two switchable procedural loom-themed skins offline.
 */
export function DemoSkinShowcase({
  label,
  hint,
  classicLabel,
  slimLabel,
  classicModelLabel,
  slimModelLabel,
  uploadLabel,
  uploadHref,
  linkAs: LinkAs = 'a',
  className,
}: DemoSkinShowcaseProps) {
  const t = useUiLabels()
  const presets: Preset[] = [
    { id: 'classic', model: 'default', label: classicLabel, modelLabel: classicModelLabel },
    { id: 'slim', model: 'slim', label: slimLabel, modelLabel: slimModelLabel },
  ]
  const [preset, setPreset] = useState<PresetId>('classic')
  const [playing, setPlaying] = useState(true)
  const [ready, setReady] = useState(false)
  const stageRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const viewerRef = useRef<Viewer | null>(null)
  const playingRef = useRef(true)

  playingRef.current = playing

  useEffect(() => {
    if (prefersReducedMotion()) {
      setPlaying(false)
      playingRef.current = false
    }
  }, [])

  useEffect(() => {
    let disposed = false
    let viewer: Viewer | undefined
    let raf = 0
    const cleanups: (() => void)[] = []

    void import('skinview3d').then(async ({ SkinViewer, WalkingAnimation }) => {
      if (disposed || !canvasRef.current || !stageRef.current) return
      const { width, height } = stageRef.current.getBoundingClientRect()
      viewer = new SkinViewer({
        canvas: canvasRef.current,
        width: Math.max(width, 200),
        height: Math.max(height, 200),
        zoom: 1,
        fov: 38,
        pixelRatio: Math.min(2, window.devicePixelRatio || 1),
      })
      canvasRef.current.style.touchAction = 'pan-y'
      viewer.controls.enableZoom = false
      viewer.controls.target.set(0, TARGET_Y, 0)
      viewer.camera.position.set(CAMERA_POS[0], CAMERA_POS[1], CAMERA_POS[2])
      viewer.camera.lookAt(0, TARGET_Y, 0)
      viewer.controls.update()
      viewer.playerObject.rotation.y = FACING

      const walk = new WalkingAnimation()
      walk.speed = 0.6
      walk.paused = !playingRef.current
      viewer.animation = walk

      await viewer.loadSkin(buildDemoSkinCanvas(preset), {
        model: preset === 'slim' ? 'slim' : 'default',
      })
      if (disposed) {
        viewer.dispose()
        return
      }
      viewerRef.current = viewer

      const home = viewer.camera.position.clone()
      const onStart = () => cancelAnimationFrame(raf)
      const onEnd = () => {
        const from = viewer!.camera.position.clone()
        const start = performance.now()
        const step = (now: number) => {
          const k = Math.min(1, (now - start) / RETURN_MS)
          const e = 1 - (1 - k) ** 3
          viewer!.camera.position.lerpVectors(from, home, e)
          viewer!.camera.lookAt(0, TARGET_Y, 0)
          if (k < 1) raf = requestAnimationFrame(step)
        }
        raf = requestAnimationFrame(step)
      }
      viewer.controls.addEventListener('start', onStart)
      viewer.controls.addEventListener('end', onEnd)

      const resize = new ResizeObserver(([entry]) => {
        if (entry) viewer?.setSize(entry.contentRect.width, entry.contentRect.height)
      })
      resize.observe(stageRef.current)

      const seen = new IntersectionObserver(([entry]) => {
        if (viewer && entry) viewer.renderPaused = !entry.isIntersecting
      })
      seen.observe(stageRef.current)

      cleanups.push(
        () => cancelAnimationFrame(raf),
        () => resize.disconnect(),
        () => seen.disconnect(),
      )
      setReady(true)
    })

    return () => {
      disposed = true
      viewerRef.current = null
      cleanups.forEach((fn) => fn())
      viewer?.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const viewer = viewerRef.current
    if (!viewer) return
    void viewer.loadSkin(buildDemoSkinCanvas(preset), {
      model: preset === 'slim' ? 'slim' : 'default',
    })
  }, [preset])

  useEffect(() => {
    const anim = viewerRef.current?.animation
    if (anim) anim.paused = !playing
  }, [playing])

  return (
    <section
      data-slot="demo-skin-showcase"
      className={cn(
        'sl-pixel-backdrop relative flex flex-col overflow-hidden rounded-2xl border bg-card/60 p-4 shadow-xs sm:p-5',
        className,
      )}
    >
      <div className="relative z-10 flex items-center gap-2">
        <span className="inline-flex items-center gap-1.5 rounded-full border bg-card/90 px-2.5 py-1 text-xs font-medium text-muted-foreground shadow-xs">
          <Sparkles className="size-3.5 text-highlight" aria-hidden />
          {label}
        </span>
        <span className="flex-1" />
        <Button
          variant="outline"
          size="icon-sm"
          className="bg-card/90"
          onClick={() => setPlaying((p) => !p)}
          aria-label={playing ? t.pause : t.play}
          title={playing ? t.pause : t.play}
          aria-pressed={!playing}
        >
          {playing ? <Pause /> : <Play />}
        </Button>
      </div>

      <div ref={stageRef} className="relative min-h-44 flex-1 sm:min-h-60">
        <canvas
          ref={canvasRef}
          role="img"
          aria-label={label}
          className={cn(
            'absolute inset-0 block size-full cursor-grab transition-opacity duration-300 ease-out active:cursor-grabbing',
            ready ? 'opacity-100' : 'opacity-0',
          )}
        />
      </div>

      <div className="relative z-10 flex flex-col items-center gap-2.5 pt-2 text-center">
        <div className="inline-flex items-center gap-1 rounded-lg border bg-card/90 p-1 shadow-xs">
          {presets.map((p) => {
            const active = preset === p.id
            return (
              <button
                key={p.id}
                type="button"
                onClick={() => setPreset(p.id)}
                aria-pressed={active}
                className={cn(
                  'inline-flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
                  active
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                <span>{p.label}</span>
                <span className={cn('text-[0.6875rem]', active ? 'text-primary-foreground/80' : 'text-faint-foreground')}>
                  {p.modelLabel}
                </span>
              </button>
            )
          })}
        </div>
        <div className="flex flex-wrap items-center justify-center gap-2 text-xs text-muted-foreground">
          <span>{hint}</span>
          <ButtonLink
            variant="secondary"
            size="xs"
            className="h-6 gap-1 rounded-md border bg-card px-2 text-xs font-medium hover:bg-accent"
            render={<LinkAs href={uploadHref} />}
          >
            <Upload className="size-3" aria-hidden />
            {uploadLabel}
          </ButtonLink>
        </div>
      </div>
    </section>
  )
}
