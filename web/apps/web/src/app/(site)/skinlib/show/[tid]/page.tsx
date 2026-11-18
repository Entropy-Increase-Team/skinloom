import { ApiError, getTexture, getTextureRules, type TextureDetail, type TextureRules } from '@skinloom/api-client'
import { Badge, ButtonLink, CopyField, PixelBackdrop, previewUrl, SkinViewer, TexturePreview, UserAvatar } from '@skinloom/ui'
import { ArrowLeft, Download, Lock } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'
import { getFormatter, getTranslations } from 'next-intl/server'
import { cache } from 'react'

import { WearOnPlayer } from '@/components/player/wear-on-player'
import { ReportButton } from '@/components/texture/report-button'
import { FavoriteButton, OwnerMenu } from '@/components/texture/texture-actions'
import { UserBadges } from '@/components/user/user-badges'
import { getBootstrap } from '@/lib/bootstrap'
import { kb } from '@/lib/skin'
import { layoutOf } from '@/lib/theme'
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
  // Link previews show the texture: front and back of a skin, or the cape (public
  // textures only; the image of a private one is not for everyone).
  const images = x.public ? [{ url: `/preview/${x.id}?height=256&png`, alt: x.name }] : undefined
  return {
    title: x.name,
    description,
    openGraph: { title: x.name, description, images },
    twitter: { card: images ? 'summary_large_image' : 'summary', title: x.name, description, images },
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
        <Lock className="size-8 text-muted-foreground" aria-hidden />
        <p className="text-body">{loaded.status === 403 ? t('privateNotice') : t('loadFailed')}</p>
        <ButtonLink variant="outline" render={<Link href="/skinlib" />}>
          {t('back')}
        </ButtonLink>
      </div>
    )
  }
  const x = loaded.texture
  const [boot, format, rules, viewer] = await Promise.all([
    getBootstrap(),
    getFormatter(),
    getTextureRules().then((r) => r.data as TextureRules, () => null),
    getViewer(),
  ])
  const canManage = x.mine || (viewer !== null && viewer.role !== 'user')
  const src = `/textures/${x.hash}`
  const stacked = layoutOf(boot.theme.layout, 'texture') === 'stacked'

  return (
    <div className={stacked ? 'mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8' : 'mx-auto flex w-full max-w-[75rem] flex-col gap-4 px-4 py-6 sm:px-6 lg:px-8 lg:py-8'}>
      <ButtonLink variant="ghost" size="sm" className="-ml-2.5 w-fit text-muted-foreground" render={<Link href="/skinlib" />}>
        <ArrowLeft aria-hidden />
        {t('back')}
      </ButtonLink>
      <div className={stacked ? 'flex flex-col gap-6' : 'grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_22rem] lg:gap-10'}>
        {/* The single view: interactive 3D/2D player preview (design.md §8.1). */}
        <PixelBackdrop className={stacked ? 'h-[24rem] rounded-xl border sm:h-[30rem]' : 'h-[24rem] rounded-xl border sm:h-[30rem] lg:h-[36rem]'}>
          <SkinViewer
            skin={x.type === 'cape' ? null : src}
            cape={x.type === 'cape' ? src : null}
            previewSrc={previewUrl({ id: x.id }, 256)}
            model={x.type === 'alex' ? 'slim' : 'default'}
            autoRotate
            controls
            label={t('preview', { name: x.name })}
          />
        </PixelBackdrop>

        <div className="flex flex-col gap-6">
          <div className="flex items-start gap-3">
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <h1 className="text-2xl font-medium break-words">{x.name}</h1>
              <div className="flex flex-wrap gap-1.5">
                <Badge variant="secondary">{t(`types.${x.type}`)}</Badge>
                {!x.public && (
                  <Badge variant="outline">
                    <Lock aria-hidden />
                    {t('private')}
                  </Badge>
                )}
                {x.inCloset && <Badge variant="outline">{t('inCloset')}</Badge>}
              </div>
            </div>
            {canManage && rules && <OwnerMenu texture={x} rates={rules.rates} charged={x.mine} />}
          </div>

          {x.uploader ? (
            <Link
              href={`/skinlib?q=${encodeURIComponent(x.uploader.nickname)}`}
              className="group -mx-2 flex w-fit items-center gap-3 rounded-lg px-2 py-1.5 transition-colors hover:bg-muted"
            >
              <UserAvatar of={{ user: x.uploader.id }} size={36} />
              <div className="flex min-w-0 flex-col">
                <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                  <span className="truncate text-sm font-medium group-hover:text-primary">{x.uploader.nickname}</span>
                  <UserBadges badges={x.uploader.badges} />
                </span>
                <span className="text-caption text-muted-foreground">
                  {t('uploadedOn', { date: format.dateTime(new Date(x.createdAt), { dateStyle: 'medium' }) })}
                </span>
              </div>
            </Link>
          ) : (
            <div className="flex items-center gap-3">
              <span className="size-9 shrink-0 rounded-sm bg-muted" aria-hidden />
              <div className="flex min-w-0 flex-col">
                <span className="truncate text-sm font-medium">{t('deletedUser')}</span>
                <span className="text-caption text-muted-foreground">
                  {t('uploadedOn', { date: format.dateTime(new Date(x.createdAt), { dateStyle: 'medium' }) })}
                </span>
              </div>
            </div>
          )}

          <div className="flex flex-col gap-2.5">
            {viewer && rules && <FavoriteButton texture={x} rates={rules.rates} />}
            {!viewer && (
              <ButtonLink size="lg" render={<Link href={`/auth/login?next=${encodeURIComponent(`/skinlib/show/${x.id}`)}`} />}>
                {t('signInToFavorite')}
              </ButtonLink>
            )}
            {x.inCloset && <WearOnPlayer textureId={x.id} kind={x.type === 'cape' ? 'cape' : 'skin'} />}
            {viewer && rules?.allowDownload && (
              <ButtonLink variant="ghost" className="text-muted-foreground" render={<a href={`/raw/${x.id}.png`} download={`${x.name}.png`} />}>
                <Download />
                {t('download')}
              </ButtonLink>
            )}
          </div>

          <dl className="grid grid-cols-2 border-y text-sm">
            {(
              [
                [t('likes'), format.number(x.likes)],
                [t('size'), `${kb(x.size)} KB`],
              ] as const
            ).map(([label, value], i) => (
              <div key={label} className={i > 0 ? 'flex flex-col-reverse gap-0.5 border-l py-3 pl-4' : 'flex flex-col-reverse gap-0.5 py-3'}>
                <dt className="text-caption text-muted-foreground">{label}</dt>
                <dd className="font-medium tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>

          {/* The dual view, as launchers and link previews show it. */}
          <div className="flex flex-col gap-2">
            <span className="text-caption text-muted-foreground">{x.type === 'cape' ? t('flat') : t('frontBack')}</span>
            <PixelBackdrop className="h-36 rounded-lg border py-3">
              <TexturePreview texture={{ id: x.id }} type={x.type} height={112} alt="" />
            </PixelBackdrop>
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-caption text-muted-foreground">{t('hash')}</span>
            <CopyField value={x.hash} label={t('hash')} />
          </div>

          {viewer && !x.mine && rules && <ReportButton textureId={x.id} rates={rules.rates} />}
        </div>
      </div>
    </div>
  )
}
