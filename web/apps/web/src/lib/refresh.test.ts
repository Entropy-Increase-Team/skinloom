import { describe, expect, it } from 'vitest'

import { MAX_REFRESH_JITTER_MS, nextRefreshDelay } from './refresh'

describe('nextRefreshDelay', () => {
  it('stays within [0, MAX)', () => {
    expect(nextRefreshDelay(() => 0)).toBe(0)
    expect(nextRefreshDelay(() => 0.999999)).toBeLessThan(MAX_REFRESH_JITTER_MS)
    for (let i = 0; i < 100; i++) {
      const d = nextRefreshDelay()
      expect(d).toBeGreaterThanOrEqual(0)
      expect(d).toBeLessThan(MAX_REFRESH_JITTER_MS)
    }
  })
})
