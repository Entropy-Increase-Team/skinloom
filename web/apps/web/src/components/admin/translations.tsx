'use client'

import { listSettings, type SettingsList, updateSettings } from '@skinloom/api-client'
import { Badge, Button, Checkbox, Input, InputGroup, InputGroupAddon, InputGroupInput, Label, SaveBar, Skeleton, toast } from '@skinloom/ui'
import { Search } from 'lucide-react'
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

  if (!saved) return error ? <FormError message={error} /> : <Skeleton className="h-96 rounded-xl" />

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
  }

  async function save() {
    setPending(true)
    setError(null)
    try {
      const list = (await updateSettings({ 'i18n.overrides': next })).data as SettingsList
      const v = (list.settings.find((s) => s.key === 'i18n.overrides')?.value ?? {}) as Overrides
      setSaved(v)
      setDraft(v)
      toast.success(t('saved'))
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
      <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
        <InputGroup className="w-full sm:w-80">
          <InputGroupAddon>
            <Search aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            value={query}
            placeholder={t('searchHint')}
            aria-label={t('search')}
            onChange={(e) => {
              setQuery(e.target.value)
              setShown(PAGE)
            }}
          />
        </InputGroup>
        <Label className="gap-2 font-normal">
          <Checkbox checked={changedOnly} onCheckedChange={setChangedOnly} />
          {t('changedOnly')}
        </Label>
        <span className="text-sm text-muted-foreground">{t('count', { count })}</span>
      </div>

      <div className="flex flex-col">
        <ul className="flex flex-col divide-y rounded-xl border bg-card">
        {matches.slice(0, shown).map((e) => (
          <li key={e.key} className="flex flex-col gap-3 p-4 sm:p-5">
              <code className="font-mono text-caption break-all text-faint-foreground">{e.key}</code>
              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {LOCALES.map((l) => {
                  const v = text(l, e.key)
                  const shipped = e.defaults[l]
                  if (shipped === undefined && v === '') {
                    return (
                      <div key={l} className="flex flex-col gap-1">
                        <div className="text-caption text-muted-foreground">{tl(l)}</div>
                        <p className="text-sm text-faint-foreground">{t('noDefault')}</p>
                      </div>
                    )
                  }
                  const p = v.trim() === '' ? null : overrideProblem(shipped ?? '', v)
                  return (
                    <div key={l} className="flex flex-col gap-1.5">
                      <div className="flex items-center gap-2 text-caption text-muted-foreground">
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
                      {p && <p className="text-caption text-destructive">{t(`problems.${p}`)}</p>}
                    </div>
                  )
                })}
              </div>
          </li>
        ))}
        {matches.length === 0 && <li className="p-8 text-center text-sm text-muted-foreground">{t('none')}</li>}
        </ul>
        {matches.length > shown && (
          <Button variant="outline" className="mt-4 self-center" onClick={() => setShown((n) => n + PAGE)}>
            {t('more', { left: matches.length - shown })}
          </Button>
        )}
        {problems.length > 0 && <p className="mt-4 text-caption text-destructive">{t('fixFirst', { count: problems.length })}</p>}
        {error && (
          <div className="mt-4">
            <FormError message={error} />
          </div>
        )}
        {/* Saving waits until every changed text keeps its placeholders. */}
        <SaveBar
          count={dirty && problems.length === 0 ? 1 : 0}
          saving={pending}
          onSave={() => void save()}
          onDiscard={() => setDraft(saved)}
        />
      </div>
    </div>
  )
}
