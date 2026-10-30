import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import { headers } from 'next/headers'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

import { CopyButton } from './copy-button'

/** authlib-injector setup hint with this site's Yggdrasil API address. */
export async function LauncherGuideBlock() {
  const boot = await getBootstrap()
  const t = await getTranslations('launcher')
  let origin = boot.site.url.replace(/\/+$/, '')
  if (!origin) {
    const h = await headers()
    const host = h.get('x-forwarded-host') ?? h.get('host') ?? 'localhost'
    const proto = h.get('x-forwarded-proto') ?? 'http'
    origin = `${proto}://${host}`
  }
  const address = `${origin}/api/yggdrasil`
  return (
    <section className="mx-auto max-w-6xl px-4 pb-16">
      <Card>
        <CardHeader>
          <CardTitle>{t('title')}</CardTitle>
          <CardDescription>{t('body')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <code className="rounded-md bg-muted px-3 py-2 font-mono text-sm">{address}</code>
            <CopyButton value={address} label={t('copy')} copiedLabel={t('copied')} />
            {/* authlib-injector's drag-and-drop: launchers that support it add the server when this is dropped on them. */}
            <a
              href={`authlib-injector:yggdrasil-server:${encodeURIComponent(address)}`}
              draggable
              className="inline-flex h-8 cursor-grab items-center rounded-md border border-dashed px-3 text-sm text-muted-foreground hover:text-foreground"
              title={t('dragHint')}
            >
              {t('drag')}
            </a>
          </div>
          <p className="text-xs text-muted-foreground">{t('note')}</p>
        </CardContent>
      </Card>
    </section>
  )
}
