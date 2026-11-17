import { cn, Markdown } from '@skinloom/ui'
import { Megaphone } from 'lucide-react'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

import { CONTAINER } from './props'

/** The site.announcement setting as a quiet notice line (design.md §6.1); nothing when it is empty. */
export async function AnnouncementBlock() {
  const boot = await getBootstrap()
  if (!boot.site.announcement.trim()) return null
  const t = await getTranslations('announcement')
  return (
    <section className={cn(CONTAINER, 'sl-block-announcement py-3')} aria-label={t('title')}>
      <div className="flex items-start gap-3 rounded-xl border bg-card px-4 py-3">
        <Megaphone className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
        <Markdown html={boot.site.announcementHtml} className="min-w-0 flex-1 text-sm" />
      </div>
    </section>
  )
}
