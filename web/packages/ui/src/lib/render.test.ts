import { describe, expect, it } from 'vitest'

import { avatarUrl, previewUrl, previewWidth, snapUp } from './render'

describe('render URLs', () => {
  it('asks for a listed size at twice the displayed one', () => {
    expect(snapUp(2 * 80, [64, 100, 128, 150, 200])).toBe(200)
    expect(snapUp(9999, [64, 512])).toBe(512)
    expect(previewUrl({ id: 7 }, 120)).toBe('/preview/7?height=256')
    expect(previewUrl({ hash: 'ab/c' }, 32)).toBe('/preview/hash/ab%2Fc?height=64')
    expect(avatarUrl({ user: 3, version: 9 }, 24)).toBe('/avatar/user/3?size=48&v=9')
    expect(avatarUrl({ texture: 5 }, 36, true)).toBe('/avatar/5?size=96&3d')
  })

  it('knows the shape of skin and cape previews', () => {
    expect(previewWidth('steve', 160)).toBe(180)
    expect(previewWidth('cape', 160)).toBe(100)
  })
})
