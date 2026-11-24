import { ButtonLink, cn, Markdown } from '@skinloom/ui'
import Link from 'next/link'

import { safeHref, safeImageUrl } from '@/lib/safe-url'

import { CONTAINER, type Props, str } from './props'

/** props: content (Markdown, rendered by the server as contentHtml). */
export function MarkdownBlock({ props }: { props: Props }) {
  const html = str(props.contentHtml)
  if (!html) return null
  return (
    <section className={cn(CONTAINER, 'sl-block-markdown py-8')}>
      <div className="max-w-[45rem]">
        <Markdown html={html} />
      </div>
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
    <section className={cn(CONTAINER, 'sl-block-image py-8')}>
      {href ? (
        href.startsWith('/') ? (
          <Link href={href} className="block">
            {img}
          </Link>
        ) : (
          <a href={href} className="block" rel="noreferrer">
            {img}
          </a>
        )
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
    <section className={cn(CONTAINER, 'sl-block-buttons flex flex-wrap gap-3 py-8')}>
      {items.map((x, i) => (
        <ButtonLink
          key={`${x.href}-${i}`}
          variant={i === 0 ? 'default' : 'outline'}
          render={x.href.startsWith('/') ? <Link href={x.href} /> : <a href={x.href} rel="noreferrer" />}
        >
          {x.label}
        </ButtonLink>
      ))}
    </section>
  )
}
