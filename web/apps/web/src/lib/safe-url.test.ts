import { describe, expect, it } from 'vitest'

import { safeHref, safeImageUrl } from './safe-url'

describe('safeHref', () => {
  it.each([
    ['/skinlib', '/skinlib'],
    ['/user/closet?tab=1#x', '/user/closet?tab=1#x'],
    ['  /about  ', '/about'],
    ['https://example.com/a', 'https://example.com/a'],
    ['http://example.com', 'http://example.com/'],
  ])('keeps %s', (input, want) => {
    expect(safeHref(input, '/fallback')).toBe(want)
  })

  it.each([
    'javascript:alert(1)',
    'JavaScript:alert(1)',
    ' javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox',
    '//evil.example.com',
    '/\\evil.example.com',
    'relative/path',
    '',
    42,
    null,
    undefined,
  ])('rejects %s', (input) => {
    expect(safeHref(input, '/fallback')).toBe('/fallback')
  })
})

describe('safeImageUrl', () => {
  it('returns undefined for unsafe values', () => {
    expect(safeImageUrl('javascript:x')).toBeUndefined()
    expect(safeImageUrl(undefined)).toBeUndefined()
  })
  it('keeps site paths and https images', () => {
    expect(safeImageUrl('/themes-assets/a/1/hero.webp')).toBe('/themes-assets/a/1/hero.webp')
    expect(safeImageUrl('https://cdn.example.com/bg.png')).toBe('https://cdn.example.com/bg.png')
  })
})
