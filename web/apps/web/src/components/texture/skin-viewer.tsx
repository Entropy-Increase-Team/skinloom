'use client'

import { useEffect, useRef, useState } from 'react'
import type { SkinViewer } from 'skinview3d'

/**
 * A rotating 3D preview (skinview3d). Loaded in the browser only; a cape is shown on
 * its own, from behind. The canvas is transparent: the theme's viewer.background token
 * (--sl-viewer-background) is the backdrop.
 */
export function SkinViewer3D({ hash, type, label }: { hash: string; type: 'steve' | 'alex' | 'cape'; label: string }) {
  const box = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let viewer: SkinViewer | undefined
    let observer: ResizeObserver | undefined
    let disposed = false
    import('skinview3d')
      .then(({ SkinViewer, WalkingAnimation }) => {
        if (disposed || !canvas.current || !box.current) return
        const { width, height } = box.current.getBoundingClientRect()
        viewer = new SkinViewer({ canvas: canvas.current, width, height, zoom: 0.8 })
        const url = `/textures/${hash}`
        if (type === 'cape') {
          viewer.playerObject.skin.visible = false
          viewer.playerObject.rotation.y = Math.PI
          void viewer.loadCape(url)
        } else {
          void viewer.loadSkin(url, { model: type === 'alex' ? 'slim' : 'default' })
        }
        const walk = new WalkingAnimation()
        walk.speed = 0.6
        viewer.animation = walk
        viewer.autoRotate = true
        viewer.autoRotateSpeed = 0.6
        observer = new ResizeObserver(([entry]) => {
          viewer?.setSize(entry.contentRect.width, entry.contentRect.height)
        })
        observer.observe(box.current)
      })
      .catch(() => setFailed(true))
    return () => {
      disposed = true
      observer?.disconnect()
      viewer?.dispose()
    }
  }, [hash, type])

  return (
    <div ref={box} className="relative h-full w-full bg-(--sl-viewer-background,transparent)" role="img" aria-label={label}>
      {failed ? null : <canvas ref={canvas} className="block h-full w-full cursor-grab active:cursor-grabbing" />}
    </div>
  )
}
