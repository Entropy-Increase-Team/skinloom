import type { AdminPlayerView } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { wearChange } from './players-table'

describe('wearChange', () => {
  // The player as the form showed it. Giving a player away takes off what the new
  // owner's closet lacks; comparing with that result instead would send the unchanged
  // skin field again, asking the server to put the skin back on, which fails (not in
  // the new owner's closet) after the transfer itself went through.
  const shown = { skin: { id: 5, name: 's' } } as Pick<AdminPlayerView, 'skin' | 'cape'>

  it('sends only the fields the administrator changed in the form', () => {
    expect(wearChange(shown, 5, 0)).toEqual({})
    expect(wearChange(shown, 7, 0)).toEqual({ skin: 7 })
    expect(wearChange(shown, 0, 9)).toEqual({ skin: 0, cape: 9 })
  })
})
