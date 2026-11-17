import { PageHeader } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { type TextEntry, Translations } from '@/components/admin/translations'
import { flatten, type Messages } from '@/lib/messages'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('admin')
  return { title: t('translations') }
}

/** The default texts of every language, read here so the editor can show and search them. */
export default async function AdminTranslationsPage() {
  const [t, ta, zh, en] = await Promise.all([
    getTranslations('admin'),
    getTranslations('adminTranslations'),
    import('../../../../../messages/zh-CN.json').then((m) => m.default as Messages),
    import('../../../../../messages/en.json').then((m) => m.default as Messages),
  ])
  const english = new Map(flatten(en))
  // A key one language lacks (the English setting help, which falls back to the server's
  // description) gets no default there: an override for it would never show.
  const entries: TextEntry[] = flatten(zh).map(([key, text]) => {
    const en = english.get(key)
    return { key, defaults: en === undefined ? { 'zh-CN': text } : { 'zh-CN': text, en } }
  })
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={t('translations')} description={ta('intro')} />
      <Translations entries={entries} />
    </div>
  )
}
