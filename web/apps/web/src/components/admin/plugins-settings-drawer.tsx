'use client'

import {
  getPluginSettings,
  type PluginSettingFieldView,
  type PluginSettingsResponse,
  updatePluginSettings,
} from '@skinloom/api-client'
import {
  Badge,
  Button,
  Checkbox,
  cn,
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  Spinner,
  Switch,
  Textarea,
  toast,
} from '@skinloom/ui'
import { Plus, Trash2 } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'
import { safeImageUrl } from '@/lib/safe-url'

import {
  apiProblemMessage,
  buildPluginSettingsDraft,
  buildPluginSettingsPatch,
  groupSubfieldToView,
  isPluginSettingsDirty,
  type PluginSettingsDraft,
} from './plugins-settings-form'

const UNSET_SELECT = '__unset'

function localizeValidationError(msg: string, t: ReturnType<typeof useTranslations>): string {
  if (msg === 'this field is required') return t('validation.required')
  if (msg === 'must be a finite number') return t('validation.finiteNumber')
  if (msg.startsWith('must be >= ')) return t('validation.min', { min: msg.slice('must be >= '.length) })
  if (msg.startsWith('must be <= ')) return t('validation.max', { max: msg.slice('must be <= '.length) })
  if (msg === 'list items must be valid numbers') return t('validation.listNumber')
  if (msg === 'invalid JSON in list item') return t('validation.listJson')
  if (msg === 'invalid JSON syntax') return t('validation.jsonSyntax')
  if (msg === 'must be a JSON object' || msg === 'invalid JSON object') return t('validation.jsonObject')
  return msg
}

