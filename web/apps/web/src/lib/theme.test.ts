import { describe, expect, it } from 'vitest'

import { themeStyle } from './theme'

describe('themeStyle', () => {
  it('renders light, dark and font tokens with high-specificity selectors', () => {
    const css = themeStyle({
      light: { primary: 'oklch(0.6 0.2 250)', radius: '0.75rem' },
      dark: { primary: 'oklch(0.7 0.1 250)' },
      fonts: { sans: 'Inter, sans-serif' },
    })
    expect(css).toBe(
      ':root:root{--primary:oklch(0.6 0.2 250);--radius:0.75rem;--sl-font-sans:Inter, sans-serif;}' +
        ':root:root.dark{--primary:oklch(0.7 0.1 250);}',
    )
  })

  it('drops values and names that could break out of the style element', () => {
    const css = themeStyle({
      light: {
        ok: 'red',
        bad1: 'red;}body{display:none',
        bad2: 'x</style><script>alert(1)</script>',
        'Bad Name': 'red',
        'x;y': 'red',
      },
      dark: {},
    })
    expect(css).toBe(':root:root{--ok:red;}:root:root.dark{}')
    expect(css).not.toContain('<')
  })

  it('tolerates missing maps', () => {
    expect(themeStyle({ light: {}, dark: {}, fonts: undefined } as never)).toBe(':root:root{}:root:root.dark{}')
  })
})
