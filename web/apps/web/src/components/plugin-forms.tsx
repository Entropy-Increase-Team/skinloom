'use client'

import type { PluginFormField } from '@skinloom/api-client'
import type { FormScope } from '@skinloom/plugin-sdk/catalog'
import {
  Checkbox,
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
  Input,
  Label,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@skinloom/ui'

import { useBootstrap } from '@/components/providers'

/** Validates required declarative plugin form fields before submitting a `noValidate` form. */
export function findMissingPluginFormErrors(
  forms: Record<string, PluginFormField[]> | undefined,
  scope: FormScope,
  values: Record<string, unknown>,
  requiredMessage: string,
): Record<string, string> {
  const defs = forms?.[scope] ?? []
  const counts = new Map<string, number>()
  for (const f of defs) {
    counts.set(f.name, (counts.get(f.name) ?? 0) + 1)
  }
  const errors: Record<string, string> = {}
  for (const f of defs) {
    if (!f.required) continue
    const collide = (counts.get(f.name) ?? 0) > 1
    const key = collide ? `${f.pluginId}:${f.name}` : f.name
    const raw = collide ? values[key] : (values[key] ?? values[f.name])
    const empty =
      f.type === 'boolean' || f.type === 'checkbox'
        ? raw !== true
        : raw === undefined || raw === null || (typeof raw === 'string' && raw.trim() === '')
    if (empty) {
      errors[key] = requiredMessage
      if (collide) {
        errors[`form.${f.pluginId}.${f.name}`] = requiredMessage
      } else {
        errors[`form.${f.name}`] = requiredMessage
      }
    }
  }
  return errors
}

/**
 * Renders declarative extension form fields declared by active plugins under
 * `manifest.forms[scope]` and reports field values into the enclosing form's `form` map.
 */
export function PluginForms({
  scope,
  values,
  onChange,
  errors,
}: {
  scope: FormScope
  values: Record<string, unknown>
  onChange: (next: Record<string, unknown>) => void
  errors?: Record<string, string>
}) {
  const boot = useBootstrap()
  const defs = boot.forms?.[scope] ?? []
  if (defs.length === 0) return null

  const nameCounts = new Map<string, number>()
  for (const f of defs) {
    nameCounts.set(f.name, (nameCounts.get(f.name) ?? 0) + 1)
  }

  const setField = (f: PluginFormField, value: unknown) => {
    const collide = (nameCounts.get(f.name) ?? 0) > 1
    if (collide) {
      onChange({ ...values, [`${f.pluginId}:${f.name}`]: value })
      return
    }
    onChange({ ...values, [f.name]: value })
  }

  return (
    <div data-sl-form-scope={scope} className="flex flex-col gap-4">
      {defs.map((f) => {
        const collide = (nameCounts.get(f.name) ?? 0) > 1
        const scopedKey = collide ? `${f.pluginId}:${f.name}` : f.name
        const inputId = `plugin-form-${scope}-${f.pluginId}-${f.name}`
        const fieldError = collide
          ? (errors?.[scopedKey] ?? errors?.[`form.${f.pluginId}.${f.name}`] ?? errors?.[`form.${scopedKey}`])
          : (errors?.[scopedKey] ??
              errors?.[`form.${f.pluginId}.${f.name}`] ??
              errors?.[`form.${f.name}`] ??
              errors?.[f.name])
        const rawValue = collide ? values[scopedKey] : (values[scopedKey] ?? values[f.name])

        if (f.type === 'boolean' || f.type === 'checkbox') {
          return (
            <Field key={`${f.pluginId}:${f.name}`} data-invalid={fieldError ? true : undefined}>
              <Label className="w-fit gap-2 font-normal">
                <Checkbox
                  id={inputId}
                  required={Boolean(f.required)}
                  checked={Boolean(rawValue)}
                  onCheckedChange={(checked) => setField(f, checked)}
                  aria-invalid={fieldError ? true : undefined}
                />
                {f.label}
              </Label>
              {f.description && <FieldDescription>{f.description}</FieldDescription>}
              {fieldError && <FieldError>{fieldError}</FieldError>}
            </Field>
          )
        }

        if (f.type === 'select' && f.options && f.options.length > 0) {
          const items = f.options.map((opt) => ({ value: opt, label: opt }))
          const val = typeof rawValue === 'string' ? rawValue : ''
          return (
            <Field key={`${f.pluginId}:${f.name}`} data-invalid={fieldError ? true : undefined}>
              <FieldLabel htmlFor={inputId}>{f.label}</FieldLabel>
              <Select
                items={items}
                required={Boolean(f.required)}
                value={val}
                onValueChange={(next) => setField(f, next)}
              >
                <SelectTrigger id={inputId} aria-invalid={fieldError ? true : undefined}>
                  <SelectValue placeholder={f.placeholder || f.label} />
                </SelectTrigger>
                <SelectContent>
                  {items.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value}>
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {f.description && <FieldDescription>{f.description}</FieldDescription>}
              {fieldError && <FieldError>{fieldError}</FieldError>}
            </Field>
          )
        }

        const inputType = f.type === 'number' || f.type === 'email' || f.type === 'password' ? f.type : 'text'
        return (
          <Field key={`${f.pluginId}:${f.name}`} data-invalid={fieldError ? true : undefined}>
            <FieldLabel htmlFor={inputId}>{f.label}</FieldLabel>
            <Input
              id={inputId}
              name={`form.${f.name}`}
              type={inputType}
              required={Boolean(f.required)}
              placeholder={f.placeholder || undefined}
              maxLength={f.maxLength && f.maxLength > 0 ? f.maxLength : undefined}
              value={rawValue === undefined || rawValue === null ? '' : String(rawValue)}
              onChange={(e) => {
                if (inputType === 'number') {
                  setField(f, e.target.value === '' ? '' : Number(e.target.value))
                } else {
                  setField(f, e.target.value)
                }
              }}
              aria-invalid={fieldError ? true : undefined}
            />
            {f.description && <FieldDescription>{f.description}</FieldDescription>}
            {fieldError && <FieldError>{fieldError}</FieldError>}
          </Field>
        )
      })}
    </div>
  )
}