export function PluginSettingsDrawer({
  pluginId,
  pluginName,
  open,
  onOpenChange,
  onSaved,
}: {
  pluginId: string | null
  pluginName: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onSaved?: () => void
}) {
  const t = useTranslations('adminPlugins.settingsDrawer')
  const te = useTranslations('adminPlugins')
  const locale = useLocale()
  const loadErrRef = useRef(te('errors.load'))
  loadErrRef.current = te('errors.load')
  const [schema, setSchema] = useState<PluginSettingsResponse | null>(null)
  const [initialDraft, setInitialDraft] = useState<PluginSettingsDraft>({})
  const [draft, setDraft] = useState<PluginSettingsDraft>({})
  const [clearedSecrets, setClearedSecrets] = useState<Set<string>>(() => new Set())
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(false)
  const activeSessionRef = useRef(0)

  useEffect(() => {
    void locale
    activeSessionRef.current += 1
    if (!open || !pluginId) return
    let active = true
    setSchema(null)
    setInitialDraft({})
    setDraft({})
    setLoading(true)
    setLoadError(null)
    setFieldErrors({})
    setClearedSecrets(new Set())
    getPluginSettings(pluginId)
      .then((res) => {
        if (!active) return
        const data = res.data as PluginSettingsResponse
        const nextDraft = buildPluginSettingsDraft(data.fields)
        setSchema(data)
        setInitialDraft(nextDraft)
        setDraft(nextDraft)
      })
      .catch((err) => {
        if (!active) return
        setSchema(null)
        setLoadError(apiProblemMessage(problemOf(err).detail, err, loadErrRef.current))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [open, pluginId, locale])

  const dirty = Boolean(schema && isPluginSettingsDirty(schema.fields, draft, clearedSecrets, initialDraft))

  async function handleSave() {
    if (!pluginId || !schema || saving) return
    const { patch, errors } = buildPluginSettingsPatch(schema.fields, draft, clearedSecrets, initialDraft)
    if (Object.keys(errors).length > 0) {
      const localized: Record<string, string> = {}
      for (const [k, v] of Object.entries(errors)) {
        localized[k] = localizeValidationError(v, t)
      }
      setFieldErrors(localized)
      return
    }
    if (Object.keys(patch).length === 0) {
      onOpenChange(false)
      return
    }
    const reqSession = activeSessionRef.current
    setSaving(true)
    setLoadError(null)
    try {
      const res = await updatePluginSettings(pluginId, patch)
      if (activeSessionRef.current !== reqSession) return
      const nextSchema = res.data as PluginSettingsResponse
      const nextDraft = buildPluginSettingsDraft(nextSchema.fields)
      setSchema(nextSchema)
      setInitialDraft(nextDraft)
      setDraft(nextDraft)
      setClearedSecrets(new Set())
      setFieldErrors({})
      toast.success(te('toasts.settingsSaved'))
      onSaved?.()
      onOpenChange(false)
    } catch (err) {
      if (activeSessionRef.current !== reqSession) return
      setLoadError(apiProblemMessage(problemOf(err).detail, err, te('errors.settingsSave')))
    } finally {
      if (activeSessionRef.current === reqSession) {
        setSaving(false)
      }
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next && saving) return
        onOpenChange(next)
      }}
    >
      <SheetContent className="flex w-full flex-col overflow-hidden sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>{t('title', { name: pluginName })}</SheetTitle>
          <SheetDescription>{t('description')}</SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-5 overflow-y-auto px-4 py-3">
          {loading ? (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Spinner />
            </div>
          ) : null}
          <FormError message={loadError} />
          {!loading && schema && schema.fields.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t('empty')}</p>
          ) : null}

          {!loading &&
            schema?.fields.map((field) => (
              <PluginSettingControl
                key={field.key}
                field={field}
                locale={locale}
                value={draft[field.key]}
                clearedSecrets={clearedSecrets}
                fieldErrors={fieldErrors}
                onChange={(nextVal) => {
                  setDraft((prev) => ({ ...prev, [field.key]: nextVal }))
                  setFieldErrors((prev) => {
                    const next = { ...prev }
                    let changed = false
                    if (field.key in next) {
                      delete next[field.key]
                      changed = true
                    }
                    const prefix = `${field.key}.`
                    for (const k of Object.keys(next)) {
                      if (k.startsWith(prefix)) {
                        delete next[k]
                        changed = true
                      }
                    }
                    return changed ? next : prev
                  })
                  if (field.type === 'secret' && typeof nextVal === 'string' && nextVal.length > 0) {
                    setClearedSecrets((prev) => {
                      if (!prev.has(field.key)) return prev
                      const nextSet = new Set(prev)
                      nextSet.delete(field.key)
                      return nextSet
                    })
                  }
                }}
                onToggleClearSecret={(secretPath, cleared) => {
                  setClearedSecrets((prev) => {
                    const nextSet = new Set(prev)
                    if (cleared) {
                      nextSet.add(secretPath)
                    } else {
                      nextSet.delete(secretPath)
                    }
                    return nextSet
                  })
                  if (cleared) {
                    setDraft((d) => {
                      if (secretPath === field.key) return { ...d, [field.key]: '' }
                      const subKey = secretPath.slice(field.key.length + 1)
                      const cur =
                        typeof d[field.key] === 'object' && d[field.key] !== null
                          ? (d[field.key] as Record<string, unknown>)
                          : {}
                      return { ...d, [field.key]: { ...cur, [subKey]: '' } }
                    })
                  }
                }}
              />
            ))}
        </div>

        <SheetFooter className="border-t border-border px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          <Button type="button" variant="outline" className="pointer-coarse:h-10" onClick={() => onOpenChange(false)} disabled={saving}>
            {t('cancel')}
          </Button>
          <Button type="button" className="pointer-coarse:h-10" onClick={handleSave} disabled={!dirty || saving || loading || !schema || schema.fields.length === 0}>
            {saving ? <Spinner /> : null}
            {t('save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function PluginSettingControl({
  field,
  locale,
  value,
  clearedSecrets,
  fieldErrors,
  onChange,
  onToggleClearSecret,
}: {
  field: PluginSettingFieldView
  locale: string
  value: unknown
  clearedSecrets: Set<string>
  fieldErrors: Record<string, string>
  onChange: (next: unknown) => void
  onToggleClearSecret: (secretPath: string, cleared: boolean) => void
}) {
  const t = useTranslations('adminPlugins.settingsDrawer')
  const title = field.label || field.key
  const controlId = `plugin-setting-${field.key}`
  const error = fieldErrors[field.key]
  const secretCleared = clearedSecrets.has(field.key)

  if (field.type === 'boolean') {
    return (
      <Field data-invalid={error ? true : undefined}>
        <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2.5">
          <div className="min-w-0 space-y-0.5">
            <FieldLabel htmlFor={controlId}>{title}</FieldLabel>
            {field.description ? <FieldDescription>{field.description}</FieldDescription> : null}
          </div>
          <Switch id={controlId} checked={value === true} onCheckedChange={(checked) => onChange(Boolean(checked))} />
        </div>
        {error ? <FieldError>{error}</FieldError> : null}
      </Field>
    )
  }

  let control: React.ReactNode
  switch (field.type) {
    case 'text':
    case 'markdown':
      control = (
        <Textarea
          id={controlId}
          rows={field.type === 'markdown' ? 5 : 3}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={error ? true : undefined}
        />
      )
      break
    case 'json':
      control = (
        <Textarea
          id={controlId}
          rows={6}
          spellCheck={false}
          className="font-mono text-xs"
          value={String(value ?? '{}')}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={error ? true : undefined}
        />
      )
      break
    case 'number':
      control = (
        <Input
          id={controlId}
          type="number"
          min={field.min}
          max={field.max}
          step={field.step}
          value={String(value ?? '')}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={error ? true : undefined}
        />
      )
      break
    case 'select':
    case 'choice': {
      const selectItems = [
        ...(!field.required ? [{ value: UNSET_SELECT, label: t('unsetOption') }] : []),
        ...(field.options ?? []).map((opt) => ({ value: opt, label: opt })),
      ]
      const cur =
        typeof value === 'string' && value !== ''
          ? value
          : field.required
            ? null
            : UNSET_SELECT
      control = (
        <Select
          items={selectItems}
          value={cur}
          onValueChange={(next) => {
            if (next != null) onChange(next === UNSET_SELECT ? '' : next)
          }}
        >
          <SelectTrigger id={controlId} aria-invalid={error ? true : undefined}>
            <SelectValue placeholder={t('selectPlaceholder')} />
          </SelectTrigger>
          <SelectContent>
            {!field.required ? <SelectItem value={UNSET_SELECT}>{t('unsetOption')}</SelectItem> : null}
            {(field.options ?? []).map((opt) => (
              <SelectItem key={opt} value={opt}>
                {opt}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
      break
    }
    case 'multiselect': {
      const selected = Array.isArray(value) ? value.map(String) : []
      control = (
        <div id={controlId} className="flex flex-wrap gap-2 rounded-lg border border-border p-2.5">
          {(field.options ?? []).map((opt) => {
            const checked = selected.includes(opt)
            return (
              <label key={opt} className="inline-flex min-h-8 cursor-pointer items-center gap-1.5 text-sm pointer-coarse:min-h-10">
                <Checkbox
                  checked={checked}
                  onCheckedChange={(next) => {
                    const on = Boolean(next)
                    onChange(on ? [...selected, opt] : selected.filter((x) => x !== opt))
                  }}
                />
                <span>{opt}</span>
              </label>
            )
          })}
        </div>
      )
      break
    }
    case 'color': {
      const str = String(value ?? '#000000')
      const hex = /^#[0-9a-fA-F]{6}$/.test(str) ? str : '#000000'
      control = (
        <div className="flex items-center gap-2">
          <input
            type="color"
            aria-label={title}
            value={hex}
            onChange={(e) => onChange(e.target.value)}
            className="h-9 w-11 cursor-pointer rounded-md border border-input bg-background p-0.5 pointer-coarse:h-10"
          />
          <Input id={controlId} value={str} onChange={(e) => onChange(e.target.value)} className="font-mono" aria-invalid={error ? true : undefined} />
        </div>
      )
      break
    }
    case 'image': {
      const src = String(value ?? '')
      const safeSrc = safeImageUrl(src)
      control = (
        <div className="space-y-2">
          <Input id={controlId} value={src} placeholder={t('imageHint')} onChange={(e) => onChange(e.target.value)} aria-invalid={error ? true : undefined} />
          {safeSrc ? (
            <img src={safeSrc} alt={title} className="max-h-24 rounded-md border border-border object-contain" />
          ) : null}
        </div>
      )
      break
    }
    case 'secret':
      control = (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Input
              id={controlId}
              type="password"
              autoComplete="new-password"
              disabled={secretCleared}
              value={String(value ?? '')}
              placeholder={field.isSet && !secretCleared ? t('secretSetPlaceholder') : t('secretUnsetPlaceholder')}
              onChange={(e) => {
                onChange(e.target.value)
                if (e.target.value.length > 0 && secretCleared) {
                  onToggleClearSecret(field.key, false)
                }
              }}
              aria-invalid={error ? true : undefined}
            />
            {field.isSet ? (
              <Badge variant="outline" className="shrink-0">
                {t('secretSetBadge')}
              </Badge>
            ) : null}
          </div>
          {field.isSet ? (
            <label className="inline-flex cursor-pointer items-center gap-2 text-xs text-muted-foreground pointer-coarse:min-h-10">
              <Checkbox checked={secretCleared} onCheckedChange={(c) => onToggleClearSecret(field.key, Boolean(c))} />
              <span>{t('clearSecret')}</span>
            </label>
          ) : null}
        </div>
      )
      break
    case 'list': {
      const items = Array.isArray(value) ? value.map(String) : []
      const itemType = typeof field.item?.type === 'string' ? field.item.type : 'string'
      control = (
        <div id={controlId} className="space-y-2">
          {items.map((item, idx) => (
            <div key={idx} className="flex items-center gap-2">
              <Input
                type={itemType === 'number' ? 'number' : 'text'}
                className={itemType === 'json' || itemType === 'group' ? 'font-mono text-xs' : undefined}
                value={item}
                onChange={(e) => {
                  const next = [...items]
                  next[idx] = e.target.value
                  onChange(next)
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="pointer-coarse:size-10"
                aria-label={t('removeItem')}
                onClick={() => onChange(items.filter((_, i) => i !== idx))}
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="outline" size="sm" className="pointer-coarse:h-10" onClick={() => onChange([...items, ''])}>
            <Plus />
            {t('addListItem')}
          </Button>
        </div>
      )
      break
    }
    case 'group': {
      if (field.fields && typeof value === 'object' && value !== null && !Array.isArray(value)) {
        const obj = value as Record<string, unknown>
        const subEntries = Object.entries(field.fields)
        control = (
          <div id={controlId} className={cn('space-y-4 rounded-lg border border-border bg-muted/30 p-3')}>
            {subEntries.map(([subKey, rawSub]) => {
              const subField = groupSubfieldToView(field.key, subKey, rawSub, locale)
              return (
                <PluginSettingControl
                  key={subKey}
                  field={subField}
                  locale={locale}
                  value={obj[subKey]}
                  clearedSecrets={clearedSecrets}
                  fieldErrors={fieldErrors}
                  onChange={(nextSubVal) => onChange({ ...obj, [subKey]: nextSubVal })}
                  onToggleClearSecret={onToggleClearSecret}
                />
              )
            })}
          </div>
        )
      } else {
        control = (
          <Textarea
            id={controlId}
            rows={5}
            spellCheck={false}
            className="font-mono text-xs"
            value={String(value ?? '{}')}
            onChange={(e) => onChange(e.target.value)}
            aria-invalid={error ? true : undefined}
          />
        )
      }
      break
    }
    default:
      control = <Input id={controlId} value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} aria-invalid={error ? true : undefined} />
      break
  }

  return (
    <Field data-invalid={error ? true : undefined}>
      <FieldLabel htmlFor={controlId}>{title}</FieldLabel>
      {control}
      {field.description ? <FieldDescription>{field.description}</FieldDescription> : null}
      {error ? <FieldError>{error}</FieldError> : null}
    </Field>
  )
}
