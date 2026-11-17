import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { SkinCarousel } from './skin-carousel'
import { SkinViewer } from './skin-viewer'

describe('SkinViewer markup accessibility', () => {
  it('places role="img" on the canvas rather than wrapping toolbar buttons', () => {
    const html = renderToStaticMarkup(<SkinViewer skin="/textures/abc" label="Steve skin" />)
    expect(html).toContain('<div data-slot="skin-viewer" data-state="loading" data-view="3d"')
    expect(html).not.toMatch(/<div[^>]*role="img"/)
    expect(html).toMatch(/<canvas[^>]*role="img"[^>]*aria-label="Steve skin"/)
    expect(html).toMatch(/<button[^>]*aria-label="[^"]+"[^>]*>.*<span class="max-sm:hidden">/)
  })
})

describe('SkinCarousel mobile accessibility', () => {
  it('renders a mobile link to the active skin detail page', () => {
    const html = renderToStaticMarkup(
      <SkinCarousel
        items={[
          { id: 1, name: 'Test Skin', skin: '/textures/abc', model: 'default', href: '/skinlib/show/1' },
          { id: 2, name: 'Second Skin', skin: '/textures/def', model: 'slim', href: '/skinlib/show/2' },
        ]}
      />,
    )
    expect(html).toMatch(/<canvas[^>]*role="img"[^>]*aria-label="Test Skin"/)
    expect(html).toContain('href="/skinlib/show/1"')
  })
})
