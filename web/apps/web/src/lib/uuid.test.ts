import { describe, expect, it } from 'vitest'

import { dashedUUID } from './uuid'

describe('dashedUUID', () => {
  it('adds the dashes', () => {
    expect(dashedUUID('b50ad385829d3141a2167e7d7539ba7f')).toBe('b50ad385-829d-3141-a216-7e7d7539ba7f')
  })
  it('leaves anything else alone', () => {
    expect(dashedUUID('not-a-uuid')).toBe('not-a-uuid')
  })
})
