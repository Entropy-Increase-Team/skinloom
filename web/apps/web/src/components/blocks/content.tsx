import { Button } from '@skinloom/ui'
import Link from 'next/link'

import { Markdown } from '@/components/markdown'
import { safeHref, safeImageUrl } from '@/lib/safe-url'

import { type Props, str } from './props'

/** props: content (Markdown, rendered by the server as contentHtml). */
export function MarkdownBlock({ props }: { props: Props }) {
  const html = str(props.contentHtml)
  if (!html) return null
  return (
    <section className="mx-auto max-w-6xl px-4 py-8">
      <Markdown html={html} className="text-base" />
    </section>
  )
}

/** props: src (image URL), alt, href (where clicking it leads). */
export function ImageBlock({ props }: { props: Props }) {
  const src = safeImageUrl(props.src)
  if (!src) return null
  const href = safeHref(props.href, '')
  // An administrator's URL of any size and host: next/image would need each host configured.
  const img = <img src={src} alt={str(props.alt) ?? ''} className="h-auto w-full rounded-xl" loading="lazy" />
  return (
    <section className="mx-auto max-w-6xl px-4 py-8">
      {href ? (
        <a href={href} className="block" rel={href.startsWith('/') ? undefined : 'noreferrer'}>
          {img}
        </a>
      ) : (
        img
      )}
    </section>
  )
}

/** props: items, a list of {label, href}; the first is the primary button. */
export function ButtonsBlock({ props }: { props: Props }) {
  const items = (Array.isArray(props.items) ? props.items : [])
    .map((x: { label?: unknown; href?: unknown }) => ({ label: str(x?.label), href: safeHref(x?.href, '') }))
    .filter((x): x is { label: string; href: string } => !!x.label && x.href !== '')
  if (items.length === 0) return null
  return (
    <section className="mx-auto flex max-w-6xl flex-wrap gap-3 px-4 py-8">
      {items.map((x, i) => (
        <Button key={`${x.href}-${i}`} asChild variant={i === 0 ? 'default' : 'outline'}>
          {x.href.startsWith('/') ? (
            <Link href={x.href}>{x.label}</Link>
          ) : (
            <a href={x.href} rel="noreferrer">
              {x.label}
            </a>
          )}
        </Button>
      ))}
    </section>
  )
}
