import { describe, expect, it } from 'vitest'

import { savedBlocks } from './appearance-form'

// Saving the home page keeps what the editor cannot change.
describe('savedBlocks', () => {
  it('keeps the props of a block whose type the catalog does not list', () => {
    // e.g. a theme's home with a plugin block (P2), or a type removed in an upgrade: the
    // editor shows "unknown type", and saving another change must not strip its props.
    expect(savedBlocks([{ type: 'plugin-gallery', props: { album: 7 } }], [])).toEqual([{ type: 'plugin-gallery', props: { album: 7 } }])
  })

  it('keeps props of schema-less plugin blocks registered in the catalog', () => {
    const types = [
      { type: 'announcement', props: [] },
      { type: 'spotlight:showcase', props: [] },
    ] as never
    const blocks = [
      { type: 'announcement', props: { stale: true } },
      { type: 'spotlight:showcase', props: { album: 7, layout: 'grid' } },
    ]
    expect(savedBlocks(blocks, types)).toEqual([
      { type: 'announcement' },
      { type: 'spotlight:showcase', props: { album: 7, layout: 'grid' } },
    ])
  })

  it('drops a button that has no link yet (the server would refuse the whole list)', () => {
    const types = [{ type: 'hero', props: [{ name: 'actions', type: 'buttons' }] }] as never
    const blocks = [{ type: 'hero', props: { actions: [{ label: 'Go', href: '/skinlib' }, { label: undefined, href: '' }] } }]
    expect(savedBlocks(blocks, types)).toEqual([{ type: 'hero', props: { actions: [{ label: 'Go', href: '/skinlib' }] } }])
  })
})
