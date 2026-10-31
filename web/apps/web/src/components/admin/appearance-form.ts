import type { Block, BlockType } from '@skinloom/api-client'

import { safeTokenValue } from '@/lib/theme'

import { LOCALES } from './settings-form'

/** theme.overrides: token values on top of the active theme, by group. */
export type Overrides = Partial<Record<'light' | 'dark' | 'fonts' | 'viewer', Record<string, string>>>
export type Mode = 'light' | 'dark'

/**
 * Tokens the colour editor offers in each mode; radius (one for both modes, set with the
 * light tokens) comes on its own. Any other token stays settable through theme.overrides.
 */
export const COLOR_TOKENS = ['primary', 'primary-foreground', 'background', 'foreground', 'card', 'muted', 'accent', 'border'] as const

/** Overrides with one token set, or removed when value is empty; empty groups are dropped. */
export function withToken(o: Overrides, group: keyof Overrides, name: string, value: string): Overrides {
  const tokens = { ...(o[group] ?? {}) }
  if (value.trim() === '') delete tokens[name]
  else tokens[name] = value.trim()
  const next: Overrides = { ...o, [group]: tokens }
  if (Object.keys(tokens).length === 0) delete next[group]
  return next
}

/** Whether a value can go into a CSS declaration; the server refuses the others (internal/theme). */
export function safeToken(v: string): boolean {
  return [...v].length <= 200 && safeTokenValue(v)
}

/** A block as the editor holds it: stored values, text possibly per language. */
export interface EditBlock {
  type: string
  props: Record<string, unknown>
}

export function editBlocks(blocks: Block[] | null | undefined): EditBlock[] {
  return (blocks ?? []).map((b) => ({ type: b.type, props: { ...(b.props ?? {}) } }))
}

/** One text per editing language; a plain string stands for every language. */
export function localizedOf(v: unknown): Record<string, string> {
  if (typeof v === 'string') return Object.fromEntries(LOCALES.map((l) => [l, v]))
  const m = (v && typeof v === 'object' ? v : {}) as Record<string, unknown>
  return Object.fromEntries(LOCALES.map((l) => [l, typeof m[l] === 'string' ? (m[l] as string) : '']))
}

/** The stored form of per-language text: languages left empty are dropped, nothing if all are. */
export function localizedValue(m: Record<string, string>): Record<string, string> | undefined {
  const out = Object.fromEntries(Object.entries(m).filter(([, s]) => s.trim() !== ''))
  return Object.keys(out).length > 0 ? out : undefined
}

/**
 * home.blocks as saved: properties without a value are left out (the block then uses
 * its default), and so is an empty props object. Buttons without a link are dropped.
 * A block of a type the catalog does not list (a plugin's that is not loaded) keeps its
 * props as they are: the editor cannot change them, and must not lose them.
 */
export function savedBlocks(blocks: EditBlock[], types: BlockType[]): Block[] {
  return blocks.map((b) => {
    const known = types.find((t) => t.type === b.type)
    if (!known) return Object.keys(b.props).length > 0 ? { type: b.type, props: { ...b.props } } : { type: b.type }
    const props: Record<string, unknown> = {}
    for (const p of known.props ?? []) {
      let v = b.props[p.name]
      if (p.type === 'buttons' && Array.isArray(v)) v = v.filter((item) => typeof item?.href === 'string' && item.href.trim() !== '')
      if (v === undefined || v === null || v === '') continue
      if (p.type === 'buttons' && (!Array.isArray(v) || v.length === 0)) continue
      props[p.name] = v
    }
    return Object.keys(props).length > 0 ? { type: b.type, props } : { type: b.type }
  })
}

/** blocks with the one at i moved by delta places (no change at the ends). */
export function moved<T>(list: T[], i: number, delta: -1 | 1): T[] {
  const j = i + delta
  if (j < 0 || j >= list.length) return list
  const next = [...list]
  ;[next[i], next[j]] = [next[j], next[i]]
  return next
}
