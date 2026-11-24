'use client'

import { listSettings, purgeCDN, type PurgeResult, sendTestMail, type SettingsList, type SettingView, updateSettings } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Badge,
  Button,
  cn,
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  PageHeader,
  SaveBar,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Skeleton,
  Switch,
  Textarea,
  toast,
  useConfirm,
} from '@skinloom/ui'
import {
  CircleAlert,
  Cloud,
  CodeXml,
  Coins,
  Gamepad2,
  Globe,
  KeyRound,
  Mail,
  PersonStanding,
  RefreshCw,
  RotateCcw,
  Search,
  SearchX,
  Send,
  ShieldCheck,
  Shirt,
  SlidersHorizontal,
} from 'lucide-react'
import { useTranslations } from 'next-intl'
import { type ComponentType, type FormEvent, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import {
  changesOf,
  CODE,
  type Draft,
  draftOf,
  type FieldProblem,
  groupSettings,
  LOCALES,
  MULTILINE,
  type SectionId,
} from './settings-form'

const SECTION_ICONS: Partial<Record<SectionId, ComponentType<{ className?: string }>>> = {
  site: Globe,
  auth: ShieldCheck,
  social: KeyRound,
  mail: Mail,
  score: Coins,
  texture: Shirt,
  player: PersonStanding,
  yggdrasil: Gamepad2,
  oauth: CodeXml,
  cdn: Cloud,
  other: SlidersHorizontal,
}

/** Every runtime setting, organised by category with instant search and conditional sub-fields. */
export function SettingsCenter({ superadmin }: { superadmin: boolean }) {
  const t = useTranslations('adminSettings')
  const tc = useTranslations('common')
  const tRef = useRef(t)
  tRef.current = t
  const tk = useTranslations('settingKeys')
  const confirm = useConfirm()
  const [list, setList] = useState<SettingsList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [activeId, setActiveId] = useState<SectionId>('site')
  const [query, setQuery] = useState('')
  const [dirtySections, setDirtySections] = useState<Set<SectionId>>(() => new Set())
  const hasDirty = dirtySections.size > 0

  const load = useCallback(() => {
    setError(null)
    listSettings()
      .then((res) => setList(res.data as SettingsList))
      .catch(() => setError(tRef.current('errors.load')))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!hasDirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [hasDirty])

  const markDirty = useCallback((id: SectionId, dirty: boolean) => {
    setDirtySections((prev) => {
      if (prev.has(id) === dirty) return prev
      const next = new Set(prev)
      if (dirty) next.add(id)
      else next.delete(id)
      return next
    })
  }, [])

  const confirmingRef = useRef(false)
  const confirmDiscardIfDirty = useCallback(async () => {
    if (dirtySections.size === 0) return true
    if (confirmingRef.current) return false
    confirmingRef.current = true
    try {
      return await confirm({
        title: t('unsavedTitle'),
        description: t('unsavedBody'),
        confirmLabel: t('unsavedConfirm'),
        destructive: false,
      })
    } finally {
      confirmingRef.current = false
    }
  }, [confirm, dirtySections.size, t])

  const groups = useMemo(() => (list ? groupSettings(list.settings, 'settings') : []), [list])
  const q = query.trim().toLowerCase()

  const filteredGroups = useMemo(() => {
    if (!q) return groups
    return groups
      .map((g) => ({
        ...g,
        settings: g.settings.filter((v) => {
          const label = tk.has(`${v.key}.label`) ? tk(`${v.key}.label`) : v.key
          const help = tk.has(`${v.key}.help`) ? tk(`${v.key}.help`) : ''
          return label.toLowerCase().includes(q) || help.toLowerCase().includes(q) || v.key.toLowerCase().includes(q)
        }),
      }))
      .filter((g) => g.settings.length > 0)
  }, [groups, q, tk])

  const currentSection = groups.find((g) => g.id === activeId) ?? groups[0]

  async function selectSection(id: SectionId) {
    if (!q && currentSection?.id === id) return
    if (!(await confirmDiscardIfDirty())) return
    setDirtySections(new Set())
    setActiveId(id)
    if (q) setQuery('')
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={t('title')}
        description={t('intro')}
        actions={
          <InputGroup className="w-full sm:w-64">
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput
              type="search"
              value={query}
              onChange={(e) => {
                const nextQ = e.target.value
                if (nextQ.trim().toLowerCase() === q) {
                  setQuery(nextQ)
                  return
                }
                if (dirtySections.size > 0) {
                  void confirmDiscardIfDirty().then((ok) => {
                    if (ok) {
                      setDirtySections(new Set())
                      setQuery(nextQ)
                    }
                  })
                  return
                }
                setQuery(nextQ)
              }}
              placeholder={t('searchPlaceholder')}
              aria-label={t('searchPlaceholder')}
            />
          </InputGroup>
        }
      />
      {!list &&
        (error ? (
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
        ))}
      {list && (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-8">
          {/* Category switcher: horizontal pills on mobile, sticky vertical rail on desktop. */}
          <nav
            aria-label={t('sectionsLabel')}
            className="no-scrollbar -mx-4 flex gap-1 self-start overflow-x-auto px-4 lg:sticky lg:top-20 lg:mx-0 lg:flex-col lg:px-0"
          >
            {groups.map((g) => {
              const Icon = SECTION_ICONS[g.id] ?? SlidersHorizontal
              const selected = !q && currentSection?.id === g.id
              const matchCount = q ? (filteredGroups.find((x) => x.id === g.id)?.settings.length ?? 0) : g.settings.length
              return (
                <button
                  key={g.id}
                  type="button"
                  aria-current={selected ? 'true' : undefined}
                  onClick={() => void selectSection(g.id)}
                  className={cn(
                    'flex shrink-0 cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm whitespace-nowrap transition-colors pointer-coarse:min-h-10',
                    selected ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                    q && matchCount === 0 && 'opacity-45',
                  )}
                >
                  <Icon className="size-4 shrink-0" aria-hidden />
                  <span className="flex-1 truncate">{t(`sections.${g.id}.title`)}</span>
                  {q && matchCount > 0 && (
                    <Badge variant="secondary" className="tabular-nums">
                      {matchCount}
                    </Badge>
                  )}
                </button>
              )
            })}
          </nav>

          <div className="flex min-w-0 flex-col gap-10">
            {q ? (
              filteredGroups.length > 0 ? (
                filteredGroups.map((g) => (
                  <SettingsSection
                    key={`${g.id}:${q}`}
                    id={g.id}
                    settings={g.settings}
                    superadmin={superadmin}
                    searching
                    onDirtyChange={markDirty}
                    onSaved={setList}
                  />
                ))
              ) : (
                <Empty className="border border-dashed py-16">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <SearchX />
                    </EmptyMedia>
                    <EmptyTitle>{t('noMatch')}</EmptyTitle>
                  </EmptyHeader>
                </Empty>
              )
            ) : (
              currentSection && (
                <SettingsSection
                  key={currentSection.id}
                  id={currentSection.id}
                  settings={currentSection.settings}
                  superadmin={superadmin}
                  onDirtyChange={markDirty}
                  onSaved={setList}
                />
              )
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function draftsOf(views: SettingView[]): Record<string, Draft> {
  return Object.fromEntries(views.map((v) => [v.key, draftOf(v)]))
}

/**
 * Hides provider-specific sub-fields (e.g. Turnstile/GeeTest/hCaptcha/Cap keys when
 * captcha.provider is "image", or SMTP fields when mail.driver is "none") until that
 * provider is selected, keeping each section concise and easy to scan.
 */
function isVisibleField(v: SettingView, drafts: Record<string, Draft>, searching?: boolean): boolean {
  if (searching) return true
  if (v.key.startsWith('captcha.') && v.key !== 'captcha.provider') {
    const provider = String(drafts['captcha.provider']?.value ?? 'none')
    return provider !== 'none' && v.key.startsWith(`captcha.${provider}.`)
  }
  if (v.key.startsWith('mail.smtp.')) {
    const driver = String(drafts['mail.driver']?.value ?? 'none')
    return driver === 'smtp'
  }
  if (v.key === 'player.name_regexp') {
    const rule = String(drafts['player.name_rule']?.value ?? 'official')
    return rule === 'custom'
  }
  if (v.key.startsWith('cdn.') && v.key !== 'cdn.provider') {
    const provider = String(drafts['cdn.provider']?.value ?? 'none')
    if (provider === 'none') return false
    if (v.key === 'cdn.base_url') return true
    return v.key.startsWith(`cdn.${provider}.`)
  }
  return true
}

function formatProblemMessage(problem: FieldProblem | string | undefined, t: ReturnType<typeof useTranslations<'adminSettings'>>): string | undefined {
  if (!problem) return undefined
  if (typeof problem === 'string') return problem
  if (problem.code === 'range') {
    return problem.max === undefined
      ? t('problems.atLeast', { min: problem.min ?? 0 })
      : t('problems.range', { min: problem.min ?? 0, max: problem.max })
  }
  return t(`problems.${problem.code}`)
}

/**
 * One section of settings with its own save bar (the settings page, and the
 * branding and custom CSS sections of the appearance page).
 */
export function SettingsSection({
  id,
  settings,
  superadmin,
  searching,
  onDirtyChange,
  onSaved,
}: {
  id: SectionId
  settings: SettingView[]
  superadmin: boolean
  searching?: boolean
  onDirtyChange?: (id: SectionId, dirty: boolean) => void
  onSaved: (list: SettingsList) => void
}) {
  const t = useTranslations('adminSettings')
  const tk = useTranslations('settingKeys')
  const [drafts, setDrafts] = useState(() => draftsOf(settings))
  const [problems, setProblems] = useState<Record<string, FieldProblem | string>>({})
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // An edit that does not check out counts too: saving is what shows its message.
  const edited = changesOf(settings, drafts)
  const count = new Set([...Object.keys(edited.changes), ...Object.keys(edited.problems)]).size

  useEffect(() => {
    onDirtyChange?.(id, count > 0)
    return () => onDirtyChange?.(id, false)
  }, [count, id, onDirtyChange])

  const visibleSettings = settings.filter((v) => isVisibleField(v, drafts, searching))

  async function save(e?: FormEvent<HTMLFormElement>) {
    e?.preventDefault()
    const { changes, problems: localProblems } = changesOf(settings, drafts)
    setProblems(localProblems)
    setError(null)
    if (Object.keys(localProblems).length > 0) {
      const visibleKeys = new Set(visibleSettings.map((v) => v.key))
      const hiddenMessages = Object.entries(localProblems)
        .filter(([k]) => !visibleKeys.has(k))
        .map(([k, prob]) => {
          const fieldLabel = tk.has(`${k}.label`) ? tk(`${k}.label`) : k
          const detail = formatProblemMessage(prob, t) ?? ''
          return t('hiddenFieldError', { field: fieldLabel, detail })
        })
      if (hiddenMessages.length > 0) setError(hiddenMessages.join(' '))
      return
    }
    if (Object.keys(changes).length === 0) return
    setPending(true)
    try {
      const next = (await updateSettings(changes)).data as SettingsList
      onSaved(next)
      setDrafts(draftsOf(next.settings.filter((v) => settings.some((s) => s.key === v.key))))
      toast.success(t('saved'))
    } catch (err) {
      const p = problemOf(err)
      if (Object.keys(p.fields).length > 0) {
        setProblems(p.fields)
        const visibleKeys = new Set(visibleSettings.map((v) => v.key))
        const hiddenMessages = Object.entries(p.fields)
          .filter(([k]) => !visibleKeys.has(k))
          .map(([k, detail]) => {
            const fieldLabel = tk.has(`${k}.label`) ? tk(`${k}.label`) : k
            return t('hiddenFieldError', { field: fieldLabel, detail })
          })
        if (hiddenMessages.length > 0) {
          setError(hiddenMessages.join(' '))
        }
      } else {
        setError(p.status === 403 ? t('errors.forbidden') : p.detail ?? t('errors.save'))
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <section id={`settings-${id}`} className="flex scroll-mt-20 flex-col gap-4" aria-labelledby={`settings-${id}-title`}>
      <div className="flex flex-col gap-1">
        <h2 id={`settings-${id}-title`} className="text-lg font-medium">
          {t(`sections.${id}.title`)}
        </h2>
        {t.has(`sections.${id}.intro`) && <p className="text-sm text-muted-foreground">{t(`sections.${id}.intro`)}</p>}
      </div>
      {/* changesOf checks the values and says why in the page's language. */}
      <form onSubmit={(e) => void save(e)} noValidate className="flex flex-col">
        <div className="flex flex-col divide-y rounded-xl border bg-card">
          {visibleSettings.map((v) => (
            <SettingField
              key={v.key}
              view={v}
              draft={drafts[v.key]}
              problem={problems[v.key]}
              readOnly={v.superadmin && !superadmin}
              onChange={(d) => {
                setDrafts((prev) => ({ ...prev, [v.key]: d }))
                setProblems(({ [v.key]: _, ...rest }) => rest)
              }}
            />
          ))}
        </div>
        {error && (
          <div className="mt-4">
            <FormError message={error} />
          </div>
        )}
        <SaveBar
          count={count}
          saving={pending}
          onSave={() => void save()}
          onDiscard={() => {
            setDrafts(draftsOf(settings))
            setProblems({})
            setError(null)
          }}
        />
      </form>
      {!searching && id === 'mail' && <TestMailTool />}
      {!searching && id === 'cdn' && <PurgeCdnTool />}
    </section>
  )
}

/** Sends an immediate test email using the saved mail configuration (POST /api/v1/admin/mail/test). */
function TestMailTool() {
  const t = useTranslations('adminSettings.testMail')
  const [to, setTo] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await sendTestMail(to.trim() ? { to: to.trim() } : {})
      toast.success(t('sent'))
    } catch (err) {
      const p = problemOf(err)
      setError(p.detail ?? t('failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={(e) => void onSubmit(e)} noValidate className="flex flex-col gap-3 rounded-xl border bg-card p-5">
      <div className="text-sm font-medium">{t('title')}</div>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Input
          type="email"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          placeholder={t('placeholder')}
          aria-label={t('title')}
          className="sm:max-w-sm"
        />
        <Button type="submit" variant="outline" loading={pending} className="self-start sm:self-auto">
          <Send aria-hidden />
          {t('button')}
        </Button>
      </div>
      <FormError message={error} />
    </form>
  )
}

/** Manually purges player profile & avatar URLs from the configured CDN (POST /api/v1/admin/cdn/purge). */
function PurgeCdnTool() {
  const t = useTranslations('adminSettings.purgeCdn')
  const [input, setInput] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const players = input
      .split(/[\s,，]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (players.length === 0) return
    setPending(true)
    setError(null)
    try {
      const res = (await purgeCDN({ players })).data as PurgeResult
      toast.success(t('done', { urls: res.urls }))
      setInput('')
    } catch (err) {
      const p = problemOf(err)
      setError(p.detail ?? t('failed'))
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={(e) => void onSubmit(e)} noValidate className="flex flex-col gap-3 rounded-xl border bg-card p-5">
      <div className="text-sm font-medium">{t('title')}</div>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t('placeholder')}
          aria-label={t('title')}
          className="sm:max-w-md"
        />
        <Button type="submit" variant="outline" loading={pending} disabled={!input.trim()} className="self-start sm:self-auto">
          <RefreshCw aria-hidden />
          {t('button')}
        </Button>
      </div>
      <FormError message={error} />
    </form>
  )
}

function SettingField({
  view: v,
  draft,
  problem,
  readOnly,
  onChange,
}: {
  view: SettingView
  draft: Draft
  problem?: FieldProblem | string
  /** A super administrators' setting shown to another administrator. */
  readOnly: boolean
  onChange: (d: Draft) => void
}) {
  const t = useTranslations('adminSettings')
  const tk = useTranslations('settingKeys')
  const id = `setting-${v.key}`
  const hasLabel = tk.has(`${v.key}.label`)
  const label = hasLabel ? tk(`${v.key}.label`) : v.key
  const help = tk.has(`${v.key}.help`) ? tk(`${v.key}.help`) : v.doc.replace(/\s*\(BS [^)]+\)\.?$/, '')
  const option = (value: unknown) => (tk.has(`${v.key}.options.${value}`) ? tk(`${v.key}.options.${value}`) : String(value))
  const message = formatProblemMessage(problem, t)
  const set = (value: Draft['value']) => {
    if (readOnly) return
    onChange({ value })
  }
  const described = `${id}-help`

  const hasEnum = Boolean(v.enum && v.enum.length > 0)
  const isInline = v.type === 'bool' || v.type === 'int' || hasEnum

  let control: ReactNode = null
  if (hasEnum) {
    const items = v.enum!.map((e) => ({ value: String(e), label: option(e) }))
    control = (
      <Select items={items} value={String(draft.value)} disabled={readOnly} onValueChange={(x) => x !== null && set(String(x))}>
        <SelectTrigger id={id} aria-describedby={described} className="w-full sm:w-72">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((i) => (
            <SelectItem key={i.value} value={i.value}>
              {i.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  } else if (v.type === 'localized') {
    const values = draft.value as Record<string, string>
    control = (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {LOCALES.map((l) => (
          <label key={l} className="flex flex-col gap-1.5 text-caption text-muted-foreground">
            {t(`locales.${l}`)}
            {MULTILINE.has(v.key) ? (
              <Textarea rows={4} value={values[l]} onChange={(e) => set({ ...values, [l]: e.target.value })} />
            ) : (
              <Input value={values[l]} onChange={(e) => set({ ...values, [l]: e.target.value })} />
            )}
          </label>
        ))}
      </div>
    )
  } else if (v.type === 'json' || v.type === 'text') {
    control = (
      <Textarea
        id={id}
        rows={CODE.has(v.key) ? 12 : v.type === 'json' ? 8 : 4}
        spellCheck={false}
        value={String(draft.value)}
        onChange={(e) => set(e.target.value)}
        aria-describedby={described}
        aria-invalid={message ? true : undefined}
        className={cn(v.type === 'json' || CODE.has(v.key) ? 'font-mono text-xs' : undefined)}
      />
    )
  } else if (v.type === 'bool') {
    control = <Switch id={id} checked={draft.value === true} disabled={readOnly} onCheckedChange={(on) => set(on)} aria-describedby={described} />
  } else {
    control = (
      <Input
        id={id}
        type={v.secret ? 'password' : v.type === 'int' ? 'number' : 'text'}
        autoComplete={v.secret ? 'new-password' : 'off'}
        inputMode={v.type === 'int' ? 'numeric' : undefined}
        min={v.min}
        max={v.max}
        value={String(draft.value)}
        placeholder={v.secret ? (draft.reset ? t('secretClearing') : v.isSet ? t('secretSet') : t('secretUnset')) : undefined}
        onChange={(e) => set(e.target.value)}
        aria-describedby={described}
        aria-invalid={message ? true : undefined}
        className={v.type === 'int' ? 'w-36 tabular-nums' : 'sm:max-w-md'}
      />
    )
  }

  return (
    <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-3 p-5 disabled:opacity-70">
      <div className={cn('flex flex-col gap-3', isInline && 'sm:flex-row sm:items-center sm:justify-between sm:gap-6')}>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            {v.type === 'localized' ? (
              <span className="text-sm font-medium">{label}</span>
            ) : (
              <label htmlFor={id} className="text-sm font-medium">
                {label}
              </label>
            )}
            {v.secret && v.isSet && !draft.reset && <Badge variant="secondary">{t('secretBadge')}</Badge>}
            {readOnly && <Badge variant="outline">{t('superadminOnly')}</Badge>}
            {v.isSet && !draft.reset && (
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="text-muted-foreground pointer-coarse:h-10"
                onClick={() => onChange({ value: draftOf({ ...v, value: v.default }).value, reset: true })}
              >
                <RotateCcw className="size-3" aria-hidden />
                {v.secret ? t('clear') : t('reset')}
              </Button>
            )}
            {draft.reset && <Badge variant="outline">{v.secret ? t('willClear') : t('willReset')}</Badge>}
          </div>
          {help && (
            <p id={described} className="text-caption leading-relaxed text-muted-foreground">
              {help}
            </p>
          )}
        </div>
        {isInline && <div className="shrink-0">{control}</div>}
      </div>
      {!isInline && control}
      {message && <p className="text-caption text-destructive">{message}</p>}
    </fieldset>
  )
}
