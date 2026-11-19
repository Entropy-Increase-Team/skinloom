import type { Block, BlockType, ThemeView } from '@skinloom/api-client'

import { safeTokenValue } from '@/lib/theme'

import { LOCALES } from './settings-form'

/** theme.overrides: token values on top of the active theme, by group. */
export type Overrides = Partial<Record<'light' | 'dark' | 'fonts' | 'viewer', Record<string, string>>>
export type Mode = 'light' | 'dark'

/**
 * Tokens the colour editor offers in each mode; radius (one for both modes, set with the
 * light tokens) comes on its own. Any other token stays settable through theme.overrides.
 */
export const COLOR_TOKENS = ['primary', 'primary-foreground', 'highlight', 'background', 'foreground', 'card', 'muted', 'accent', 'border'] as const

/** Overrides with one token set, or removed when value is empty; empty groups are dropped. */
export function withToken(o: Overrides, group: keyof Overrides, name: string, value: string): Overrides {
  const tokens = { ...(o[group] ?? {}) }
  if (value.trim() === '') delete tokens[name]
  else tokens[name] = value.trim()
  const next: Overrides = { ...o, [group]: tokens }
  if (Object.keys(tokens).length === 0) delete next[group]
  return next
}

/** Whether a value can go into a CSS declaration; the server refuses the others (internal/modules/theme). */
export function safeToken(v: string): boolean {
  return [...v].length <= 200 && safeTokenValue(v)
}

/** A block as the editor holds it: stored values, text possibly per language. */
export interface EditBlock {
  _uid?: string
  type: string
  props: Record<string, unknown>
}

let nextEditBlockUid = 1

export function createEditBlockUid(): string {
  return `blk-${nextEditBlockUid++}`
}

