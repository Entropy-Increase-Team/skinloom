export type SkinModel = 'steve' | 'alex'

/** A rectangle copied from the texture (s*) to the figure (d*), in skin pixels. */
export interface Part {
  sx: number
  sy: number
  w: number
  h: number
  dx: number
  dy: number
}

/**
 * The front faces of a 64×64 skin laid out as a 16×32 figure seen from the front:
 * base layer first, then the overlay (hat, jacket, sleeves, trousers). The player's
 * right arm and leg appear on the viewer's left; slim (Alex) arms are 3 pixels wide.
 */
export function frontParts(model: SkinModel): Part[] {
  const a = model === 'alex' ? 3 : 4
  const layout = [
    { base: [8, 8], over: [40, 8], w: 8, h: 8, dx: 4, dy: 0 }, // head
    { base: [20, 20], over: [20, 36], w: 8, h: 12, dx: 4, dy: 8 }, // body
    { base: [44, 20], over: [44, 36], w: a, h: 12, dx: 4 - a, dy: 8 }, // right arm
    { base: [36, 52], over: [52, 52], w: a, h: 12, dx: 12, dy: 8 }, // left arm
    { base: [4, 20], over: [4, 36], w: 4, h: 12, dx: 4, dy: 20 }, // right leg
    { base: [20, 52], over: [4, 52], w: 4, h: 12, dx: 8, dy: 20 }, // left leg
  ]
  const part = (l: (typeof layout)[number], [sx, sy]: number[]) => ({ sx, sy, w: l.w, h: l.h, dx: l.dx, dy: l.dy })
  return [...layout.map((l) => part(l, l.base)), ...layout.map((l) => part(l, l.over))]
}

/** The outer face of a cape (the side seen from behind), in a 64×32 cape texture. */
export const capeFront: Part = { sx: 1, sy: 1, w: 10, h: 16, dx: 0, dy: 0 }

/** Figure sizes in skin pixels. */
export const FIGURE = { skin: { w: 16, h: 32 }, cape: { w: 10, h: 16 } } as const

/**
 * Draws parts from a texture whose width is `textureWidth` (64 × its resolution) onto
 * ctx, `scale` canvas pixels per skin pixel, without smoothing.
 */
export function drawParts(
  ctx: CanvasRenderingContext2D,
  texture: CanvasImageSource,
  textureWidth: number,
  parts: Part[],
  scale: number,
) {
  const u = textureWidth / 64
  ctx.imageSmoothingEnabled = false
  for (const p of parts) {
    ctx.drawImage(texture, p.sx * u, p.sy * u, p.w * u, p.h * u, p.dx * scale, p.dy * scale, p.w * scale, p.h * scale)
  }
}

/**
 * Whether a 2:1 texture looks like a cape rather than a legacy 64×32 skin: a cape
 * leaves everything right of its 22-pixel-wide pattern transparent, a skin does not.
 * alpha(x, y) reads the alpha channel in texture pixels.
 */
export function looksLikeCape(width: number, height: number, alpha: (x: number, y: number) => number): boolean {
  if (width !== 2 * height) return false
  const u = width / 64
  for (let y = 0; y < height; y++) {
    for (let x = Math.ceil(24 * u); x < width; x++) {
      if (alpha(x, y) !== 0) return false
    }
  }
  return true
}

/** KB as Blessing Skin and the server count them: every started KB. */
export function kb(bytes: number): number {
  return Math.ceil(bytes / 1024)
}

/** The score an upload of `bytes` costs (mirrors score.Rates.UploadCost on the server). */
export function uploadCost(
  bytes: number,
  isPublic: boolean,
  r: { perStorage: number; privatePerStorage: number; perClosetItem: number; awardPerTexture: number },
): number {
  return isPublic ? kb(bytes) * r.perStorage + r.perClosetItem - r.awardPerTexture : kb(bytes) * r.privatePerStorage + r.perClosetItem
}
