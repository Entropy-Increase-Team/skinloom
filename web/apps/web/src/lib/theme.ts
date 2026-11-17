import type { ResolvedTheme } from '@skinloom/api-client'

const tokenName = /^[a-z0-9-]+$/
const settingName = /^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/

/**
 * Whether a token value stays inside its CSS declaration (as internal/modules/theme checks it).
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

function declarations(map: Record<string, string> | null | undefined, prefix = '--', namePat = tokenName): string {
  return Object.entries(map ?? {})
    .filter(([k, v]) => namePat.test(k) && safeTokenValue(v))
    .map(([k, v]) => `${prefix}${k}:${v};`)
    .join('')
}

/**
 * CSS for the active theme's tokens and non-token settings. The doubled :root selector
 * outranks the fallback values in globals.css regardless of stylesheet order; .dark
 * outranks light. Fonts, the 3D viewer's tokens and --sl-setting-* apply in both modes.
 */
export function themeStyle(tokens: ResolvedTheme['tokens'], settings?: Record<string, string>): string {
  const light =
    declarations(tokens.light) +
    declarations(tokens.fonts, '--sl-font-') +
    declarations(tokens.viewer, '--sl-viewer-') +
    declarations(settings, '--sl-setting-', settingName)
  const dark = declarations(tokens.dark)
  return `:root:root{${light}}:root:root.dark{${dark}}`
}

/**
 * data-sl-setting-<name> attributes placed on <html> so theme CSS can branch on choice or
 * boolean settings (e.g. html[data-sl-setting-density="compact"], ADR 0043).
 */
export function themeSettingAttrs(settings?: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(settings ?? {})) {
    if (settingName.test(k) && safeTokenValue(v)) {
      out[`data-sl-setting-${k.toLowerCase()}`] = v
    }
  }
  return out
}

/** Layout variant of one area of the site (theme.layout), the first variant if unset. */
export function layoutOf<A extends keyof Layouts>(layout: Record<string, string> | undefined, area: A): Layouts[A] {
  const v = layout?.[area]
  return ((LAYOUTS[area] as readonly string[]).includes(v ?? '') ? v : LAYOUTS[area][0]) as Layouts[A]
}

/** Grid of texture cards in the skin library's density (layout skinlib). */
export function textureGridClass(density: Layouts['skinlib']): string {
  return density === 'compact'
    ? 'grid grid-cols-3 gap-2 sm:grid-cols-4 sm:gap-3 md:grid-cols-6 lg:grid-cols-8'
    : 'grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-5'
}

/** The layout variants themes can choose (internal/modules/theme.Layouts). */
const LAYOUTS = {
  auth: ['card', 'split'],
  skinlib: ['comfortable', 'compact'],
  user: ['sidebar', 'tabs'],
  texture: ['split', 'stacked'],
} as const
type Layouts = { [A in keyof typeof LAYOUTS]: (typeof LAYOUTS)[A][number] }
