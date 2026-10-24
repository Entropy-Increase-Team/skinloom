import { describe, expect, it } from 'vitest'

import { FIGURE, frontParts, looksLikeCape, uploadCost } from './skin'

describe('frontParts', () => {
  it('lays a classic skin out on a 16×32 figure without overlap in the base layer', () => {
    const base = frontParts('steve').slice(0, 6)
    const covered = new Set<string>()
    for (const p of base) {
      for (let y = p.dy; y < p.dy + p.h; y++) {
        for (let x = p.dx; x < p.dx + p.w; x++) {
          expect(covered.has(`${x},${y}`)).toBe(false)
          expect(x).toBeLessThan(FIGURE.skin.w)
          expect(y).toBeLessThan(FIGURE.skin.h)
          covered.add(`${x},${y}`)
        }
      }
    }
    expect(covered.size).toBe(8 * 8 + 8 * 12 + 2 * 4 * 12 + 2 * 4 * 12)
  })

  it('makes slim arms 3 pixels wide, against the body', () => {
    const [, body, right, left] = frontParts('alex')
    expect(right.w).toBe(3)
    expect(right.dx + right.w).toBe(body.dx)
    expect(left.dx).toBe(body.dx + body.w)
  })

  it('draws every overlay over its base part', () => {
    const parts = frontParts('steve')
    for (let i = 0; i < 6; i++) {
      expect({ ...parts[i + 6], sx: 0, sy: 0 }).toEqual({ ...parts[i], sx: 0, sy: 0 })
    }
  })
})

describe('looksLikeCape', () => {
  it('tells capes from legacy skins by the empty right side', () => {
    const cape = (x: number) => (x < 22 ? 255 : 0)
    const skin = (x: number) => (x >= 40 && x < 56 ? 255 : 0) // arms and hat area
    expect(looksLikeCape(64, 32, cape)).toBe(true)
    expect(looksLikeCape(64, 32, skin)).toBe(false)
    expect(looksLikeCape(64, 64, cape)).toBe(false)
    expect(looksLikeCape(128, 64, (x) => (x < 44 ? 255 : 0))).toBe(true)
  })
})

describe('uploadCost', () => {
  const rates = { perStorage: 1, privatePerStorage: 10, perClosetItem: 2, awardPerTexture: 5 }
  it('charges every started KB and awards only public uploads', () => {
    expect(uploadCost(1, true, rates)).toBe(1 + 2 - 5)
    expect(uploadCost(1025, false, rates)).toBe(20 + 2)
  })
})
