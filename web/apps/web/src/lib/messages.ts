import { type MessageFormatElement, parse, TYPE } from '@formatjs/icu-messageformat-parser'

/** Nested interface texts, as in messages/<locale>.json. */
export interface Messages {
  [key: string]: string | Messages
}

/**
 * messages with the administrator's wording (i18n.overrides, from the bootstrap) laid
 * over it. Only existing texts are replaced, so an override for a removed key changes
 * nothing; the objects on the way are copied, as messages is the module shared by every
 * request.
 */
export function withOverrides(messages: Messages, overrides: Record<string, string> | undefined): Messages {
  let out = messages
  for (const [key, text] of Object.entries(overrides ?? {})) {
    if (typeof text !== 'string') continue
    out = setText(out, key.split('.'), text) ?? out
  }
  return out
}

function setText(node: Messages, path: string[], text: string): Messages | undefined {
  const [head, ...rest] = path
  const child = node[head]
  if (rest.length === 0) return typeof child === 'string' ? { ...node, [head]: text } : undefined
  if (typeof child !== 'object' || child === null) return undefined
  const next = setText(child, rest, text)
  return next ? { ...node, [head]: next } : undefined
}

/** Every text of messages by its dotted key, in file order. */
export function flatten(messages: Messages, prefix = ''): [string, string][] {
  return Object.entries(messages).flatMap(([k, v]) => (typeof v === 'string' ? [[prefix + k, v] as [string, string]] : flatten(v, `${prefix}${k}.`)))
}

/**
 * Why an override of a text cannot be used: it is not a well-formed ICU message (next-intl
 * would show the key instead), or it does not keep the default's placeholders and tags,
 * which the page fills in. null when it is fine.
 */
export function overrideProblem(defaultText: string, text: string): 'syntax' | 'placeholders' | null {
  let override: MessageFormatElement[]
  try {
    override = parse(text)
  } catch {
    return 'syntax'
  }
  let original: MessageFormatElement[]
  try {
    original = parse(defaultText)
  } catch {
    return null // nothing to compare with
  }
  return names(override).join() === names(original).join() ? null : 'placeholders'
}

/** i18n.overrides as saved: no empty texts or languages; null when nothing is left. */
export function savedOverrides(draft: Record<string, Record<string, string>>): Record<string, Record<string, string>> | null {
  const out: Record<string, Record<string, string>> = {}
  for (const [locale, texts] of Object.entries(draft)) {
    const kept = Object.fromEntries(Object.entries(texts).filter(([, t]) => t.trim() !== ''))
    if (Object.keys(kept).length > 0) out[locale] = kept
  }
  return Object.keys(out).length > 0 ? out : null
}

/** The placeholders (by name) and tags (as <name>) of a parsed message, sorted. */
function names(elements: MessageFormatElement[]): string[] {
  const out = new Set<string>()
  const walk = (els: MessageFormatElement[]) => {
    for (const el of els) {
      switch (el.type) {
        case TYPE.argument:
        case TYPE.number:
        case TYPE.date:
        case TYPE.time:
          out.add(el.value)
          break
        case TYPE.select:
        case TYPE.plural:
          out.add(el.value)
          for (const option of Object.values(el.options)) walk(option.value)
          break
        case TYPE.tag:
          out.add(`<${el.value}>`)
          walk(el.children)
          break
      }
    }
  }
  walk(elements)
  return [...out].sort()
}
