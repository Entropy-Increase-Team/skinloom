import type { PackageInspectResult, PermissionGroups, PluginSettingFieldView } from '@skinloom/api-client'

export type PluginSettingsDraft = Record<string, unknown>

const UNSET_SELECT = '__unset'

function isEqualValue(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false
    return a.every((v, i) => isEqualValue(v, b[i]))
  }
  const ka = Object.keys(a as Record<string, unknown>)
  const kb = Object.keys(b as Record<string, unknown>)
  if (ka.length !== kb.length) return false
  return ka.every((k) => k in (b as Record<string, unknown>) && isEqualValue((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
}

export function resolveLocalizedFieldText(raw: unknown, locale: string, fallback: string): string {
  if (typeof raw === 'string' && raw.trim() !== '') return raw
  if (typeof raw === 'object' && raw !== null) {
    const rec = raw as Record<string, unknown>
    const exact = rec[locale]
    if (typeof exact === 'string' && exact.trim() !== '') return exact
    if (typeof rec.en === 'string' && rec.en.trim() !== '') return rec.en
    for (const v of Object.values(rec)) {
      if (typeof v === 'string' && v.trim() !== '') return v
    }
  }
  return fallback
}

export function groupSubfieldToView(
  parentKey: string,
  subKey: string,
  rawSub: unknown,
  locale = 'en',
): PluginSettingFieldView {
  const rec = typeof rawSub === 'object' && rawSub !== null ? (rawSub as Record<string, unknown>) : {}
  const itemRaw = typeof rec.item === 'object' && rec.item !== null ? (rec.item as Record<string, unknown>) : undefined
  const fieldsRaw =
    typeof rec.fields === 'object' && rec.fields !== null ? (rec.fields as Record<string, unknown>) : undefined
  const desc = resolveLocalizedFieldText(rec.description, locale, '')
  return {
    key: `${parentKey}.${subKey}`,
    type: typeof rec.type === 'string' ? rec.type : 'string',
    label: resolveLocalizedFieldText(rec.label, locale, subKey),
    description: desc !== '' ? desc : undefined,
    default: rec.default,
    value: undefined,
    min: typeof rec.min === 'number' ? rec.min : undefined,
    max: typeof rec.max === 'number' ? rec.max : undefined,
    step: typeof rec.step === 'number' ? rec.step : undefined,
    options: Array.isArray(rec.options) ? rec.options.map(String) : undefined,
    required: typeof rec.required === 'boolean' ? rec.required : undefined,
    secret: rec.type === 'secret' || rec.secret === true,
    isSet: Boolean(rec.isSet),
    item: itemRaw as PluginSettingFieldView['item'],
    fields: fieldsRaw as PluginSettingFieldView['fields'],
  }
}

function initialDraftValue(field: PluginSettingFieldView, rawVal: unknown): unknown {
  switch (field.type) {
    case 'boolean':
      return rawVal === true
    case 'number':
      return rawVal === undefined || rawVal === null ? '' : String(rawVal)
    case 'multiselect':
      return Array.isArray(rawVal) ? rawVal.map(String) : []
    case 'list':
      return Array.isArray(rawVal) ? rawVal.map((item) => (typeof item === 'string' ? item : JSON.stringify(item))) : []
    case 'json':
      return rawVal === undefined || rawVal === null ? '{}' : JSON.stringify(rawVal, null, 2)
    case 'group':
      if (field.fields) {
        const src = typeof rawVal === 'object' && rawVal !== null && !Array.isArray(rawVal) ? (rawVal as Record<string, unknown>) : {}
        const out: Record<string, unknown> = {}
        for (const [subKey, rawSub] of Object.entries(field.fields)) {
          const subField = groupSubfieldToView(field.key, subKey, rawSub)
          const subRaw = subKey in src ? src[subKey] : subField.default
          out[subKey] = initialDraftValue(subField, subRaw)
        }
        return out
      }
      return rawVal === undefined || rawVal === null ? '{}' : JSON.stringify(rawVal, null, 2)
    case 'secret':
      // Never pre-populate stored secret strings; empty means keep unchanged on save.
      return ''
    default:
      return rawVal === undefined || rawVal === null ? '' : String(rawVal)
  }
}

/** Builds editable UI draft state from server-returned schema fields. */
export function buildPluginSettingsDraft(fields: PluginSettingFieldView[]): PluginSettingsDraft {
  const draft: PluginSettingsDraft = {}
  for (const field of fields) {
    const rawVal = field.value ?? field.default
    draft[field.key] = initialDraftValue(field, rawVal)
  }
  return draft
}

export interface BuildPatchResult {
  patch: Record<string, unknown>
  errors: Record<string, string>
}

function convertFieldValue(
  field: PluginSettingFieldView,
  val: unknown,
  clearedSecrets: Set<string>,
  pathKey: string,
  errors: Record<string, string>,
  hasInitial: boolean,
  initialVal: unknown,
): { include: boolean; serialized?: unknown } {
  const unchanged = hasInitial && isEqualValue(val, initialVal)

  switch (field.type) {
    case 'secret': {
      if (clearedSecrets.has(pathKey)) {
        if (field.required) {
          errors[pathKey] = 'this field is required'
          return { include: false }
        }
        return { include: true, serialized: null }
      }
      const s = typeof val === 'string' ? val.trim() : ''
      if (s !== '' && s !== '********') {
        return { include: true, serialized: s }
      }
      return { include: false }
    }
    case 'boolean': {
      const next = val === true
      if (hasInitial && next === (initialVal === true)) return { include: false }
      return { include: true, serialized: next }
    }
    case 'number': {
      const raw = String(val ?? '').trim()
      if (raw === '') {
        if (field.required && (!hasInitial || String(initialVal ?? '').trim() !== '')) {
          errors[pathKey] = 'this field is required'
          return { include: false }
        }
        if (unchanged) return { include: false }
        return { include: true, serialized: null }
      }
      const num = Number(raw)
      if (!Number.isFinite(num)) {
        errors[pathKey] = 'must be a finite number'
        return { include: false }
      }
      if (field.min !== undefined && num < field.min) {
        errors[pathKey] = `must be >= ${field.min}`
        return { include: false }
      }
      if (field.max !== undefined && num > field.max) {
        errors[pathKey] = `must be <= ${field.max}`
        return { include: false }
      }
      if (unchanged) return { include: false }
      return { include: true, serialized: num }
    }
    case 'select':
    case 'choice': {
      const raw = typeof val === 'string' && val !== UNSET_SELECT ? val : ''
      if (raw === '') {
        if (field.required && (!hasInitial || String(initialVal ?? '') !== '')) {
          errors[pathKey] = 'this field is required'
          return { include: false }
        }
        if (hasInitial && (initialVal === '' || initialVal === UNSET_SELECT || initialVal === undefined || initialVal === null)) {
          return { include: false }
        }
        return { include: true, serialized: null }
      }
      if (unchanged) return { include: false }
      return { include: true, serialized: raw }
    }
    case 'multiselect': {
      const next = Array.isArray(val) ? val.map(String) : []
      if (field.required && next.length === 0 && (!hasInitial || !unchanged)) {
        errors[pathKey] = 'this field is required'
        return { include: false }
      }
      if (unchanged) return { include: false }
      return { include: true, serialized: next }
    }
    case 'list': {
      const items = Array.isArray(val) ? val : []
      const parsed: unknown[] = []
      const itemType = typeof field.item?.type === 'string' ? field.item.type : 'string'
      for (const item of items) {
        if (itemType === 'number') {
          if (String(item).trim() === '') {
            errors[pathKey] = 'list items must be valid numbers'
            break
          }
          const n = Number(item)
          if (!Number.isFinite(n)) {
            errors[pathKey] = 'list items must be valid numbers'
            break
          }
          parsed.push(n)
        } else if (itemType === 'json' || itemType === 'group') {
          try {
            if (typeof item === 'string') {
              if (!item.trim()) {
                errors[pathKey] = 'invalid JSON in list item'
                break
              }
              parsed.push(JSON.parse(item))
            } else {
              parsed.push(item)
            }
          } catch {
            errors[pathKey] = 'invalid JSON in list item'
            break
          }
        } else {
          parsed.push(String(item))
        }
      }
      if (errors[pathKey]) return { include: false }
      if (unchanged) return { include: false }
      return { include: true, serialized: parsed }
    }
    case 'json': {
      const raw = typeof val === 'string' ? val.trim() : ''
      if (!raw) {
        if (field.required && (!hasInitial || String(initialVal ?? '').trim() !== '')) {
          errors[pathKey] = 'this field is required'
          return { include: false }
        }
        if (unchanged) return { include: false }
        return { include: true, serialized: null }
      }
      try {
        const parsed = JSON.parse(raw)
        if (unchanged) return { include: false }
        return { include: true, serialized: parsed }
      } catch {
        errors[pathKey] = 'invalid JSON syntax'
        return { include: false }
      }
    }
    case 'group': {
      if (field.fields && typeof val === 'object' && val !== null && !Array.isArray(val)) {
        const obj = val as Record<string, unknown>
        const initObj =
          hasInitial && typeof initialVal === 'object' && initialVal !== null && !Array.isArray(initialVal)
            ? (initialVal as Record<string, unknown>)
            : {}
        const subPatch: Record<string, unknown> = {}
        for (const [subKey, rawSub] of Object.entries(field.fields)) {
          const subField = groupSubfieldToView(field.key, subKey, rawSub)
          const subPath = `${pathKey}.${subKey}`
          const subRes = convertFieldValue(subField, obj[subKey], clearedSecrets, subPath, errors, hasInitial, initObj[subKey])
          if (subRes.include) {
            subPatch[subKey] = subRes.serialized
          }
        }
        if (Object.keys(subPatch).length === 0) return { include: false }
        return { include: true, serialized: subPatch }
      }
      const raw = typeof val === 'string' ? val.trim() : ''
      if (!raw) {
        if (unchanged) return { include: false }
        return { include: true, serialized: {} }
      }
      try {
        const obj = JSON.parse(raw)
        if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) {
          errors[pathKey] = 'must be a JSON object'
          return { include: false }
        }
        if (unchanged) return { include: false }
        return { include: true, serialized: obj }
      } catch {
        errors[pathKey] = 'invalid JSON object'
        return { include: false }
      }
    }
    default: {
      const str = val === undefined || val === null ? '' : String(val)
      if (field.required && str.trim() === '' && (!hasInitial || String(initialVal ?? '').trim() !== '')) {
        errors[pathKey] = 'this field is required'
        return { include: false }
      }
      if (unchanged) return { include: false }
      return { include: true, serialized: str }
    }
  }
}

/**
 * Validates and converts the settings draft into a sparse PATCH payload.
 * Untouched secret fields are omitted so existing secrets remain intact; explicitly
 * cleared secrets send `null`. When `initialDraft` is passed, unchanged fields are omitted.
 */
export function buildPluginSettingsPatch(
  fields: PluginSettingFieldView[],
  draft: PluginSettingsDraft,
  clearedSecrets: Set<string>,
  initialDraft?: PluginSettingsDraft,
): BuildPatchResult {
  const patch: Record<string, unknown> = {}
  const errors: Record<string, string> = {}
  const hasInitial = initialDraft !== undefined

  for (const field of fields) {
    const res = convertFieldValue(field, draft[field.key], clearedSecrets, field.key, errors, hasInitial, initialDraft?.[field.key])
    if (res.include) {
      patch[field.key] = res.serialized
    }
  }
  return { patch, errors }
}

/** Returns true when at least one setting differs from `initialDraft` or a secret is queued for clearing. */
export function isPluginSettingsDirty(
  fields: PluginSettingFieldView[],
  draft: PluginSettingsDraft,
  clearedSecrets: Set<string>,
  initialDraft: PluginSettingsDraft,
): boolean {
  if (clearedSecrets.size > 0) return true
  for (const field of fields) {
    if (field.type === 'secret') {
      if (typeof draft[field.key] === 'string' && String(draft[field.key]).trim() !== '') return true
      continue
    }
    if (!isEqualValue(draft[field.key], initialDraft[field.key])) return true
  }
  return false
}

/** Collects initially checked permission names when opening the package inspect dialog. */
export function collectInitialGrantedPermissions(groups: PermissionGroups): string[] {
  const out: string[] = []
  for (const item of [...groups.normal, ...groups.sensitive, ...groups.high_risk]) {
    if (item.granted) out.push(item.name)
  }
  return out
}

/** Enforces superadmin package trust, dual-tier native policy, and high-risk ID confirmation before submit. */
export function canSubmitPackageInstall(
  inspect: PackageInspectResult | null,
  granted: string[],
  allowUnsigned: boolean,
  confirmIdInput: string,
): boolean {
  if (!inspect) return false
  if (inspect.nativeInstallBlocked) return false
  if (inspect.runtime === 'native' && !inspect.platformCompatible) return false
  if (!inspect.verification.trusted && !allowUnsigned) return false
  const isNative = inspect.runtime === 'native'
  const hasHighRiskGranted = isNative || inspect.permissions.high_risk.some((p) => granted.includes(p.name))
  if ((inspect.requiresHighRiskConfirm || isNative) && hasHighRiskGranted && confirmIdInput.trim() !== inspect.id) {
    return false
  }
  if (!inspect.verification.trusted && confirmIdInput.trim() !== inspect.id) {
    return false
  }
  return true
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MiB`
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0s'
  const total = Math.floor(seconds)
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const mins = Math.floor((total % 3600) / 60)
  const secs = total % 60
  if (days > 0) return `${days}d ${hours}h ${mins}m`
  if (hours > 0) return `${hours}h ${mins}m`
  if (mins > 0) return `${mins}m ${secs}s`
  return `${secs}s`
}

export function apiProblemMessage(problemDetail: string | undefined, _err: unknown, fallback: string): string {
  return problemDetail || fallback
}
