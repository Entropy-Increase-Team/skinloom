import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { cn } from '../../lib/utils'
import { Alert, AlertAction, AlertDescription } from '../ui/alert'
import { Avatar, AvatarFallback } from '../ui/avatar'
import { Button } from '../ui/button'
import { Card, CardHeader, CardTitle } from '../ui/card'
import { Pagination, PaginationContent, PaginationItem, PaginationLink } from '../ui/pagination'
import { Dropzone } from './dropzone'
import { PageHeader, UsageMeter } from './layout'
import { RelativeTime } from './relative-time'
import { SaveBar } from './save-bar'
import { TextureCard } from './texture-card'

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
    expect(negative).toContain('aria-valuenow="0"')
    expect(negative).toContain('style="width:0%"')

    const over = renderToStaticMarkup(<UsageMeter label="Score" value={150} max={100} />)
    expect(over).toContain('aria-valuenow="100"')
    expect(over).toContain('style="width:100%"')

    const zeroMax = renderToStaticMarkup(<UsageMeter label="Score" value={5} max={0} />)
    expect(zeroMax).toContain('aria-valuemax="0"')
    expect(zeroMax).toContain('aria-valuenow="0"')
    expect(zeroMax).toContain('style="width:0%"')
  })
})

describe('RelativeTime invalid date guard', () => {
  it('renders an em-dash placeholder instead of crashing on invalid dates', () => {
    const html = renderToStaticMarkup(<RelativeTime date="not-a-date" />)
    expect(html).toContain('>—</span>')
  })
})

describe('SaveBar disabled and accessibility', () => {
  it('disables only Save when disabled=true while keeping Discard active', () => {
    const html = renderToStaticMarkup(<SaveBar count={2} disabled onSave={() => {}} onDiscard={() => {}} />)
    expect(html).toContain('role="region"')
    expect(html).toContain('aria-label="Unsaved changes"')
    const buttons = [...html.matchAll(/<button([^>]*)>[\s\S]*?<\/button>/g)].map((m) => m[1].replace(/class="[^"]*"/, ''))
    expect(buttons).toHaveLength(2)
    expect(buttons[0]).not.toMatch(/\bdisabled\b/)
    expect(buttons[1]).toMatch(/\bdisabled\b/)
  })
})

describe('Design-system primitive invariants', () => {
  it('renders square rounded-sm avatars (Minecraft pixel heads)', () => {
    const html = renderToStaticMarkup(
      <Avatar>
        <AvatarFallback>SL</AvatarFallback>
      </Avatar>,
    )
    expect(html).toContain('rounded-sm')
    expect(html).not.toContain('rounded-full')
  })

  it('supports semantic Alert variants without crashing', () => {
    for (const variant of ['default', 'info', 'success', 'warning', 'destructive'] as const) {
      const html = renderToStaticMarkup(
        <Alert variant={variant}>
          <AlertDescription>Notice</AlertDescription>
        </Alert>,
      )
      expect(html).toContain('role="alert"')
    }
  })

  it('renders PaginationLink as a clean anchor without role="button"', () => {
    const html = renderToStaticMarkup(
      <Pagination>
        <PaginationContent>
          <PaginationItem>
            <PaginationLink href="/skinlib?page=2" isActive>
              2
            </PaginationLink>
          </PaginationItem>
        </PaginationContent>
      </Pagination>,
    )
    expect(html).toContain('aria-label="Pagination"')
    expect(html).toContain('<a ')
    expect(html).not.toContain('role="button"')
  })

  it('keeps button text color when merged with custom text-caption and text-body font sizes', () => {
    expect(cn('text-sm text-primary-foreground', 'text-caption')).toBe('text-primary-foreground text-caption')
    expect(cn('text-sm text-destructive-foreground', 'text-body')).toBe('text-destructive-foreground text-body')

    const smHtml = renderToStaticMarkup(<Button size="sm">Retry</Button>)
    expect(smHtml).toContain('text-primary-foreground')
    expect(smHtml).toContain('text-caption')

    const lgHtml = renderToStaticMarkup(
      <Button variant="destructive" size="lg">
        Delete
      </Button>,
    )
    expect(lgHtml).toContain('text-destructive-foreground')
    expect(lgHtml).toContain('text-body')
  })

  it('preserves sr-only accessible text on icon-only loading buttons', () => {
    const html = renderToStaticMarkup(
      <Button size="icon" loading>
        <span className="sr-only">Refresh</span>
      </Button>,
    )
    expect(html).toContain('Refresh')
    expect(html).toContain('aria-busy="true"')
  })

  it('places AlertAction in-flow rather than absolutely positioned', () => {
    const html = renderToStaticMarkup(
      <Alert variant="destructive">
        <AlertDescription>Load failed</AlertDescription>
        <AlertAction>
          <Button size="sm" variant="outline">
            Retry
          </Button>
        </AlertAction>
      </Alert>,
    )
    expect(html).toContain('data-slot="alert-action"')
    expect(html).not.toContain('top-2 right-2')
  })

  it('omits dangling hint aria-describedby on Dropzone when custom children replace the hint', () => {
    const html = renderToStaticMarkup(
      <Dropzone onFile={() => {}} hint="PNG only">
        <span>Preview ready</span>
      </Dropzone>,
    )
    expect(html).not.toContain('aria-describedby')
  })

  it('uses sr-only text instead of aria-label on TextureCard likes span', () => {
    const html = renderToStaticMarkup(
      <TextureCard
        texture={{ id: 1, name: 'Classic', type: 'steve', likes: 42 }}
        href="/skinlib/show/1"
        likesLabel="Likes"
      />,
    )
    expect(html).toContain('<span class="sr-only">Likes </span>42')
    expect(html).not.toContain('aria-label="Likes: 42"')
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

describe('PageHeader mobile flex-wrap layout', () => {
  it('assigns basis-64 to the heading column so action buttons wrap cleanly on narrow screens', () => {
    const html = renderToStaticMarkup(
      <PageHeader title="Plugins and themes" description="Manage extensions" actions={<button type="button">Upload</button>} />,
    )
    expect(html).toContain('basis-64')
  })
})
