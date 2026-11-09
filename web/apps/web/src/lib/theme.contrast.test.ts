import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

// Built-in themes must keep every text readable (web/design.md §2.4), and the fallback
// tokens in @skinloom/ui's globals.css must stay equal to the default theme's.

const repo = path.resolve(import.meta.dirname, '../../../../..')
const themesDir = path.join(repo, 'themes')

type Mode = 'light' | 'dark'
interface Manifest {
  id: string
  theme: { tokens: Record<Mode, Record<string, string>> }
}

const themes: Manifest[] = readdirSync(themesDir, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .map((d) => JSON.parse(readFileSync(path.join(themesDir, d.name, 'plugin.json'), 'utf8')) as Manifest)

/** Linear sRGB of an opaque oklch() colour; undefined for anything else. */
function rgb(value: string): [number, number, number] | undefined {
  const m = /^oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\)$/.exec(value.trim())
  if (!m) return undefined
  const [L, C, h] = m.slice(1).map(Number)
  const a = C * Math.cos((h * Math.PI) / 180)
  const b = C * Math.sin((h * Math.PI) / 180)
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const mm = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const clamp = (v: number) => Math.min(1, Math.max(0, v))
  return [
    clamp(4.0767416621 * l - 3.3077115913 * mm + 0.2309699292 * s),
    clamp(-1.2684380046 * l + 2.6097574011 * mm - 0.3413193965 * s),
    clamp(-0.0041960863 * l - 0.7034186147 * mm + 1.707614701 * s),
  ]
}

function contrast(fg: string, bg: string): number {
  const [a, b] = [rgb(fg), rgb(bg)]
  if (!a || !b) throw new Error(`not an opaque oklch() colour: ${a ? bg : fg}`)
  const lum = (c: [number, number, number]) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** [text token, surface token, minimum ratio] (design.md §2.4). */
const PAIRS: [string, string, number][] = [
  ['foreground', 'background', 7],
  ['muted-foreground', 'card', 4.5],
  ['muted-foreground', 'muted', 4.5],
  ['faint-foreground', 'card', 4.5],
  ['faint-foreground', 'background', 4.5],
  ['primary-foreground', 'primary', 4.5],
  ['primary', 'card', 4.5],
  ['highlight-foreground', 'highlight', 4.5],
  ['success-foreground', 'success', 4.5],
  ['destructive-foreground', 'destructive', 4.5],
  ['ring', 'background', 3],
]

describe.each(themes.map((t) => [t.id, t] as const))('theme %s', (_, theme) => {
  it.each(['light', 'dark'] as const)('%s mode keeps text readable', (mode) => {
    const tokens = theme.theme.tokens[mode]
    for (const [fg, bg, min] of PAIRS) {
      expect(tokens[fg], `${mode}.${fg}`).toBeDefined()
      expect(tokens[bg], `${mode}.${bg}`).toBeDefined()
      const ratio = contrast(tokens[fg], tokens[bg])
      expect(ratio, `${mode}: ${fg} on ${bg} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(min)
    }
  })
})

describe('globals.css fallback tokens', () => {
  const css = readFileSync(path.join(repo, 'web/packages/ui/src/styles/globals.css'), 'utf8')
  const block = (selector: string) => {
    const body = new RegExp(`\\n  ${selector.replace('.', '\\.')} \\{([^}]*)\\}`).exec(css)?.[1] ?? ''
    return Object.fromEntries([...body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]))
  }
  const fallback: Record<Mode, Record<string, string>> = { light: block(':root'), dark: block('.dark') }
  const def = themes.find((t) => t.id === 'default')!

  it.each(['light', 'dark'] as const)('equal the default theme in %s mode', (mode) => {
    const tokens = def.theme.tokens[mode]
    expect(Object.keys(tokens).length).toBeGreaterThan(10)
    for (const [name, value] of Object.entries(tokens)) expect(fallback[mode][name], `${mode}.${name}`).toBe(value)
  })
})
