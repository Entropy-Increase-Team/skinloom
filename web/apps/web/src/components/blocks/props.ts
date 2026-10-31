/**
 * Block properties as pages receive them: the server has checked them against the
 * block catalog (internal/theme/blocks.go) and picked the visitor's language, so text is
 * a string and Markdown arrives rendered as <name>Html. A theme's own blocks are checked
 * when it is loaded. These helpers still treat every value as untrusted input.
 */
export type Props = Record<string, unknown>

/** A non-empty string, or undefined. */
export function str(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

/** A whole number from min to max, or fallback. */
export function num(v: unknown, min: number, max: number, fallback: number): number {
  return typeof v === 'number' && Number.isInteger(v) && v >= min && v <= max ? v : fallback
}
