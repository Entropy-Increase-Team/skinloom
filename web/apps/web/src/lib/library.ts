export const CATEGORIES = ['', 'skin', 'steve', 'alex', 'cape'] as const
export const SORTS = ['time', 'likes'] as const

export type Category = (typeof CATEGORIES)[number]
export type Sort = (typeof SORTS)[number]

export interface LibraryFilter {
  category: Category
  q: string
  sort: Sort
}

type Params = Record<string, string | string[] | undefined>

const first = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v) ?? ''

/** Reads the skin library filter from the URL; anything unknown falls back to the default. */
export function parseFilter(sp: Params): LibraryFilter {
  const category = first(sp.category)
  const sort = first(sp.sort)
  return {
    category: (CATEGORIES as readonly string[]).includes(category) ? (category as Category) : '',
    q: first(sp.q).trim().slice(0, 50),
    sort: (SORTS as readonly string[]).includes(sort) ? (sort as Sort) : 'time',
  }
}

/** The library URL for `f` with `change` applied; defaults are left out. */
export function filterHref(f: LibraryFilter, change: Partial<LibraryFilter> = {}): string {
  const next = { ...f, ...change }
  const p = new URLSearchParams()
  if (next.category) p.set('category', next.category)
  if (next.q) p.set('q', next.q)
  if (next.sort !== 'time') p.set('sort', next.sort)
  const qs = p.toString()
  return qs ? `/skinlib?${qs}` : '/skinlib'
}
