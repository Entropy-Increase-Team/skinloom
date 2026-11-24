import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import { layoutOf, themeSettingAttrs, themeStyle } from './theme'

const repo = path.resolve(import.meta.dirname, '../../../../..')

describe('themeStyle', () => {
  it('renders light, dark, font, viewer and setting tokens with high-specificity selectors', () => {
    const css = themeStyle(
      {
        light: { primary: 'oklch(0.6 0.2 250)', radius: '0.75rem' },
        dark: { primary: 'oklch(0.7 0.1 250)' },
        fonts: { sans: 'Inter, sans-serif' },
        viewer: { background: 'transparent' },
      },
      { density: 'compact', bad: 'red;}' },
    )
    expect(css).toBe(
      ':root:root{--primary:oklch(0.6 0.2 250);--radius:0.75rem;--sl-font-sans:Inter, sans-serif;--sl-viewer-background:transparent;--sl-setting-density:compact;}' +
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

describe('themeSettingAttrs', () => {
  it('builds lowercase data-sl-setting-* attributes for safe settings', () => {
    expect(themeSettingAttrs({ density: 'compact', cardBorder: 'strong', lattice: 'true', 'bad name': 'x', inject: 'a;b' })).toEqual({
      'data-sl-setting-density': 'compact',
      'data-sl-setting-cardborder': 'strong',
      'data-sl-setting-lattice': 'true',
    })
  })
})

describe('layoutOf', () => {
  it('takes a known variant and falls back to the first one', () => {
    expect(layoutOf({ auth: 'split' }, 'auth')).toBe('split')
    expect(layoutOf({ auth: 'fullscreen' }, 'auth')).toBe('card')
    expect(layoutOf(undefined, 'skinlib')).toBe('comfortable')
    expect(layoutOf({ user: 'tabs' }, 'user')).toBe('tabs')
    expect(layoutOf(undefined, 'user')).toBe('sidebar')
    expect(layoutOf({ texture: 'stacked' }, 'texture')).toBe('stacked')
    expect(layoutOf(undefined, 'texture')).toBe('split')
  })
})

describe('cascade layer contract (ADR 0043)', () => {
  function stripTopLevelAtBlocks(css: string): string {
    const allowed = /^@(?:layer|theme|utility|custom-variant|keyframes)\b/
    let out = ''
    let i = 0
    while (i < css.length) {
      if (css[i] === '@' && allowed.test(css.slice(i))) {
        const open = css.indexOf('{', i)
        if (open === -1) break
        let depth = 1
        let j = open + 1
        while (j < css.length && depth > 0) {
          if (css[j] === '{') depth++
          else if (css[j] === '}') depth--
          j++
        }
        i = j
        continue
      }
      out += css[i]
      i++
    }
    return out.replace(/@(?:import|source|plugin)\b[^;]*;/g, '').trim()
  }

  it('keeps all host selector rules inside @layer so unlayered theme CSS always wins', () => {
    for (const rel of ['web/packages/ui/src/styles/globals.css', 'web/apps/web/src/app/globals.css']) {
      const src = readFileSync(path.join(repo, rel), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
      const rest = stripTopLevelAtBlocks(src)
      expect(rest, `${rel} has unlayered rules: ${rest}`).toBe('')
    }
  })
})

describe('theme hook contract (web/design.md §14)', () => {
  function collectSource(dir: string): string {
    let out = ''
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name)
      if (entry.isDirectory()) out += collectSource(full)
      else if (/\.(tsx|ts)$/.test(entry.name) && !entry.name.endsWith('.test.ts')) out += readFileSync(full, 'utf8') + '\n'
    }
    return out
  }

  const allSource =
    collectSource(path.join(repo, 'web/apps/web/src')) + collectSource(path.join(repo, 'web/packages/ui/src'))

  const requiredHooks = [
    'data-sl-theme=',
    'data-sl-area=',
    'data-sl-setting-',
    'sl-header',
    'sl-footer',
    'sl-sidebar',
    'sl-page-header',
    'sl-hero',
    'sl-block-hero',
    'sl-block-announcement',
    'sl-block-features',
    'sl-block-stats',
    'sl-block-latest-textures',
    'sl-block-popular-textures',
    'sl-block-markdown',
    'sl-block-image',
    'sl-block-buttons',
    'sl-block-launcher-guide',
    'sl-auth-panel',
    'sl-texture-card',
    'sl-player-card',
    'sl-pixel-backdrop',
    'sl-viewer',
    'sl-markdown',
    'data-slot=',
  ]

  it.each(requiredHooks)('renders documented hook %s', (hook) => {
    expect(allSource).toContain(hook)
  })
})
