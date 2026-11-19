import { Heart, Lock } from 'lucide-react'
import type * as React from 'react'

import { cn } from '../../lib/utils'
import { PixelBackdrop } from './pixel-backdrop'
import { type TextureType, TexturePreview } from './texture-preview'
import { UserAvatar } from './user-avatar'

export interface TextureCardTexture {
  id: number
  name: string
  type: TextureType
  likes: number
  public?: boolean
  uploader?: { name: string; userId?: number; avatarVersion?: number } | null
}

/** Preview heights for the skin library's densities (theme layout `skinlib`). */
const HEIGHTS = { comfortable: 150, compact: 104 } as const

/**
 * A texture in a grid (web/design.md §10.3): the dual view on the pixel backdrop, the
 * name, the uploader and the favourite count. The whole card is one link. Themes restyle
 * it through .sl-texture-card.
 */
export function TextureCard({
  texture,
  href,
  linkAs: LinkAs = 'a',
  density = 'comfortable',
  meta,
  badges,
  privateLabel,
  likesLabel,
  className,
}: {
  texture: TextureCardTexture
  href: string
  /** The link component, e.g. Next's Link; a plain <a> by default. */
  linkAs?: React.ElementType
  density?: keyof typeof HEIGHTS
  /** Replaces the uploader line, e.g. with the type and the uploader in words. */
  meta?: React.ReactNode
  /** Inline badges rendered alongside the title (e.g. skinlib.card.badges plugin slot). */
  badges?: React.ReactNode
  privateLabel?: string
  likesLabel?: string
  className?: string
}) {
  const h = HEIGHTS[density]
  return (
    <LinkAs
      href={href}
      data-slot="texture-card"
      data-density={density}
      className={cn(
        'sl-texture-card group flex flex-col rounded-xl border bg-card p-1 text-card-foreground outline-none transition-colors duration-150 hover:border-input hover:bg-muted/40 focus-visible:ring-3 focus-visible:ring-ring/50',
        className,
      )}
    >
      <PixelBackdrop className="relative" style={{ height: h + (density === 'compact' ? 24 : 40) }}>
        <TexturePreview texture={{ id: texture.id }} type={texture.type} height={h} alt="" />
        {texture.public === false && (
          <span className="absolute top-1.5 left-1.5 inline-flex items-center gap-1 rounded-sm bg-card/90 px-1.5 py-0.5 text-xs text-muted-foreground">
            <Lock className="size-3" aria-hidden />
            {privateLabel}
          </span>
        )}
      </PixelBackdrop>
      <div className={cn('flex flex-col gap-1.5', density === 'compact' ? 'px-1.5 pt-2 pb-1.5' : 'px-2 pt-2.5 pb-2')}>
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{texture.name}</span>
          {badges ? (
            <span className="flex min-w-0 max-w-[45%] shrink-0 items-center gap-1 overflow-hidden">{badges}</span>
          ) : null}
        </div>
        <span className="flex items-center gap-1.5 text-xs text-faint-foreground">
          {meta ?? (
            <>
              {texture.uploader?.userId !== undefined && (
                <UserAvatar of={{ user: texture.uploader.userId, version: texture.uploader.avatarVersion }} size={16} />
              )}
              <span className="truncate">{texture.uploader?.name}</span>
            </>
          )}
          <span className="flex-1" />
          <span className="inline-flex shrink-0 items-center gap-1 tabular-nums" title={likesLabel}>
            <Heart className="size-3" aria-hidden />
            {texture.likes}
          </span>
        </span>
      </div>
    </LinkAs>
  )
}

/**
 * A game character (web/design.md §10.3): what it wears in the dual view, its name, its
 * UUID and a few facts, with the actions underneath. Framed like TextureCard; with
 * `href` (and no actions) the whole card is one link. Themes restyle it through
 * .sl-player-card.
 */
export function PlayerCard({
  name,
  uuid,
  skin,
  details,
  actions,
  href,
  linkAs: LinkAs = 'a',
  size = 'default',
  className,
}: {
  name: string
  uuid?: React.ReactNode
  /** The skin it wears, if any. */
  skin?: { id: number; type: 'steve' | 'alex' } | null
  /** Label and value pairs: 皮肤 织工长袍, 披风 —. */
  details?: { label: React.ReactNode; value: React.ReactNode }[]
  actions?: React.ReactNode
  /** Makes the card a link (only without actions: links cannot hold buttons). */
  href?: string
  linkAs?: React.ElementType
  /** sm: a shorter figure, for overviews. */
  size?: 'default' | 'sm'
  className?: string
}) {
  const h = size === 'sm' ? 136 : 176
  const body = (
    <>
      <PixelBackdrop style={{ height: h + 40 }}>{skin && <TexturePreview texture={{ id: skin.id }} type={skin.type} height={h} alt="" />}</PixelBackdrop>
      <div className="flex flex-col gap-2.5 px-2 pt-2.5 pb-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate text-[0.9375rem] font-medium">{name}</span>
          {uuid && <span className="truncate font-mono text-xs text-faint-foreground">{uuid}</span>}
        </div>
        {details && details.length > 0 && (
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-2.5 gap-y-1 text-caption">
            {details.map((d, i) => (
              <div key={i} className="contents">
                <dt className="text-faint-foreground">{d.label}</dt>
                <dd className="truncate">{d.value}</dd>
              </div>
            ))}
          </dl>
        )}
        {actions && <div className="-mx-1 flex flex-wrap items-center gap-1">{actions}</div>}
      </div>
    </>
  )
  const frame = 'sl-player-card flex flex-col rounded-xl border bg-card p-1 text-card-foreground'
  if (href && !actions) {
    return (
      <LinkAs
        href={href}
        data-slot="player-card"
        className={cn(frame, 'outline-none transition-colors duration-150 hover:border-input hover:bg-muted/40 focus-visible:ring-3 focus-visible:ring-ring/50', className)}
      >
        {body}
      </LinkAs>
    )
  }
  return (
    <div data-slot="player-card" className={cn(frame, className)}>
      {body}
    </div>
  )
}
