import { Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { Megaphone } from 'lucide-react'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

/** Shows the site.announcement setting; renders nothing when it is empty. */
export async function AnnouncementBlock() {
  const boot = await getBootstrap()
  const text = boot.site.announcement.trim()
  if (!text) return null
  const t = await getTranslations('announcement')
  return (
    <section className="mx-auto max-w-6xl px-4 pt-10">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Megaphone className="size-4 text-primary" aria-hidden />
            {t('title')}
          </CardTitle>
        </CardHeader>
        {/* Markdown rendering (sanitized on the server) arrives with the content module in M2. */}
        <CardContent className="whitespace-pre-line text-sm leading-relaxed">{text}</CardContent>
      </Card>
    </section>
  )
}
