import { Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { Megaphone } from 'lucide-react'
import { getTranslations } from 'next-intl/server'

import { Markdown } from '@/components/markdown'
import { getBootstrap } from '@/lib/bootstrap'

/** Shows the site.announcement setting; renders nothing when it is empty. */
export async function AnnouncementBlock() {
  const boot = await getBootstrap()
  if (!boot.site.announcement.trim()) return null
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
        <CardContent>
          <Markdown html={boot.site.announcementHtml} />
        </CardContent>
      </Card>
    </section>
  )
}