export function editBlocks(blocks: Block[] | null | undefined): EditBlock[] {
  return (blocks ?? []).map((b) => ({ _uid: createEditBlockUid(), type: b.type, props: { ...(b.props ?? {}) } }))
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

const BUILTIN_BLOCK_TYPES = new Set([
  'hero',
  'announcement',
  'features',
  'stats',
  'latest-textures',
  'popular-textures',
  'markdown',
  'image',
  'buttons',
  'launcher-guide',
])

/**
 * home.blocks as saved: properties without a value are left out (the block then uses
 * its default), and so is an empty props object. Buttons without a link are dropped.
 * Unknown blocks and schema-less plugin blocks keep their props as-is so saving other
 * changes never strips plugin block configuration.
 */
export function savedBlocks(blocks: EditBlock[], types: BlockType[]): Block[] {
  return blocks.map((b) => {
    const bareType = b.type.includes(':') ? b.type.split(':', 2)[1] : b.type
    const known = types.find((t) => t.type === b.type || t.type === bareType)
    if (!known || (!BUILTIN_BLOCK_TYPES.has(b.type) && (known.props?.length ?? 0) === 0)) {
      return Object.keys(b.props).length > 0 ? { type: b.type, props: { ...b.props } } : { type: b.type }
    }
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

/** theme.settings: configured values per theme ID. */
export type ThemeSettingsMap = Record<string, Record<string, string | boolean>>

/** Updates one setting of themeId in theme.settings, dropping empty theme objects. */
export function withThemeSetting(all: ThemeSettingsMap, themeId: string, name: string, value: string | boolean | null): ThemeSettingsMap {
  const cur = { ...(all[themeId] ?? {}) }
  if (value === null || (typeof value === 'string' && value.trim() === '')) delete cur[name]
  else cur[name] = typeof value === 'string' ? value.trim() : value
  const next: ThemeSettingsMap = { ...all, [themeId]: cur }
  if (Object.keys(cur).length === 0) delete next[themeId]
  return next
}

/**
 * Packages the current token overrides into a local theme zip archive that extends
 * base (ADR 0043: "颜色 → 导出为主题"). When base itself already extends a parent
 * theme, flatten its resolved tokens under overrides and inherit directly from its
 * parent so the exported package obeys single-level inheritance.
 */
export function buildThemeZip(
  base: string | Pick<ThemeView, 'id' | 'extends' | 'tokens'>,
  overrides: Overrides,
): Uint8Array {
  const baseId = typeof base === 'string' ? base : base.id
  const parentExtends = typeof base === 'string' ? undefined : base.extends
  const parentTokens = typeof base === 'string' ? undefined : base.tokens
  const id = `${baseId}-custom`.slice(0, 32)
  const extendsId = parentExtends || baseId
  const light = parentExtends ? { ...parentTokens?.light, ...(overrides.light ?? {}) } : (overrides.light ?? {})
  const dark = parentExtends ? { ...parentTokens?.dark, ...(overrides.dark ?? {}) } : (overrides.dark ?? {})
  const fonts = parentExtends && parentTokens?.fonts ? { ...parentTokens.fonts, ...(overrides.fonts ?? {}) } : overrides.fonts
  const viewer = parentExtends && parentTokens?.viewer ? { ...parentTokens.viewer, ...(overrides.viewer ?? {}) } : overrides.viewer
  const manifest = {
    $schema: 'https://skinloom.cn/schema/plugin.v1.json',
    id,
    kind: 'theme',
    version: '0.1.0',
    name: { 'zh-CN': `${baseId} 定制`, en: `${baseId} Custom` },
    description: { 'zh-CN': '从后台颜色调整导出的主题', en: 'Exported from appearance color adjustments' },
    license: 'MIT',
    apiVersion: 1,
    extends: extendsId,
    theme: {
      tokens: {
        light,
        dark,
        ...(fonts && Object.keys(fonts).length > 0 ? { fonts } : {}),
        ...(viewer && Object.keys(viewer).length > 0 ? { viewer } : {}),
      },
      css: 'theme.css',
    },
  }
  const enc = new TextEncoder()
  return storeZip([
    { name: `${id}/plugin.json`, data: enc.encode(`${JSON.stringify(manifest, null, 2)}\n`) },
    { name: `${id}/theme.css`, data: enc.encode(`/* ${id}: extra styles on top of token variables. */\n`) },
    {
      name: `${id}/README.md`,
      data: enc.encode(`# ${id}\n\nUnpack into \`data/themes/${id}\` and click Rescan in Admin -> Appearance.\n`),
    },
  ])
}

function crc32(bytes: Uint8Array): number {
  let c = 0xffffffff
  for (let i = 0; i < bytes.length; i++) {
    c ^= bytes[i]!
    for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (c & 1 ? 0xedb88320 : 0)
  }
  return (c ^ 0xffffffff) >>> 0
}

function storeZip(files: { name: string; data: Uint8Array }[]): Uint8Array {
  const enc = new TextEncoder()
  const locals: Uint8Array[] = []
  const centrals: Uint8Array[] = []
  let offset = 0
  for (const f of files) {
    const nameBytes = enc.encode(f.name)
    const crc = crc32(f.data)
    const local = new Uint8Array(30 + nameBytes.length + f.data.length)
    const lv = new DataView(local.buffer)
    lv.setUint32(0, 0x04034b50, true)
    lv.setUint16(4, 20, true)
    lv.setUint32(14, crc, true)
    lv.setUint32(18, f.data.length, true)
    lv.setUint32(22, f.data.length, true)
    lv.setUint16(26, nameBytes.length, true)
    local.set(nameBytes, 30)
    local.set(f.data, 30 + nameBytes.length)
    locals.push(local)

    const central = new Uint8Array(46 + nameBytes.length)
    const cv = new DataView(central.buffer)
    cv.setUint32(0, 0x02014b50, true)
    cv.setUint16(4, 20, true)
    cv.setUint16(6, 20, true)
    cv.setUint32(16, crc, true)
    cv.setUint32(20, f.data.length, true)
    cv.setUint32(24, f.data.length, true)
    cv.setUint16(28, nameBytes.length, true)
    cv.setUint32(42, offset, true)
    central.set(nameBytes, 46)
    centrals.push(central)
    offset += local.length
  }
  const cdSize = centrals.reduce((s, b) => s + b.length, 0)
  const eocd = new Uint8Array(22)
  const ev = new DataView(eocd.buffer)
  ev.setUint32(0, 0x06054b50, true)
  ev.setUint16(8, files.length, true)
  ev.setUint16(10, files.length, true)
  ev.setUint32(12, cdSize, true)
  ev.setUint32(16, offset, true)

  const out = new Uint8Array(offset + cdSize + 22)
  let pos = 0
  for (const b of [...locals, ...centrals, eocd]) {
    out.set(b, pos)
    pos += b.length
  }
  return out
}
