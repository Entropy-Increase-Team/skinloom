'use client'

import { listSettings, type SettingsList, type SettingView, updateSettings } from '@skinloom/api-client'
import { Badge, Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input } from '@skinloom/ui'
import { useTranslations } from 'next-intl'
import { type FormEvent, type ReactNode, useEffect, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import { changesOf, type Draft, draftOf, type FieldProblem, groupSettings, LOCALES, MULTILINE, type SectionId } from './settings-form'

const selectClass = 'h-9 rounded-md border border-input bg-transparent px-2 text-sm'
const areaClass = 'rounded-md border border-input bg-transparent px-3 py-2 text-sm'

/** Every runtime setting, by section; each section saves on its own and applies at once. */
export function SettingsCenter({ superadmin }: { superadmin: boolean }) {
  const t = useTranslations('adminSettings')
  const [list, setList] = useState<SettingsList | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listSettings()
      .then((res) => setList(res.data as SettingsList))
      .catch(() => setError(t('errors.load')))
  }, [t])

  if (!list) return error ? <FormError message={error} /> : <p className="text-sm text-muted-foreground">{t('loading')}</p>
  const groups = groupSettings(list.settings, 'settings')
  return (
    <div className="grid gap-6 lg:grid-cols-[11rem_1fr]">
      <nav aria-label={t('sectionsLabel')} className="flex gap-1 self-start overflow-x-auto lg:sticky lg:top-20 lg:flex-col">
        {groups.map((g) => (
          <a key={g.id} href={`#settings-${g.id}`} className="whitespace-nowrap rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground">
            {t(`sections.${g.id}.title`)}
          </a>
        ))}
      </nav>
      <div className="flex min-w-0 flex-col gap-6">
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
        {groups.map((g) => (
          <SettingsSection key={g.id} id={g.id} settings={g.settings} superadmin={superadmin} onSaved={setList} />
        ))}
      </div>
    </div>
  )
}

function draftsOf(views: SettingView[]): Record<string, Draft> {
  return Object.fromEntries(views.map((v) => [v.key, draftOf(v)]))
}

/**
 * One section of settings with its own save button (the settings page, and the
 * branding and custom CSS sections of the appearance page).
 */
