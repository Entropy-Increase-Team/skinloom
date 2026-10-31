'use client'

import { listSettings, type SettingsList, updateSettings } from '@skinloom/api-client'
import { Badge, Button, Card, CardContent, Input, Label } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { overrideProblem, savedOverrides } from '@/lib/messages'
import { problemOf } from '@/lib/problem'

import { equal, LOCALES } from './settings-form'

/** One interface text: its message key and the shipped wording in each language that has it. */
export interface TextEntry {
  key: string
  defaults: Partial<Record<string, string>>
}

type Overrides = Record<string, Record<string, string>>

const PAGE = 40

/**
 * Rewords any interface text (i18n.overrides, BS language_lines): search by key or
 * text, type a replacement per language, save. Texts keep the default's placeholders.
 */
export function Translations({ entries }: { entries: TextEntry[] }) {
  const t = useTranslations('adminTranslations')
  const tl = useTranslations('adminSettings.locales')
  const router = useRouter()
  const [saved, setSaved] = useState<Overrides | null>(null)
  const [draft, setDraft] = useState<Overrides>({})
  const [query, setQuery] = useState('')
  const [changedOnly, setChangedOnly] = useState(false)
  const [shown, setShown] = useState(PAGE)
  const [pending, setPending] = useState(false)
  const [status, setStatus] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Once: a save refreshes the page (router.refresh), which hands out a new t, and
  // loading again then would replace what was typed since.
  useEffect(() => {
    listSettings()
      .then((res) => {
        const v = (res.data as SettingsList).settings.find((s) => s.key === 'i18n.overrides')?.value
        const o = (v ?? {}) as Overrides
        setSaved(o)
        setDraft(o)
      })
      .catch(() => setError(t('errors.load')))
  }, [])

  if (!saved) return error ? <FormError message={error} /> : <p className="text-sm text-muted-foreground">{t('loading')}</p>

  const text = (locale: string, key: string) => draft[locale]?.[key] ?? ''
  const q = query.trim().toLowerCase()
  const matches = entries.filter(
    (e) =>
      (!changedOnly || LOCALES.some((l) => text(l, e.key) !== '')) &&
      (q === '' || e.key.toLowerCase().includes(q) || LOCALES.some((l) => `${e.defaults[l]} ${text(l, e.key)}`.toLowerCase().includes(q))),
  )

  const problems = entries.flatMap((e) =>
    LOCALES.flatMap((l) => {
      const v = text(l, e.key)
      const p = v.trim() === '' ? null : overrideProblem(e.defaults[l] ?? '', v)
      return p ? [{ key: e.key, locale: l, problem: p }] : []
    }),
  )
  const next = savedOverrides(draft)
  const dirty = !equal(next ?? {}, savedOverrides(saved) ?? {})
  const count = Object.values(next ?? {}).reduce((n, m) => n + Object.keys(m).length, 0)

  function set(locale: string, key: string, value: string) {
    setDraft((d) => ({ ...d, [locale]: { ...(d[locale] ?? {}), [key]: value } }))
    setStatus(null)
  }

  async function save() {
    setPending(true)
    setError(null)
    try {
      const list = (await updateSettings({ 'i18n.overrides': next })).data as SettingsList
      const v = (list.settings.find((s) => s.key === 'i18n.overrides')?.value ?? {}) as Overrides
      setSaved(v)
      setDraft(v)
      setStatus(t('saved'))
      router.refresh() // this page's own texts
    } catch (err) {
      const detail = problemOf(err).fields['i18n.overrides']
      setError(detail ? t('errors.invalid', { detail }) : t('errors.save'))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t('intro')}</p>
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="text-search">{t('search')}</Label>
          <Input
            id="text-search"
            value={query}
            placeholder={t('searchHint')}
            onChange={(e) => {
              setQuery(e.target.value)
              setShown(PAGE)
            }}
            className="w-72"
          />
        </div>
        <label className="flex items-center gap-2 pb-2 text-sm">
          <input type="checkbox" checked={changedOnly} onChange={(e) => setChangedOnly(e.target.checked)} className="size-4" />
          {t('changedOnly')}
        </label>
        <span className="pb-2 text-sm text-muted-foreground">{t('count', { count })}</span>
      </div>

      <div className="flex flex-col gap-3">
        {matches.slice(0, shown).map((e) => (
          <Card key={e.key} className="py-4">
            <CardContent className="flex flex-col gap-3">
              <code className="font-mono text-xs text-muted-foreground">{e.key}</code>
              <div className="grid gap-3 md:grid-cols-2">
                {LOCALES.map((l) => {
                  const v = text(l, e.key)
                  const shipped = e.defaults[l]
                  if (shipped === undefined && v === '') {
                    return (
                      <div key={l} className="flex flex-col gap-1">
                        <div className="text-xs text-muted-foreground">{tl(l)}</div>
                        <p className="text-sm text-muted-foreground">{t('noDefault')}</p>
                      </div>
                    )
                  }
                  const p = v.trim() === '' ? null : overrideProblem(shipped ?? '', v)
                  return (
                    <div key={l} className="flex flex-col gap-1">
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        {tl(l)}
                        {v.trim() !== '' && <Badge variant="secondary">{t('changed')}</Badge>}
                      </div>
                      <p className="text-sm">{shipped || '—'}</p>
                      <Input
                        aria-label={`${e.key} ${tl(l)}`}
                        value={v}
                        placeholder={t('keep')}
                        aria-invalid={p ? true : undefined}
                        onChange={(ev) => set(l, e.key, ev.target.value)}
                      />
                      {p && <p className="text-xs text-destructive">{t(`problems.${p}`)}</p>}
                    </div>
                  )
                })}
              </div>
            </CardContent>
          </Card>
        ))}
        {matches.length === 0 && <p className="py-8 text-center text-sm text-muted-foreground">{t('none')}</p>}
        {matches.length > shown && (
          <Button variant="outline" className="self-center" onClick={() => setShown((n) => n + PAGE)}>
            {t('more', { left: matches.length - shown })}
          </Button>
        )}
      </div>

      <div className="sticky bottom-0 flex flex-wrap items-center gap-3 border-t bg-background/95 py-3">
        <Button disabled={pending || !dirty || problems.length > 0} onClick={() => void save()}>
          {t('save')}
        </Button>
        {problems.length > 0 && <span className="text-xs text-destructive">{t('fixFirst', { count: problems.length })}</span>}
        {dirty && problems.length === 0 && <span className="text-xs text-muted-foreground">{t('unsaved')}</span>}
        {status && !dirty && (
          <span role="status" className="text-sm text-primary">
            {status}
          </span>
        )}
        <FormError message={error} />
      </div>
    </div>
  )
}
