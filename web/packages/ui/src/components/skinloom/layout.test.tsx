import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { Card, CardHeader, CardTitle } from '../ui/card'
import { UsageMeter } from './layout'

describe('UsageMeter accessibility', () => {
  it('links role="meter" to its visible label via aria-labelledby and sets aria-valuetext', () => {
    const html = renderToStaticMarkup(<UsageMeter label="Storage" value={64} max={100} format={(n) => `${n} MB`} />)
    const labelId = /<span id="([^"]+)"[^>]*>Storage<\/span>/.exec(html)?.[1]
    expect(labelId).toBeTruthy()
    expect(html).toContain(`role="meter" aria-labelledby="${labelId}"`)
    expect(html).toContain('aria-valuetext="64 MB / 100 MB"')
  })

  it('clamps bar width and ARIA bounds cleanly on edge values', () => {
    const negative = renderToStaticMarkup(<UsageMeter label="Score" value={-10} max={100} />)
    expect(negative).toContain('style="width:0%"')

    const zeroMax = renderToStaticMarkup(<UsageMeter label="Score" value={5} max={0} />)
    expect(zeroMax).toContain('aria-valuemax="0"')
    expect(zeroMax).toContain('style="width:0%"')
  })
})

describe('CardTitle semantic heading support', () => {
  it('renders an h1 element when as="h1" is passed', () => {
    const html = renderToStaticMarkup(
      <Card>
        <CardHeader>
          <CardTitle as="h1">Sign in</CardTitle>
        </CardHeader>
      </Card>,
    )
    expect(html).toMatch(/<h1[^>]*data-slot="card-title"[^>]*>Sign in<\/h1>/)
  })
})