export function SettingsSection({
  id,
  settings,
  superadmin,
  onSaved,
}: {
  id: SectionId
  settings: SettingView[]
  superadmin: boolean
  onSaved: (list: SettingsList) => void
}) {
  const t = useTranslations('adminSettings')
  const [drafts, setDrafts] = useState(() => draftsOf(settings))
  const [problems, setProblems] = useState<Record<string, FieldProblem | string>>({})
  const [pending, setPending] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // An edit that does not check out counts too: saving is what shows its message.
  const edited = changesOf(settings, drafts)
  const dirty = Object.keys(edited.changes).length + Object.keys(edited.problems).length > 0

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const { changes, problems } = changesOf(settings, drafts)
    setProblems(problems)
    setSaved(false)
    setError(null)
    if (Object.keys(problems).length > 0 || Object.keys(changes).length === 0) return
    setPending(true)
    try {
      const next = (await updateSettings(changes)).data as SettingsList
      onSaved(next)
      setDrafts(draftsOf(next.settings.filter((v) => settings.some((s) => s.key === v.key))))
      setSaved(true)
    } catch (err) {
      const p = problemOf(err)
      if (Object.keys(p.fields).length > 0) setProblems(p.fields)
      else setError(p.status === 403 ? t('errors.forbidden') : t('errors.save'))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card id={`settings-${id}`} className="scroll-mt-20">
      <CardHeader>
        <CardTitle>{t(`sections.${id}.title`)}</CardTitle>
        {t.has(`sections.${id}.intro`) && <CardDescription>{t(`sections.${id}.intro`)}</CardDescription>}
      </CardHeader>
      <CardContent>
        {/* changesOf checks the values and says why in the page's language. */}
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-6">
          {settings.map((v) => (
            <SettingField
              key={v.key}
              view={v}
              draft={drafts[v.key]}
              problem={problems[v.key]}
              readOnly={v.superadmin && !superadmin}
              onChange={(d) => {
                setDrafts((prev) => ({ ...prev, [v.key]: d }))
                setProblems(({ [v.key]: _, ...rest }) => rest)
                setSaved(false)
              }}
            />
          ))}
          <FormError message={error} />
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={pending || !dirty}>
              {t('save')}
            </Button>
            {saved && !dirty && (
              <span role="status" className="text-sm text-primary">
                {t('saved')}
              </span>
            )}
            {dirty && <span className="text-xs text-muted-foreground">{t('unsaved')}</span>}
          </div>
        </form>
      </CardContent>
    </Card>
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
  const label = tk.has(`${v.key}.label`) ? tk(`${v.key}.label`) : v.key
  // The server's description (English) stands in for keys without a translation, e.g. a plugin's.
  const help = tk.has(`${v.key}.help`) ? tk(`${v.key}.help`) : v.doc
  const option = (value: unknown) => (tk.has(`${v.key}.options.${value}`) ? tk(`${v.key}.options.${value}`) : String(value))
  const message =
    typeof problem === 'string'
      ? problem
      : problem?.code === 'range'
        ? problem.max === undefined
          ? t('problems.atLeast', { min: problem.min ?? 0 })
          : t('problems.range', { min: problem.min ?? 0, max: problem.max })
        : problem && t(`problems.${problem.code}`)
  const set = (value: Draft['value']) => onChange({ value })
  const described = `${id}-help`

  let control: ReactNode
  if (v.type === 'bool') {
    control = (
      <label className="flex items-center gap-2 text-sm font-medium">
        <input id={id} type="checkbox" checked={draft.value === true} onChange={(e) => set(e.target.checked)} aria-describedby={described} className="size-4" />
        {label}
      </label>
    )
  } else if (v.enum && v.enum.length > 0) {
    control = (
      <select id={id} value={String(draft.value)} onChange={(e) => set(e.target.value)} aria-describedby={described} className={`${selectClass} w-fit`}>
        {v.enum.map((e) => (
          <option key={String(e)} value={String(e)}>
            {option(e)}
          </option>
        ))}
      </select>
    )
  } else if (v.type === 'localized') {
    const values = draft.value as Record<string, string>
    control = (
      <div className="grid gap-2 sm:grid-cols-2">
        {LOCALES.map((l) => (
          <label key={l} className="flex flex-col gap-1 text-xs text-muted-foreground">
            {t(`locales.${l}`)}
            {MULTILINE.has(v.key) ? (
              <textarea rows={4} value={values[l]} onChange={(e) => set({ ...values, [l]: e.target.value })} className={areaClass} />
            ) : (
              <Input value={values[l]} onChange={(e) => set({ ...values, [l]: e.target.value })} />
            )}
          </label>
        ))}
      </div>
    )
  } else if (v.type === 'json' || v.type === 'text') {
    control = (
      <textarea
        id={id}
        rows={v.type === 'json' ? 8 : 4}
        spellCheck={false}
        value={String(draft.value)}
        onChange={(e) => set(e.target.value)}
        aria-describedby={described}
        className={`${areaClass} ${v.type === 'json' ? 'font-mono text-xs' : ''}`}
      />
    )
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
        className={v.type === 'int' ? 'w-40' : undefined}
      />
    )
  }

  return (
    // A disabled fieldset disables every control in it, the reset link included.
    <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-2">
      {v.type !== 'bool' && (
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={id} className="text-sm font-medium">
            {label}
          </label>
          {v.secret && v.isSet && !draft.reset && <Badge variant="secondary">{t('secretBadge')}</Badge>}
          {readOnly && <Badge variant="outline">{t('superadminOnly')}</Badge>}
        </div>
      )}
      {control}
      {message && <p className="text-xs text-destructive">{message}</p>}
      <p id={described} className="text-xs text-muted-foreground">
        {help}
      </p>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <code className="font-mono">{v.key}</code>
        {v.isSet && !draft.reset && (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0 text-xs"
            onClick={() => onChange({ value: draftOf({ ...v, value: v.default }).value, reset: true })}
          >
            {v.secret ? t('clear') : t('reset')}
          </Button>
        )}
        {draft.reset && <span>{v.secret ? t('willClear') : t('willReset')}</span>}
      </div>
    </fieldset>
  )
}
