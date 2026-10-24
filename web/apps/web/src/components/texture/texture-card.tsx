import type { TextureView } from '@skinloom/api-client'
import { Badge } from '@skinloom/ui'
import { Heart } from 'lucide-react'
import Link from 'next/link'
import { useTranslations } from 'next-intl'

import { TexturePreview } from './texture-preview'

/** A texture in a grid: preview, name, type, uploader and likes. */
export function TextureCard({ texture, title }: { texture: TextureView; title?: string }) {
  const t = useTranslations('texture')
  return (
    <Link
      href={`/skinlib/show/${texture.id}`}
      className="group flex flex-col overflow-hidden rounded-lg border bg-card transition-shadow hover:shadow-md focus-visible:outline-2 focus-visible:outline-ring"
    >
      <div className="flex h-44 items-center justify-center bg-muted/40">
        <TexturePreview src={`/textures/${texture.hash}`} type={texture.type} height={144} className="transition-transform group-hover:scale-105" />
      </div>
      <div className="flex flex-col gap-1 p-3">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium">{title ?? texture.name}</span>
          {!texture.public && <Badge variant="secondary">{t('private')}</Badge>}
        </div>
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span className="truncate">
            {t(`types.${texture.type}`)} · {texture.uploader?.nickname ?? t('deletedUser')}
          </span>
          <span className="flex shrink-0 items-center gap-1" title={t('likes')}>
            <Heart className="size-3" aria-hidden />
            {texture.likes}
          </span>
        </div>
      </div>
    </Link>
  )
}
