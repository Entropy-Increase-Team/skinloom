'use client'

import { listSettings, type SettingsList, updateSettings } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Badge,
  Button,
  Checkbox,
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Label,
  SaveBar,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Textarea,
  toast,
} from '@skinloom/ui'
import { CircleAlert, RotateCcw, Search, SearchX } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useFormatter, useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { overrideProblem, savedOverrides } from '@/lib/messages'
import { problemOf } from '@/lib/problem'

import { LOCALES } from './settings-form'

/** One interface text: its message key and the shipped wording in each language that has it. */
export interface TextEntry {
  key: string
  defaults: Partial<Record<string, string>>
}

type Overrides = Record<string, Record<string, string>>

const PAGE = 40
const ALL_LOCALES = 'all'

/**
 * Rewords any interface text (i18n.overrides, BS language_lines): search by key or
 * text, type a replacement per language, save. Texts keep the default's placeholders.
 */
export function Translations({ entries }: { entries: TextEntry[] }) {
  const t = useTranslations('adminTranslations')
  const tl = useTranslations('adminSettings.locales')
  const tc = useTranslations('common')
  const format = useFormatter()
  const tRef = useRef(t)
  tRef.current = t
  const router = useRouter()
  const [saved, setSaved] = useState<Overrides | null>(null)
  const [draft, setDraft] = useState<Overrides>({})
  const [query, setQuery] = useState('')
  const [localeFilter, setLocaleFilter] = useState<string>(ALL_LOCALES)
  const [changedOnly, setChangedOnly] = useState(false)
  const [shown, setShown] = useState(PAGE)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    setError(null)
    listSettings()
      .then((res) => {
        const v = (res.data as SettingsList).settings.find((s) => s.key === 'i18n.overrides')?.value
        const o = (v ?? {}) as Overrides
        setSaved(o)
        setDraft(o)
      })
      .catch(() => setError(tRef.current('errors.load')))
  }, [])

  // Once: a save refreshes the page (router.refresh), which hands out a new t, and
  // loading again then would replace what was typed since.
  useEffect(() => {
    load()
  }, [load])

  const next = savedOverrides(draft)
  const base = saved ? savedOverrides(saved) : null
  const dirtyCount = saved
    ? LOCALES.reduce((sum, l) => {
        const a = next?.[l] ?? {}
        const b = base?.[l] ?? {}
        const keys = new Set([...Object.keys(a), ...Object.keys(b)])
        return sum + Array.from(keys).filter((k) => (a[k] ?? '') !== (b[k] ?? '')).length
      }, 0)
    : 0

  useEffect(() => {
    if (dirtyCount === 0) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirtyCount])

  if (!saved) {
    return error ? (
      <Alert variant="destructive" role="alert">
        <CircleAlert />
        <AlertDescription>{error}</AlertDescription>
        <AlertAction>
          <Button type="button" variant="outline" size="sm" onClick={load}>
            {tc('retry')}
          </Button>
        </AlertAction>
      </Alert>
    ) : (
      <Skeleton className="h-96 rounded-xl" />
    )
  }

  const visibleLocales = localeFilter === ALL_LOCALES ? LOCALES : LOCALES.filter((l) => l === localeFilter)
  const text = (locale: string, key: string) => draft[locale]?.[key] ?? ''
  const q = query.trim().toLowerCase()
  const matches = entries.filter(
    (e) =>
      (!changedOnly || visibleLocales.some((l) => text(l, e.key) !== '')) &&
      (q === '' || e.key.toLowerCase().includes(q) || visibleLocales.some((l) => `${e.defaults[l]} ${text(l, e.key)}`.toLowerCase().includes(q))),
  )

  const problems = entries.flatMap((e) =>
    LOCALES.flatMap((l) => {
      const v = text(l, e.key)
      const p = v.trim() === '' ? null : overrideProblem(e.defaults[l] ?? '', v)
      return p ? [{ key: e.key, locale: l, problem: p }] : []
    }),
  )
  const count = Object.values(next ?? {}).reduce((n, m) => n + Object.keys(m).length, 0)
  const localeItems = [{ value: ALL_LOCALES, label: t('allLocales') }, ...LOCALES.map((l) => ({ value: l, label: tl(l) }))]

  function set(locale: string, key: string, value: string) {
    setDraft((d) => ({ ...d, [locale]: { ...(d[locale] ?? {}), [key]: value } }))
  }

  async function save() {
    if (problems.length > 0) return
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
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
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
        <Select
          items={localeItems}
          value={localeFilter}
          onValueChange={(v) => {
            if (v) {
              setLocaleFilter(String(v))
              setShown(PAGE)
            }
          }}
        >
          <SelectTrigger className="w-40" aria-label={t('allLocales')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {localeItems.map((i) => (
              <SelectItem key={i.value} value={i.value}>
                {i.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Label className="gap-2 font-normal pointer-coarse:min-h-10">
          <Checkbox checked={changedOnly} onCheckedChange={setChangedOnly} />
          {t('changedOnly')}
        </Label>
        <span className="text-sm text-muted-foreground tabular-nums">
          {t('count', { count: format.number(count) })}
          {(q !== '' || changedOnly) && ` · ${t('matchedCount', { matched: format.number(matches.length), total: format.number(entries.length) })}`}
        </span>
      </div>

      <div className="flex flex-col">
        {matches.length === 0 ? (
          <Empty className="border border-dashed py-16">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <SearchX />
              </EmptyMedia>
              <EmptyTitle>{t('none')}</EmptyTitle>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="flex flex-col divide-y rounded-xl border bg-card">
            {matches.slice(0, shown).map((e) => (
              <li key={e.key} className="flex flex-col gap-3 p-4 sm:p-5">
                <code className="font-mono text-caption break-all text-faint-foreground">{e.key}</code>
                <div className={visibleLocales.length === 1 ? 'grid grid-cols-1 gap-4' : 'grid grid-cols-1 gap-4 md:grid-cols-2'}>
                  {visibleLocales.map((l) => {
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
                    const multiline = Boolean(shipped?.includes('\n') || saved?.[l]?.[e.key]?.includes('\n') || (shipped?.length ?? 0) > 80)
                    return (
                      <div key={l} className="flex flex-col gap-1.5">
                        <div className="flex items-center gap-2 text-caption text-muted-foreground">
                          <span>{tl(l)}</span>
                          {v.trim() !== '' && <Badge variant="secondary">{t('changed')}</Badge>}
                          {v !== '' && (
                            <Button
                              type="button"
                              variant="ghost"
                              size="xs"
                              className="ml-auto text-muted-foreground pointer-coarse:h-10"
                              onClick={() => set(l, e.key, '')}
                            >
                              <RotateCcw className="size-3" aria-hidden />
                              {t('reset')}
                            </Button>
                          )}
                        </div>
                        <p className="text-sm break-words whitespace-pre-line">{shipped || '—'}</p>
                        {multiline ? (
                          <Textarea
                            rows={3}
                            aria-label={`${e.key} ${tl(l)}`}
                            value={v}
                            placeholder={t('keep')}
                            aria-invalid={p ? true : undefined}
                            onChange={(ev) => set(l, e.key, ev.target.value)}
                          />
                        ) : (
                          <Input
                            aria-label={`${e.key} ${tl(l)}`}
                            value={v}
                            placeholder={t('keep')}
                            aria-invalid={p ? true : undefined}
                            onChange={(ev) => set(l, e.key, ev.target.value)}
                          />
                        )}
                        {p && <p className="text-caption text-destructive">{t(`problems.${p}`)}</p>}
                      </div>
                    )
                  })}
                </div>
              </li>
            ))}
          </ul>
        )}
        {matches.length > shown && (
          <Button variant="outline" className="mt-4 self-center" onClick={() => setShown((n) => n + PAGE)}>
            {t('more', { left: format.number(matches.length - shown) })}
          </Button>
        )}
        {problems.length > 0 && <p className="mt-4 text-caption text-destructive">{t('fixFirst', { count: problems.length })}</p>}
        {error && (
          <div className="mt-4">
            <FormError message={error} />
          </div>
        )}
        {/* Saving waits until every changed text keeps its placeholders; Discard stays available. */}
        <SaveBar
          count={dirtyCount}
          disabled={problems.length > 0}
          saving={pending}
          onSave={() => void save()}
          onDiscard={() => setDraft(saved)}
        />
      </div>
    </div>
  )
}
