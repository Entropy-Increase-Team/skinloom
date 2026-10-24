import { ApiError, getTexture, getTextureRules, type TextureDetail, type TextureRules } from '@skinloom/api-client'
import { Badge, Button, Card, CardContent } from '@skinloom/ui'
import { Download, Heart, Lock } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { getFormatter, getTranslations } from 'next-intl/server'
import { cache } from 'react'

import { WearOnPlayer } from '@/components/player/wear-on-player'
import { SkinViewer3D } from '@/components/texture/skin-viewer'
import { FavoriteButton, OwnerControls } from '@/components/texture/texture-actions'
import { kb } from '@/lib/skin'
import { getViewer } from '@/lib/viewer'
import { asVisitor } from '@/lib/visitor'

type Loaded = { texture: TextureDetail } | { status: number }

/** One request per page render, shared by the metadata and the page. */
const load = cache(async (tid: string): Promise<Loaded> => {
  const id = Number(tid)
  if (!Number.isSafeInteger(id) || id <= 0) return { status: 404 }
  try {
    const res = await getTexture(id, await asVisitor())
    return { texture: res.data as TextureDetail }
  } catch (err) {
    return { status: err instanceof ApiError ? err.status : 500 }
  }
})

export async function generateMetadata({ params }: { params: Promise<{ tid: string }> }): Promise<Metadata> {
  const loaded = await load((await params).tid)
  const t = await getTranslations('texture')
  if (!('texture' in loaded)) return { title: t('notAvailable'), robots: { index: false } }
  const x = loaded.texture
  const description = `${t(`types.${x.type}`)} · ${x.uploader?.nickname ?? t('deletedUser')}`
  return {
    title: x.name,
    description,
    openGraph: { title: x.name, description },
    robots: x.public ? undefined : { index: false },
  }
}

export default async function TexturePage({ params }: { params: Promise<{ tid: string }> }) {
  const loaded = await load((await params).tid)
  const t = await getTranslations('texture')
  if (!('texture' in loaded)) {
    if (loaded.status === 404) notFound()
    return (
      <div className="mx-auto flex max-w-md flex-col items-center gap-4 px-4 py-24 text-center">
        <Lock className="size-10 text-muted-foreground" aria-hidden />
        <p>{loaded.status === 403 ? t('privateNotice') : t('loadFailed')}</p>
        <Button asChild variant="outline">
          <Link href="/skinlib">{t('back')}</Link>
        </Button>
      </div>
    )
  }
  const x = loaded.texture
  const [format, rules, viewer] = await Promise.all([
    getFormatter(),
    getTextureRules().then((r) => r.data as TextureRules, () => null),
    getViewer(),
  ])
  const canManage = x.mine || (viewer !== null && viewer.role !== 'user')

  return (
    <div className="mx-auto grid max-w-5xl gap-6 px-4 py-8 md:grid-cols-[1fr_20rem]">
      <div className="h-[28rem] overflow-hidden rounded-lg border bg-muted/40">
        <SkinViewer3D hash={x.hash} type={x.type} label={t('preview', { name: x.name })} />
      </div>
      <Card className="self-start">
        <CardContent className="flex flex-col gap-4 pt-6">
          <div>
            <h1 className="break-words text-2xl font-bold tracking-tight">{x.name}</h1>
            <div className="mt-2 flex flex-wrap gap-2">
              <Badge variant="secondary">{t(`types.${x.type}`)}</Badge>
              {!x.public && <Badge variant="outline">{t('private')}</Badge>}
              {x.inCloset && <Badge>{t('inCloset')}</Badge>}
            </div>
          </div>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">{t('uploader')}</dt>
            <dd className="truncate">{x.uploader?.nickname ?? t('deletedUser')}</dd>
            <dt className="text-muted-foreground">{t('likes')}</dt>
            <dd className="flex items-center gap-1">
              <Heart className="size-3.5" aria-hidden />
              {x.likes}
            </dd>
            <dt className="text-muted-foreground">{t('size')}</dt>
            <dd>{kb(x.size)} KB</dd>
            <dt className="text-muted-foreground">{t('uploaded')}</dt>
            <dd>{format.dateTime(new Date(x.createdAt), { dateStyle: 'medium' })}</dd>
            <dt className="text-muted-foreground">{t('hash')}</dt>
            <dd className="truncate font-mono text-xs" title={x.hash}>
              {x.hash}
            </dd>
          </dl>
          {viewer && rules && <FavoriteButton texture={x} rates={rules.rates} />}
          {x.inCloset && <WearOnPlayer textureId={x.id} kind={x.type === 'cape' ? 'cape' : 'skin'} />}
          {rules?.allowDownload && (
            <Button asChild variant="outline">
              <a href={`/raw/${x.id}.png`} download={`${x.name}.png`}>
                <Download />
                {t('download')}
              </a>
            </Button>
          )}
          {canManage && rules && <OwnerControls texture={x} rates={rules.rates} charged={x.mine} />}
        </CardContent>
      </Card>
    </div>
  )
}
