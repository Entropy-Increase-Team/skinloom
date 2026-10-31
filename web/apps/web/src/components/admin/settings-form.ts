import type { SettingView } from '@skinloom/api-client'

/** UI languages a localized setting is edited in. */
export const LOCALES = ['zh-CN', 'en'] as const

/**
 * Sections of the settings and appearance pages in page order, matched by key prefix
 * (the longest match wins). Settings of unknown prefixes, such as a plugin's, land in
 * "other" on the settings page.
 */
export const SECTIONS = [
  { id: 'site', page: 'settings', prefixes: ['site.'] },
  { id: 'auth', page: 'settings', prefixes: ['auth.', 'captcha.'] },
  { id: 'social', page: 'settings', prefixes: ['auth.social.'] },
  { id: 'mail', page: 'settings', prefixes: ['mail.'] },
  { id: 'score', page: 'settings', prefixes: ['score.'] },
  { id: 'texture', page: 'settings', prefixes: ['texture.'] },
  { id: 'player', page: 'settings', prefixes: ['player.', 'delivery.'] },
  { id: 'yggdrasil', page: 'settings', prefixes: ['yggdrasil.'] },
  { id: 'oauth', page: 'settings', prefixes: ['oauth.'] },
  { id: 'cdn', page: 'settings', prefixes: ['cdn.'] },
  { id: 'other', page: 'settings', prefixes: [''] },
  { id: 'branding', page: 'appearance', prefixes: ['site.logo', 'site.favicon', 'site.copyright', 'seo.'] },
  { id: 'customCss', page: 'appearance', prefixes: ['theme.custom_css'] },
] as const

export type SectionId = (typeof SECTIONS)[number]['id']
export type SettingsPage = (typeof SECTIONS)[number]['page']

/** Settings edited on pages of their own (appearance, translations), not as form fields. */
const OWN_EDITORS = new Set(['theme.active', 'theme.overrides', 'home.blocks', 'i18n.overrides'])

/** Localized settings written in Markdown, edited in a text area. */
export const MULTILINE = new Set(['site.announcement', 'site.copyright', 'texture.content_policy'])

/**
 * The settings of each section of page that has any, in page order. Within a section
 * they follow its prefixes (logo before favicon), then the order given (by key).
 */
export function groupSettings(views: SettingView[], page: SettingsPage): { id: SectionId; settings: SettingView[] }[] {
  const groups = new Map<SectionId, { view: SettingView; rank: number }[]>()
  for (const v of views) {
    if (OWN_EDITORS.has(v.key)) continue
    let best: { id: SectionId; length: number; rank: number } = { id: 'other', length: -1, rank: 0 }
    for (const s of SECTIONS) {
      s.prefixes.forEach((p: string, rank) => {
        if (v.key.startsWith(p) && p.length > best.length) best = { id: s.id, length: p.length, rank }
      })
    }
    groups.set(best.id, [...(groups.get(best.id) ?? []), { view: v, rank: best.rank }])
  }
  return SECTIONS.filter((s) => s.page === page && groups.has(s.id)).map((s) => ({
    id: s.id,
    settings: groups
      .get(s.id)!
      .toSorted((a, b) => a.rank - b.rank)
      .map((x) => x.view),
  }))
}

/**
 * What a field holds while it is edited: text for strings and numbers, a boolean, one
 * text per language, or JSON source. `reset` asks for the default back.
 */
export type Draft = { value: string | boolean | Record<string, string>; reset?: boolean }

export function draftOf(v: SettingView): Draft {
  switch (v.type) {
    case 'bool':
      return { value: v.value === true }
    case 'localized': {
      const m = (v.value ?? {}) as Record<string, string>
      return { value: Object.fromEntries(LOCALES.map((l) => [l, m[l] ?? ''])) }
    }
    case 'json':
      return { value: JSON.stringify(v.value, null, 2) }
    case 'int':
      return { value: String(v.value ?? '') }
  }
  // A secret is never sent back: its field starts empty and is sent only when typed in.
  return { value: v.secret ? '' : String(v.value ?? '') }
}

/** Why a field cannot be saved, for a message in the page's language. */
export type FieldProblem = { code: 'integer' } | { code: 'range'; min?: number; max?: number } | { code: 'choice' } | { code: 'json' }

/**
 * The changes of one section for PATCH /api/v1/admin/settings: only fields that differ
 * from what was loaded, null for a reset, secrets only when typed in. Values are checked
 * against the enum and range the server declares, so most mistakes are caught before
 * saving and shown in the page's language.
 */
export function changesOf(
  views: SettingView[],
  drafts: Record<string, Draft>,
): { changes: Record<string, unknown>; problems: Record<string, FieldProblem> } {
  const changes: Record<string, unknown> = {}
  const problems: Record<string, FieldProblem> = {}
  for (const v of views) {
    const d = drafts[v.key]
    if (!d) continue
    if (d.reset) {
      if (v.isSet) changes[v.key] = null
      continue
    }
    const value = valueOf(v, d)
    if ('problem' in value) {
      problems[v.key] = value.problem
      continue
    }
    if (v.secret ? value.value === '' : equal(value.value, comparable(v))) continue
    changes[v.key] = value.value
  }
  return { changes, problems }
}

function valueOf(v: SettingView, d: Draft): { value: unknown } | { problem: FieldProblem } {
  let value: unknown = d.value
  switch (v.type) {
    case 'int': {
      const n = Number(String(d.value).trim())
      if (String(d.value).trim() === '' || !Number.isInteger(n)) return { problem: { code: 'integer' } }
      if ((v.min !== undefined && n < v.min) || (v.max !== undefined && n > v.max)) return { problem: { code: 'range', min: v.min, max: v.max } }
      value = n
      break
    }
    case 'localized':
      // Over the stored value: languages the editor has no field for are kept.
      value = withoutEmpty({ ...((v.value ?? {}) as Record<string, string>), ...(d.value as Record<string, string>) })
      break
    case 'json':
      try {
        value = JSON.parse(String(d.value))
      } catch {
        return { problem: { code: 'json' } }
      }
      break
    case 'string':
    case 'text':
      if (!v.secret) value = String(d.value).trim()
  }
  if (v.enum && v.enum.length > 0 && !v.enum.some((e) => equal(e, value))) return { problem: { code: 'choice' } }
  return { value }
}

/**
 * The loaded value as changesOf compares it: localized values without empty languages,
 * texts trimmed as a draft is (the API and the CLI store them as given).
 */
function comparable(v: SettingView): unknown {
  switch (v.type) {
    case 'localized':
      return withoutEmpty((v.value ?? {}) as Record<string, string>)
    case 'string':
    case 'text':
      return typeof v.value === 'string' ? v.value.trim() : v.value
  }
  return v.value
}

function withoutEmpty(m: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(m).filter(([, s]) => s.trim() !== ''))
}

/** Deep equality of JSON values; object key order does not matter. */
export function equal(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const ka = Object.keys(a)
  const kb = Object.keys(b)
  return ka.length === kb.length && ka.every((k) => equal((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
}
