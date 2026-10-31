import type { ResolvedTheme } from '@skinloom/api-client'

const tokenName = /^[a-z0-9-]+$/

/**
 * Whether a token value stays inside its CSS declaration (as internal/theme checks it).
 * All tokens are written on one line, so a value must neither end the declaration (; { }
 * < >, a comment) nor run past it into the next ones (an unterminated string, an unclosed
 * bracket, a trailing escape, a line break).
 */
export function safeTokenValue(v: string): boolean {
  if (/[;{}<>\n\r\f]/.test(v) || v.includes('/*')) return false
  let quote = ''
  let depth = 0
  let escaped = false
  for (const c of v) {
    if (escaped) escaped = false
    else if (c === '\\') escaped = true
    else if (quote) {
      if (c === quote) quote = ''
    } else if (c === '"' || c === "'") quote = c
    else if (c === '(') depth++
    else if (c === ')') {
      if (depth === 0) return false
      depth--
    }
  }
  return !escaped && quote === '' && depth === 0
}

function declarations(map: Record<string, string> | null | undefined, prefix = '--'): string {
  return Object.entries(map ?? {})
    .filter(([k, v]) => tokenName.test(k) && safeTokenValue(v))
    .map(([k, v]) => `${prefix}${k}:${v};`)
    .join('')
}

/**
 * CSS for the active theme's tokens. The doubled :root selector outranks the fallback
 * values in globals.css regardless of stylesheet order; .dark outranks light. Fonts and
 * the 3D viewer's tokens apply in both modes.
 */
export function themeStyle(tokens: ResolvedTheme['tokens']): string {
  const light = declarations(tokens.light) + declarations(tokens.fonts, '--sl-font-') + declarations(tokens.viewer, '--sl-viewer-')
  const dark = declarations(tokens.dark)
  return `:root:root{${light}}:root:root.dark{${dark}}`
}

/** Layout variant of one area of the site (theme.layout), the first variant if unset. */
export function layoutOf<A extends keyof Layouts>(layout: Record<string, string> | undefined, area: A): Layouts[A] {
  const v = layout?.[area]
  return ((LAYOUTS[area] as readonly string[]).includes(v ?? '') ? v : LAYOUTS[area][0]) as Layouts[A]
}

/** Grid of texture cards in the skin library's density (layout skinlib). */
export function textureGridClass(density: Layouts['skinlib']): string {
  return density === 'compact'
    ? 'grid grid-cols-3 gap-3 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8'
    : 'grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5'
}

/** The layout variants themes can choose (internal/theme.Layouts). */
const LAYOUTS = { auth: ['card', 'split'], skinlib: ['comfortable', 'compact'] } as const
type Layouts = { [A in keyof typeof LAYOUTS]: (typeof LAYOUTS)[A][number] }
