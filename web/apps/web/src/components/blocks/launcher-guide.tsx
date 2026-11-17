import { ButtonLink, cn, CopyField } from '@skinloom/ui'
import { GripVertical } from 'lucide-react'
import { headers } from 'next/headers'
import type * as React from 'react'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

import { CONTAINER } from './props'

/** How to use this site from a launcher (authlib-injector): three steps, the address to copy in the second. */
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
  const step = (n: number, title: string, body: React.ReactNode) => (
    <li className="flex flex-col gap-2.5 border-t pt-5">
      <span className="font-mono text-xs text-faint-foreground">{String(n).padStart(2, '0')}</span>
      <h3 className="text-base font-medium">{title}</h3>
      {body}
    </li>
  )
  return (
    <section className="sl-block-launcher-guide border-y bg-card">
      <div className={cn(CONTAINER, 'flex flex-col gap-8 py-12 lg:py-16')}>
        <div className="flex flex-col gap-1.5">
          <h2 className="text-xl font-medium sm:text-[1.375rem]">{t('title')}</h2>
          <p className="text-sm text-muted-foreground">{t('body')}</p>
        </div>
        <ol className="grid gap-6 md:grid-cols-3">
          {step(1, t('step1'), <p className="text-sm leading-relaxed text-muted-foreground">{t('step1Body')}</p>)}
          {step(
            2,
            t('step2'),
            <div className="flex flex-col gap-2">
              <CopyField value={address} />
              {/* authlib-injector's drag-and-drop: launchers that support it add the server when this is dropped on them. */}
              <ButtonLink
                variant="outline"
                size="sm"
                draggable
                className="w-fit cursor-grab border-dashed active:cursor-grabbing"
                title={t('dragHint')}
                render={<a href={`authlib-injector:yggdrasil-server:${encodeURIComponent(address)}`} />}
              >
                <GripVertical className="text-muted-foreground" aria-hidden />
                {t('drag')}
              </ButtonLink>
            </div>,
          )}
          {step(3, t('step3'), <p className="text-sm leading-relaxed text-muted-foreground">{t('step3Body')}</p>)}
        </ol>
      </div>
    </section>
  )
}
