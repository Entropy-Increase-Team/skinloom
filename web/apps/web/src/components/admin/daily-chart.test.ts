import { describe, expect, it } from 'vitest'

import { niceMax } from './daily-chart'

describe('niceMax', () => {
  it('rounds up to 1, 2 or 5 times a power of ten', () => {
    expect([0, 1, 2, 3, 7, 10, 11, 23, 51, 180, 4200].map(niceMax)).toEqual([1, 1, 2, 5, 10, 10, 20, 50, 100, 200, 5000])
  })
})
