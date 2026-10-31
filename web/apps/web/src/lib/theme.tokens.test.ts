import { describe, expect, it } from 'vitest'

import { safeToken } from '../components/admin/appearance-form'
import { safeTokenValue, themeStyle } from './theme'

// All tokens are written on one line: one value must not swallow the ones after it.
describe('theme token values', () => {
  it.each(['"', "'", 'red\\', 'url(https://x/a.png', 'calc(1px + 2px', 'a\nb'])('%s is refused, and left out of the stylesheet', (bad) => {
    expect(safeToken(bad)).toBe(false)
    expect(safeTokenValue(bad)).toBe(false)
    const css = themeStyle({ light: { primary: bad, radius: '1rem' }, dark: { primary: 'red' } })
    expect(css).toBe(':root:root{--radius:1rem;}:root:root.dark{--primary:red;}')
  })

  it.each(['"Noto Sans SC", sans-serif', "url('/a b.png')", 'calc(1px + (2px * 3))', 'oklch(0.7 0.1 200)'])('%s is fine', (ok) => {
    expect(safeToken(ok)).toBe(true)
  })
})
