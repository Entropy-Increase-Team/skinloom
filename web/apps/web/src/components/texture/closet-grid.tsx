'use client'

import { type ClosetItemView, type ClosetPage, listCloset } from '@skinloom/api-client'
import { Button, Input } from '@skinloom/ui'
import Link from 'next/link'
import { useTranslations } from 'next-intl'
import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { TextureCard } from './texture-card'

type Category = 'skin' | 'cape'

/** The signed-in user's closet: skins or capes, searchable by item name. */
export function ClosetGrid() {
  const t = useTranslations('closet')
  const [category, setCategory] = useState<Category>('skin')
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<ClosetItemView[] | null>(null)
  const [next, setNext] = useState(0)
  const [loading, setLoading] = useState(false)
  const [failed, setFailed] = useState(false)
  // Answers can arrive out of order when the tab or search changes quickly: only the
  // latest request may update the list.
  const latest = useRef(0)

  const load = useCallback(
    async (before?: number) => {
      const seq = ++latest.current
      setLoading(true)
      setFailed(false)
      try {
        const page = (await listCloset({ category, ...(query ? { q: query } : {}), ...(before ? { before } : {}) })).data as ClosetPage
        if (seq !== latest.current) return
        setItems((list) => (before && list ? [...list, ...page.items] : page.items))
        setNext(page.next)
      } catch {
        if (seq === latest.current) setFailed(true)
      } finally {
        if (seq === latest.current) setLoading(false)
      }
    },
    [category, query],
  )
  useEffect(() => {
    void load()
  }, [load])

  function onSearch(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setQuery(String(new FormData(e.currentTarget).get('q') ?? '').trim())
  }

  const tab = (active: boolean) =>
    `rounded-full px-3 py-1 text-sm transition-colors ${active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-4">
        <div className="flex gap-1" role="tablist">
          {(['skin', 'cape'] as const).map((c) => (
            <button key={c} type="button" role="tab" aria-selected={category === c} className={tab(category === c)} onClick={() => setCategory(c)}>
              {t(c === 'skin' ? 'skins' : 'capes')}
            </button>
          ))}
        </div>
        <form onSubmit={onSearch}>
          <Input name="q" placeholder={t('search')} aria-label={t('search')} maxLength={50} className="w-48" />
        </form>
      </div>
      {failed && <p className="text-center text-sm text-destructive">{t('loadFailed')}</p>}
      {items && items.length === 0 && !loading && (
        <div className="flex flex-col items-center gap-3 py-16 text-muted-foreground">
          <p>{query ? t('noMatch') : t('empty')}</p>
          <div className="flex gap-2">
            <Button asChild variant="outline">
              <Link href="/skinlib">{t('browse')}</Link>
            </Button>
            <Button asChild>
              <Link href="/skinlib/upload">{t('upload')}</Link>
            </Button>
          </div>
        </div>
      )}
      {items && items.length > 0 && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {items.map((it) => (
            <TextureCard key={it.id} texture={it.texture} title={it.name} />
          ))}
        </div>
      )}
      {next > 0 && (
        <Button variant="outline" className="self-center" disabled={loading} onClick={() => void load(next)}>
          {t('more')}
        </Button>
      )}
    </div>
  )
}
