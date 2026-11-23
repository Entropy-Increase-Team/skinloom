import { existsSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'
import net from 'node:net'
import {
  FORM_SCOPES,
  HOOK_NAMES,
  NATIVE_ONLY_PERMISSIONS,
  PERMISSION_RISK,
  PLUGIN_SLOTS,
  THEME_OVERRIDE_TARGETS,
} from '@skinloom/plugin-sdk/catalog'
import {
  checkSafeRelPath,
  COLL_NAME_RE,
  effectiveRuntime,
  INDEX_FIELD_RE,
  isNativeExecutablePath,
  loadManifest,
  MAX_ASSET_FILE_BYTES,
  MAX_NATIVE_BINARY_BYTES,
  MAX_NATIVE_UNPACKED_FAT_BYTES,
  MAX_NATIVE_UNPACKED_SINGLE_BYTES,
  MAX_PACKAGE_TOTAL_BYTES,
  MAX_SERVER_BUNDLE_BYTES,
  NODE_BUILTIN_MODULES,
  PLATFORM_RE,
  PLUGIN_ID_RE,
  resolveLocalizedText,
  THEME_ID_RE,
  walkFiles,
  type ManifestJson,
} from './manifest.js'

export interface LintIssue {
  severity: 'error' | 'warning'
  code: string
  message: string
  file?: string
}

export interface LintResult {
  ok: boolean
  errors: LintIssue[]
  warnings: LintIssue[]
}

const VALID_PERMISSIONS = new Set<string>(Object.keys(PERMISSION_RISK))
const NATIVE_ONLY_PERM_SET = new Set<string>(NATIVE_ONLY_PERMISSIONS)
const VALID_GUARD_HOOKS = new Set<string>(HOOK_NAMES.guard)
const VALID_FILTER_HOOKS = new Set<string>(HOOK_NAMES.filter)
const VALID_EVENT_HOOKS = new Set<string>(HOOK_NAMES.event)
const VALID_SLOTS = new Set<string>(PLUGIN_SLOTS)
const VALID_FORM_SCOPES = new Set<string>(FORM_SCOPES)
const VALID_THEME_OVERRIDES = new Set<string>(THEME_OVERRIDE_TARGETS)
const VALID_MENU_AREAS = new Set(['user', 'admin', 'explore'])
const VALID_MENU_ROLES = new Set(['admin', 'superadmin'])
const VALID_SETTING_TYPES = new Set([
  'string',
  'text',
  'markdown',
  'number',
  'boolean',
  'select',
  'multiselect',
  'color',
  'image',
  'secret',
  'json',
  'list',
  'group',
  'choice',
])
const VALID_INDEX_SLOTS = new Set(['string:unique', 'string', 'number', 'time'])
const TEST_FILE_RE = /(?:\.test|\.spec)\.(?:[cm]?[jt]sx?)$/i
const SEMVER_RE = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?$/

interface CtxRule {
  pattern: RegExp
  permission: string
  description: string
}

const CTX_PERMISSION_RULES: CtxRule[] = [
  { pattern: /\bctx\.users\.(?:get|find|list)\s*\(/g, permission: 'users.read', description: 'ctx.users read methods' },
  { pattern: /\bctx\.users\.update\s*\(/g, permission: 'users.write', description: 'ctx.users.update' },
  { pattern: /\bctx\.users\.ban\s*\(/g, permission: 'users.ban', description: 'ctx.users.ban' },
  { pattern: /\bctx\.players\.(?:get|findByName|list)\s*\(/g, permission: 'players.read', description: 'ctx.players read methods' },
  { pattern: /\bctx\.players\.(?:create|rename|setTextures|delete)\s*\(/g, permission: 'players.write', description: 'ctx.players write methods' },
  { pattern: /\bctx\.textures\.(?:get|list|readBytes)\s*\(/g, permission: 'textures.read', description: 'ctx.textures read methods' },
  { pattern: /\bctx\.textures\.(?:create|update|delete)\s*\(/g, permission: 'textures.write', description: 'ctx.textures write methods' },
  { pattern: /\bctx\.closet\.\w+\s*\(/g, permission: 'closet.write', description: 'ctx.closet' },
  { pattern: /\bctx\.score\.apply\s*\(/g, permission: 'score.write', description: 'ctx.score.apply' },
  { pattern: /\bctx\.notify\.\w+\s*\(/g, permission: 'notify.send', description: 'ctx.notify' },
  { pattern: /\bctx\.mail\.\w+\s*\(/g, permission: 'mail.send', description: 'ctx.mail' },
  { pattern: /\bctx\.fetch\s*\(/g, permission: 'http.fetch', description: 'ctx.fetch' },
  { pattern: /\bctx\.kv\.\w+\s*\(/g, permission: 'storage.kv', description: 'ctx.kv' },
  { pattern: /\bctx\.db\.\w+\s*\(/g, permission: 'storage.docs', description: 'ctx.db' },
  { pattern: /\bctx\.files\.\w+\s*\(/g, permission: 'storage.files', description: 'ctx.files' },
  { pattern: /\bctx\.auth\.registerProvider\s*\(/g, permission: 'auth.provider', description: 'ctx.auth.registerProvider' },
  { pattern: /\bctx\.auth\.registerPasswordVerifier\s*\(/g, permission: 'auth.password', description: 'ctx.auth.registerPasswordVerifier' },
  { pattern: /\bctx\.captcha\.\w+\s*\(/g, permission: 'captcha.provider', description: 'ctx.captcha' },
  { pattern: /\bctx\.cdn\.\w+\s*\(/g, permission: 'cdn.provider', description: 'ctx.cdn' },
  { pattern: /\bctx\.yggdrasil\.\w+\s*\(/g, permission: 'yggdrasil.extend', description: 'ctx.yggdrasil' },
  { pattern: /\bctx\.cron\.\w+\s*\(/g, permission: 'cron', description: 'ctx.cron' },
]

function pushIssue(issues: LintIssue[], severity: LintIssue['severity'], code: string, message: string, file?: string): void {
  issues.push(file ? { severity, code, message, file } : { severity, code, message })
}

function validateHostAllowEntry(host: string): string | null {
  const trimmed = host.trim()
  if (!trimmed || trimmed === '*' || /[/:@?#\s]/.test(trimmed)) {
    return `invalid http.allow host "${host}"`
  }
  const bare = trimmed.replace(/^\[|\]$/g, '').toLowerCase().replace(/\.+$/, '')
  if (!bare || net.isIP(bare) !== 0) {
    return `IP literals are not allowed in http.allow ("${host}")`
  }
  if (bare === 'localhost' || bare.endsWith('.localhost') || bare.endsWith('.local') || bare.endsWith('.internal')) {
    return `internal hostnames are not allowed in http.allow ("${host}")`
  }
  if (trimmed.startsWith('*.') && trimmed.split('*').length - 1 === 1 && trimmed.length > 3) {
    return null
  }
  if (trimmed.includes('*')) {
    return `wildcard only allowed as leading "*." in http.allow ("${host}")`
  }
  return null
}

function isValidSemverConstraint(raw: string): boolean {
  const trimmed = raw.trim()
  if (!trimmed) return false
  if (trimmed === '*') return true
  const tokens = trimmed.split(/\s+/).filter(Boolean)
  const tokenRe = /^(?:\^|~|>=|<=|>|<|=)?\d+(?:\.(?:\d+|x|X|\*)){0,2}(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?$/
  return tokens.length > 0 && tokens.every((tok) => tokenRe.test(tok))
}

function validateSettingsMap(settings: Record<string, unknown>, prefix: string, issues: LintIssue[]): void {
  for (const [key, rawField] of Object.entries(settings)) {
    const fieldPath = prefix ? `${prefix}.${key}` : key
    if (!rawField || typeof rawField !== 'object' || Array.isArray(rawField)) {
      pushIssue(issues, 'error', 'manifest.settings', `Setting "${fieldPath}" must be an object`, 'plugin.json')
      continue
    }
    const field = rawField as Record<string, unknown>
    if (typeof field.type !== 'string' || !VALID_SETTING_TYPES.has(field.type)) {
      pushIssue(
        issues,
        'error',
        'manifest.settings.type',
        `Setting "${fieldPath}" has unsupported type "${String(field.type ?? '')}"`,
        'plugin.json',
      )
      continue
    }
    if (field.type === 'group' && field.fields && typeof field.fields === 'object' && !Array.isArray(field.fields)) {
      validateSettingsMap(field.fields as Record<string, unknown>, fieldPath, issues)
    }
  }
}

function lintManifest(manifest: ManifestJson, issues: LintIssue[]): void {
  if (manifest.kind === 'plugin') {
    if (!PLUGIN_ID_RE.test(manifest.id ?? '')) {
      pushIssue(issues, 'error', 'manifest.id', `Plugin id "${manifest.id ?? ''}" must match ${PLUGIN_ID_RE.source}`, 'plugin.json')
    }
  } else if (manifest.kind === 'theme') {
    if (!THEME_ID_RE.test(manifest.id ?? '') || manifest.id === '_custom') {
      pushIssue(issues, 'error', 'manifest.id', `Theme id "${manifest.id ?? ''}" must match ${THEME_ID_RE.source} and not be "_custom"`, 'plugin.json')
    }
  } else {
    pushIssue(issues, 'error', 'manifest.kind', `kind must be "plugin" or "theme", got "${String(manifest.kind ?? '')}"`, 'plugin.json')
  }

  if (!manifest.version || manifest.version.length > 64 || !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/.test(manifest.version) || manifest.version.includes('..')) {
    pushIssue(issues, 'error', 'manifest.version', 'version must be 1-64 safe version characters without ".."', 'plugin.json')
  } else if (!SEMVER_RE.test(manifest.version)) {
    pushIssue(issues, 'warning', 'manifest.version.semver', `version "${manifest.version}" is not valid SemVer`, 'plugin.json')
  }

  if (manifest.apiVersion !== 1) {
    pushIssue(issues, 'error', 'manifest.apiVersion', `apiVersion must be 1, got ${String(manifest.apiVersion)}`, 'plugin.json')
  }
  if (!resolveLocalizedText(manifest.name)) {
    pushIssue(issues, 'error', 'manifest.name', 'name must be a non-empty string or locale map', 'plugin.json')
  }
  if (manifest.extends && manifest.extends === manifest.id) {
    pushIssue(issues, 'error', 'manifest.extends', `Theme "${manifest.id}" cannot extend itself`, 'plugin.json')
  }

  if (manifest.runtime !== undefined && manifest.runtime !== 'js' && manifest.runtime !== 'native') {
    pushIssue(issues, 'error', 'manifest.runtime', `runtime must be "js" or "native", got "${String(manifest.runtime)}"`, 'plugin.json')
  }
  const rt = effectiveRuntime(manifest)
  if (manifest.kind === 'theme' && rt === 'native') {
    pushIssue(issues, 'error', 'manifest.runtime', 'Themes cannot use runtime "native"', 'plugin.json')
  }
  if (rt === 'native') {
    if (manifest.server) {
      pushIssue(issues, 'error', 'manifest.native', 'Native plugins must use native.executable instead of JS "server" entry', 'plugin.json')
    }
    if (!manifest.native || !manifest.native.executable) {
      pushIssue(issues, 'error', 'manifest.native.executable', 'Native plugins require native.executable', 'plugin.json')
    } else {
      const execErr = checkSafeRelPath(manifest.native.executable, 'native.executable')
      if (execErr) pushIssue(issues, 'error', 'manifest.path', execErr, 'plugin.json')
      if (manifest.native.protocolVersion !== 1) {
        pushIssue(issues, 'error', 'manifest.native.protocolVersion', `native.protocolVersion must be 1, got ${String(manifest.native.protocolVersion)}`, 'plugin.json')
      }
      const seenPlats = new Set<string>()
      for (const plat of manifest.native.platforms ?? []) {
        if (!PLATFORM_RE.test(plat)) {
          pushIssue(issues, 'error', 'manifest.native.platforms', `Invalid platform "${plat}" (expected os/arch like linux/amd64)`, 'plugin.json')
        } else if (seenPlats.has(plat)) {
          pushIssue(issues, 'error', 'manifest.native.platforms', `Duplicate platform "${plat}" in native.platforms`, 'plugin.json')
        } else {
          seenPlats.add(plat)
        }
      }
      const permSet = new Set(manifest.permissions ?? [])
      if (manifest.native.httpProxy && !permSet.has('http.stream')) {
        pushIssue(issues, 'error', 'manifest.native.httpProxy', 'native.httpProxy requires declaring "http.stream" permission', 'plugin.json')
      }
      if (manifest.native.database?.required && !permSet.has('storage.sql')) {
        pushIssue(issues, 'error', 'manifest.native.database', 'native.database.required requires declaring "storage.sql" permission', 'plugin.json')
      }
    }
  } else if (manifest.native) {
    pushIssue(issues, 'error', 'manifest.native', 'native block is only allowed when runtime is "native"', 'plugin.json')
  }

  for (const [field, val] of [
    ['server', manifest.server],
    ['web', manifest.web],
    ['icon', manifest.icon],
    ['screenshot', manifest.screenshot],
    ['theme.css', manifest.theme?.css],
  ] as const) {
    const err = checkSafeRelPath(val, field)
    if (err) pushIssue(issues, 'error', 'manifest.path', err, 'plugin.json')
  }

  for (const perm of manifest.permissions ?? []) {
    if (!VALID_PERMISSIONS.has(perm)) {
      pushIssue(issues, 'error', 'manifest.permission', `Unknown permission "${perm}"`, 'plugin.json')
    } else if (rt !== 'native' && NATIVE_ONLY_PERM_SET.has(perm)) {
      pushIssue(issues, 'error', 'manifest.permission.nativeOnly', `Permission "${perm}" is only available to runtime "native" plugins`, 'plugin.json')
    }
  }

  for (const host of manifest.http?.allow ?? []) {
    const err = validateHostAllowEntry(host)
    if (err) pushIssue(issues, 'error', 'manifest.http.allow', err, 'plugin.json')
  }

  const publicRoutes = manifest.routes?.public ?? []
  if (publicRoutes.length > 0) {
    const permSet = new Set(manifest.permissions ?? [])
    if (!permSet.has('routes.public')) {
      pushIssue(issues, 'error', 'manifest.routes.public', 'routes.public requires declaring "routes.public" permission', 'plugin.json')
    }
    const reservedHeads = new Set([
      'oauth', '.well-known', 'csl', 'usm', 'skin', 'cape', 'textures', 'raw',
      'avatar', 'preview', 'plugins-assets', 'themes-assets', 'events', 'healthz', 'readyz',
      '_next', 'auth', 'user', 'admin', 'setup', 'skinlib', 'p', 'favicon.ico', 'robots.txt',
    ])
    const reservedAPISubroutes = new Set([
      'v1', 'yggdrasil', 'plugins', 'user', 'players', 'closet', 'admin', 'docs', 'openapi', 'schemas',
    ])
    const seenRoutes = new Set<string>()
    for (const raw of publicRoutes) {
      const p = String(raw ?? '').trim()
      if (!p.startsWith('/') || /[\\?# \t\r\n]/.test(p) || p.includes('//') || p.includes('..')) {
        pushIssue(issues, 'error', 'manifest.routes.public', `Invalid routes.public pattern "${raw}"`, 'plugin.json')
        continue
      }
      const segs = p.replace(/^\/+|\/+$/g, '').split('/').filter(Boolean)
      if (segs.length === 0) {
        pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" cannot claim site root "/"`, 'plugin.json')
        continue
      }
      const head = segs[0]!
      if (head === '*' || head.startsWith(':') || head.includes('{') || head.includes('}')) {
        pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" must begin with a literal path segment`, 'plugin.json')
        continue
      }
      const lowerHead = head.toLowerCase()
      if (lowerHead === 'api') {
        if (segs.length < 2) {
          pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" conflicts with a reserved SkinLoom route`, 'plugin.json')
          continue
        }
        const sub = segs[1]!
        if (sub === '*' || sub.startsWith(':') || sub.includes('{') || sub.includes('}')) {
          pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" must begin with a literal sub-path under "/api/"`, 'plugin.json')
          continue
        }
        if (reservedAPISubroutes.has(sub.toLowerCase())) {
          pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" conflicts with a reserved SkinLoom route`, 'plugin.json')
          continue
        }
      } else if (reservedHeads.has(lowerHead) || (segs.length === 1 && lowerHead.endsWith('.json'))) {
        pushIssue(issues, 'error', 'manifest.routes.public', `routes.public pattern "${raw}" conflicts with a reserved SkinLoom route`, 'plugin.json')
        continue
      }
      if (segs.some((s, idx) => s === '*' && idx !== segs.length - 1)) {
        pushIssue(issues, 'error', 'manifest.routes.public', `Wildcard "*" in routes.public pattern "${raw}" must be the final segment`, 'plugin.json')
        continue
      }
      const norm = p === '/' ? '/' : p.replace(/\/+$/, '')
      if (seenRoutes.has(norm)) {
        pushIssue(issues, 'error', 'manifest.routes.public', `Duplicate routes.public pattern "${raw}"`, 'plugin.json')
      } else {
        seenRoutes.add(norm)
      }
    }
  }

  for (const g of manifest.hooks?.guard ?? []) {
    if (!VALID_GUARD_HOOKS.has(g)) {
      pushIssue(issues, 'error', 'manifest.hooks.guard', `Unknown guard hook "${g}"`, 'plugin.json')
    }
  }
  for (const f of manifest.hooks?.filter ?? []) {
    if (!VALID_FILTER_HOOKS.has(f)) {
      pushIssue(issues, 'error', 'manifest.hooks.filter', `Unknown filter hook "${f}"`, 'plugin.json')
    }
  }
  for (const e of manifest.hooks?.event ?? []) {
    if (e.startsWith('plugin:')) {
      const rest = e.slice('plugin:'.length)
      const dot = rest.indexOf('.')
      if (dot <= 0 || dot >= rest.length - 1) {
        pushIssue(issues, 'error', 'manifest.hooks.event', `Invalid plugin event "${e}" (expected plugin:<id>.<event>)`, 'plugin.json')
      }
      continue
    }
    if (!VALID_EVENT_HOOKS.has(e)) {
      pushIssue(issues, 'error', 'manifest.hooks.event', `Unknown event hook "${e}"`, 'plugin.json')
    }
  }

  for (const slot of manifest.slots ?? []) {
    if (!VALID_SLOTS.has(slot)) {
      pushIssue(issues, 'error', 'manifest.slots', `Unknown UI slot "${slot}"`, 'plugin.json')
    }
  }

  const VALID_FORM_FIELD_TYPES = new Set(['text', 'number', 'boolean', 'select', 'textarea'])
  for (const [scope, fields] of Object.entries(manifest.forms ?? {})) {
    if (!VALID_FORM_SCOPES.has(scope)) {
      pushIssue(issues, 'error', 'manifest.forms', `Unknown form scope "${scope}"`, 'plugin.json')
    }
    if (Array.isArray(fields)) {
      for (const f of fields) {
        if (!f?.name || !f?.type || !resolveLocalizedText(f.label)) {
          pushIssue(issues, 'error', 'manifest.forms.field', `Form field under "${scope}" requires name, type, and label`, 'plugin.json')
        } else if (!VALID_FORM_FIELD_TYPES.has(f.type)) {
          pushIssue(issues, 'error', 'manifest.forms.fieldType', `Unknown form field type "${f.type}" under "${scope}.${f.name}"`, 'plugin.json')
        }
      }
    }
  }

  for (const menu of manifest.menus ?? []) {
    if (!VALID_MENU_AREAS.has(menu.area)) {
      pushIssue(issues, 'error', 'manifest.menus.area', `Invalid menu area "${menu.area}"`, 'plugin.json')
    }
    if (!menu.path || !menu.path.startsWith('/')) {
      pushIssue(issues, 'error', 'manifest.menus.path', `Menu path "${menu.path ?? ''}" must start with "/"`, 'plugin.json')
    }
    if (!resolveLocalizedText(menu.title)) {
      pushIssue(issues, 'error', 'manifest.menus.title', `Menu "${menu.path ?? ''}" requires a non-empty title`, 'plugin.json')
    }
    if (menu.role && !VALID_MENU_ROLES.has(menu.role)) {
      pushIssue(issues, 'error', 'manifest.menus.role', `Invalid menu role "${menu.role}"`, 'plugin.json')
    }
  }

  if (manifest.settings && typeof manifest.settings === 'object' && !Array.isArray(manifest.settings)) {
    validateSettingsMap(manifest.settings, '', issues)
  }

  for (const [coll, spec] of Object.entries(manifest.storage?.collections ?? {})) {
    if (!COLL_NAME_RE.test(coll)) {
      pushIssue(issues, 'error', 'manifest.storage.collection', `Invalid collection name "${coll}"`, 'plugin.json')
      continue
    }
    let u = 0
    let s = 0
    let n = 0
    let t = 0
    for (const [field, kind] of Object.entries(spec?.indexes ?? {})) {
      if (!INDEX_FIELD_RE.test(field)) {
        pushIssue(issues, 'error', 'manifest.storage.indexField', `Invalid index field "${field}" in "${coll}"`, 'plugin.json')
      }
      if (!VALID_INDEX_SLOTS.has(kind)) {
        pushIssue(issues, 'error', 'manifest.storage.indexType', `Invalid index type "${kind}" on "${coll}.${field}"`, 'plugin.json')
        continue
      }
      if (kind === 'string:unique') u++
      else if (kind === 'string') s++
      else if (kind === 'number') n++
      else t++
    }
    if (u > 2) pushIssue(issues, 'error', 'manifest.storage.quota', `Collection "${coll}" exceeds max 2 'string:unique' index slots`, 'plugin.json')
    if (s > 3) pushIssue(issues, 'error', 'manifest.storage.quota', `Collection "${coll}" exceeds max 3 'string' index slots`, 'plugin.json')
    if (n > 3) pushIssue(issues, 'error', 'manifest.storage.quota', `Collection "${coll}" exceeds max 3 'number' index slots`, 'plugin.json')
    if (t > 2) pushIssue(issues, 'error', 'manifest.storage.quota', `Collection "${coll}" exceeds max 2 'time' index slots`, 'plugin.json')
  }

  for (const target of manifest.theme?.overrides ?? []) {
    if (!VALID_THEME_OVERRIDES.has(target)) {
      pushIssue(issues, 'error', 'manifest.theme.overrides', `Unknown theme override target "${target}"`, 'plugin.json')
    }
  }

  for (const [depKind, depMap] of [
    ['dependencies', manifest.dependencies],
    ['optionalDependencies', manifest.optionalDependencies],
  ] as const) {
    for (const [depId, constraint] of Object.entries(depMap ?? {})) {
      if (!PLUGIN_ID_RE.test(depId)) {
        pushIssue(issues, 'error', `manifest.${depKind}`, `Invalid dependency plugin id "${depId}"`, 'plugin.json')
      } else if (depId === manifest.id) {
        pushIssue(issues, 'error', `manifest.${depKind}`, `Plugin "${manifest.id}" cannot depend on itself`, 'plugin.json')
      }
      if (typeof constraint !== 'string' || !isValidSemverConstraint(constraint)) {
        pushIssue(issues, 'error', `manifest.${depKind}`, `Invalid version constraint "${String(constraint)}" for "${depId}"`, 'plugin.json')
      }
    }
  }

  for (const svc of manifest.services ?? []) {
    if (typeof svc !== 'string' || !svc.trim() || /[.\s]/.test(svc)) {
      pushIssue(issues, 'error', 'manifest.services', `Invalid service name "${String(svc)}"`, 'plugin.json')
    }
  }

  if (manifest.engines?.skinloom && !isValidSemverConstraint(manifest.engines.skinloom)) {
    pushIssue(issues, 'error', 'manifest.engines', `Invalid engines.skinloom constraint "${manifest.engines.skinloom}"`, 'plugin.json')
  }
  if (manifest.engines?.go && !isValidSemverConstraint(manifest.engines.go)) {
    pushIssue(issues, 'error', 'manifest.engines', `Invalid engines.go constraint "${manifest.engines.go}"`, 'plugin.json')
  }
}

function stripCommentsForLint(code: string): string {
  return code.replace(/\/\*[\s\S]*?\*\/|(?<!:)\/\/.*$/gm, '')
}

function scanSourceFiles(projectDir: string, manifest: ManifestJson, issues: LintIssue[]): void {
  const srcDir = path.join(projectDir, 'src')
  if (!existsSync(srcDir)) return

  const declaredPerms = new Set(manifest.permissions ?? [])
  const declaredGuards = new Set(manifest.hooks?.guard ?? [])
  const declaredFilters = new Set(manifest.hooks?.filter ?? [])
  const declaredEvents = new Set(manifest.hooks?.event ?? [])
  const declaredDeps = new Set([
    ...Object.keys(manifest.dependencies ?? {}),
    ...Object.keys(manifest.optionalDependencies ?? {}),
  ])

  const files = walkFiles(srcDir).filter((f) => /\.(?:[cm]?[jt]sx?)$/i.test(f) && !TEST_FILE_RE.test(f))
  for (const rel of files) {
    const fileLabel = `src/${rel}`
    const isWeb = rel === 'web.ts' || rel === 'web.tsx' || rel.startsWith('web/')
    const rawContent = readFileSync(path.join(srcDir, ...rel.split('/')), 'utf8')
    const content = stripCommentsForLint(rawContent)

    // Disallow Node/Bun built-ins, dynamic eval, and browser UI imports in server code.
    const importRe = /(?:from\s+|import\s+(?!type\b)(?:[^'"\n]*?\s+from\s+)?|import\s*\(\s*|require\s*\(\s*)['"]([^'"]+)['"]/g
    let match: RegExpExecArray | null
    while ((match = importRe.exec(content)) !== null) {
      const mod = match[1]
      if (mod.startsWith('node:') || mod.startsWith('bun:') || NODE_BUILTIN_MODULES.has(mod)) {
        pushIssue(issues, 'error', 'source.forbiddenImport', `Forbidden Node/Bun built-in import "${mod}"`, fileLabel)
      }
      if (
        !isWeb &&
        (mod === 'react' ||
          mod.startsWith('react/') ||
          mod === 'react-dom' ||
          mod.startsWith('react-dom/') ||
          mod === '@tanstack/react-query' ||
          mod.startsWith('@tanstack/react-query/') ||
          mod === '@skinloom/ui' ||
          mod.startsWith('@skinloom/ui/') ||
          mod === '@skinloom/plugin-sdk' ||
          mod === '@skinloom/plugin-sdk/web' ||
          mod.startsWith('@skinloom/plugin-sdk/web/'))
      ) {
        pushIssue(issues, 'error', 'source.forbiddenImport', `Browser/SDK barrel module "${mod}" cannot be used in server code`, fileLabel)
      }
    }
    if (/\b(?:eval|Function)\s*\(/.test(content)) {
      pushIssue(issues, 'warning', 'source.dynamicEval', 'Dynamic eval()/Function() may fail CSP or sandbox policy', fileLabel)
    }

    if (isWeb) continue

    for (const rule of CTX_PERMISSION_RULES) {
      rule.pattern.lastIndex = 0
      if (rule.pattern.test(content) && !declaredPerms.has(rule.permission)) {
        pushIssue(
          issues,
          'error',
          'source.missingPermission',
          `${rule.description} used in ${fileLabel}, but "${rule.permission}" is missing from plugin.json permissions`,
          fileLabel,
        )
      }
    }

    const guardRe = /\bctx\.hooks\.guard\s*\(\s*['"]([^'"]+)['"]/g
    while ((match = guardRe.exec(content)) !== null) {
      if (!declaredGuards.has(match[1])) {
        pushIssue(
          issues,
          'error',
          'source.undeclaredHook',
          `Guard hook "${match[1]}" must be declared in plugin.json hooks.guard`,
          fileLabel,
        )
      }
    }

    const filterRe = /\bctx\.hooks\.filter\s*\(\s*['"]([^'"]+)['"]/g
    while ((match = filterRe.exec(content)) !== null) {
      if (!declaredFilters.has(match[1])) {
        pushIssue(
          issues,
          'error',
          'source.undeclaredHook',
          `Filter hook "${match[1]}" must be declared in plugin.json hooks.filter`,
          fileLabel,
        )
      }
    }

    const eventRe = /\bctx\.events\.on\s*\(\s*['"]([^'"]+)['"]/g
    while ((match = eventRe.exec(content)) !== null) {
      if (!declaredEvents.has(match[1])) {
        pushIssue(
          issues,
          'error',
          'source.undeclaredHook',
          `Event hook "${match[1]}" must be declared in plugin.json hooks.event`,
          fileLabel,
        )
      }
    }

    const serviceUseRe = /\bctx\.services\.use\s*\(\s*['"]([^'"]+)['"]/g
    while ((match = serviceUseRe.exec(content)) !== null) {
      const target = match[1].split('.')[0] ?? match[1]
      if (target && !declaredDeps.has(target)) {
        pushIssue(
          issues,
          'error',
          'source.undeclaredDependency',
          `Service "${match[1]}" requires "${target}" in dependencies or optionalDependencies`,
          fileLabel,
        )
      }
    }

    const declaredServices = new Set(manifest.services ?? [])
    const serviceExposeRe = /\bctx\.services\.expose\s*\(\s*['"]([^'"]+)['"]/g
    while ((match = serviceExposeRe.exec(content)) !== null) {
      if (!declaredServices.has(match[1])) {
        pushIssue(
          issues,
          'error',
          'source.undeclaredService',
          `Service "${match[1]}" exposed in ${fileLabel} must be declared in plugin.json services`,
          fileLabel,
        )
      }
    }
  }
}

function checkBuiltArtifactSizes(dir: string, manifest: ManifestJson, issues: LintIssue[], outDir = 'dist'): void {
  const candidateOut = path.resolve(dir, outDir)
  const inspectDir = existsSync(path.join(candidateOut, 'plugin.json')) ? candidateOut : dir
  const files = walkFiles(inspectDir, new Set(['node_modules', '.git', 'src']))
  const isNative = effectiveRuntime(manifest) === 'native'
  const cleanServer = manifest.server ? path.posix.normalize(manifest.server.replace(/^\.\//, '')) : 'server.js'
  let totalBytes = 0
  let binaryCount = 0
  for (const rel of files) {
    if (rel.endsWith('.slp')) continue
    const abs = path.join(inspectDir, ...rel.split('/'))
    const size = statSync(abs).size
    totalBytes += size
    const isNativeBin = isNative && isNativeExecutablePath(manifest, rel)
    if (isNativeBin) {
      binaryCount++
      if (size > MAX_NATIVE_BINARY_BYTES) {
        pushIssue(issues, 'error', 'bundle.binarySize', `${rel} (${size} bytes) exceeds 64 MiB native binary cap`, rel)
      }
      continue
    }
    if ((rel === 'server.js' || rel === cleanServer) && size > MAX_SERVER_BUNDLE_BYTES) {
      pushIssue(issues, 'error', 'bundle.serverSize', `${rel} (${size} bytes) exceeds 5 MiB cap`, rel)
    }
    if (size > MAX_ASSET_FILE_BYTES) {
      pushIssue(issues, 'error', 'bundle.fileSize', `${rel} (${size} bytes) exceeds 10 MiB per-file cap`, rel)
    }
  }
  const totalCap = isNative
    ? binaryCount > 1 || (manifest.native?.platforms?.length ?? 0) > 1
      ? MAX_NATIVE_UNPACKED_FAT_BYTES
      : MAX_NATIVE_UNPACKED_SINGLE_BYTES
    : MAX_PACKAGE_TOTAL_BYTES
  if (totalBytes > totalCap) {
    pushIssue(
      issues,
      'error',
      'bundle.totalSize',
      `Total package size (${totalBytes} bytes) exceeds ${Math.round(totalCap / (1024 * 1024))} MiB cap`,
    )
  }
}

/**
 * Validates `plugin.json`, static `src/` permission/hook/dependency consistency,
 * and built bundle size limits for a plugin or theme directory.
 */
export function lintPlugin(dir = '.', outDir = 'dist'): LintResult {
  const projectDir = path.resolve(dir)
  const issues: LintIssue[] = []

  let manifest: ManifestJson
  try {
    manifest = loadManifest(projectDir).manifest
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    pushIssue(issues, 'error', 'manifest.load', message, 'plugin.json')
    return { ok: false, errors: issues, warnings: [] }
  }

  lintManifest(manifest, issues)
  scanSourceFiles(projectDir, manifest, issues)
  checkBuiltArtifactSizes(projectDir, manifest, issues, outDir)

  const errors = issues.filter((i) => i.severity === 'error')
  const warnings = issues.filter((i) => i.severity === 'warning')
  return {
    ok: errors.length === 0,
    errors,
    warnings,
  }
}
