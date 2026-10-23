import type { ResolvedTheme } from '@skinloom/api-client'

const tokenName = /^[a-z0-9-]+$/

function declarations(map: Record<string, string> | null | undefined, prefix = '--'): string {
  return Object.entries(map ?? {})
    .filter(([k, v]) => tokenName.test(k) && !/[;{}<>]/.test(v))
    .map(([k, v]) => `${prefix}${k}:${v};`)
    .join('')
}

/**
 * CSS for the active theme's tokens. The doubled :root selector outranks the fallback
 * values in globals.css regardless of stylesheet order; .dark outranks light.
 */
export function themeStyle(tokens: ResolvedTheme['tokens']): string {
  const light = declarations(tokens.light) + declarations(tokens.fonts, '--sl-font-')
  const dark = declarations(tokens.dark)
  return `:root:root{${light}}:root:root.dark{${dark}}`
}
