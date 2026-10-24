import { describe, expect, it } from 'vitest'

import { filterHref, parseFilter } from './library'

describe('parseFilter', () => {
  it('keeps known values and drops the rest', () => {
    expect(parseFilter({ category: 'cape', sort: 'likes', q: '  red ' })).toEqual({ category: 'cape', sort: 'likes', q: 'red' })
    expect(parseFilter({ category: 'creeper', sort: 'email', q: ['a', 'b'] })).toEqual({ category: '', sort: 'time', q: 'a' })
    expect(parseFilter({ q: 'x'.repeat(80) }).q).toHaveLength(50)
  })
})

describe('filterHref', () => {
  const f = parseFilter({})
  it('leaves defaults out', () => {
    expect(filterHref(f)).toBe('/skinlib')
    expect(filterHref(f, { category: 'skin', sort: 'likes' })).toBe('/skinlib?category=skin&sort=likes')
    expect(filterHref({ ...f, q: '红 色' })).toBe('/skinlib?q=%E7%BA%A2+%E8%89%B2')
  })
})
