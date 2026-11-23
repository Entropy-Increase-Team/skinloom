import { createHash, createHmac, randomBytes, randomUUID, timingSafeEqual as nodeTimingSafeEqual } from 'node:crypto'

import type {
  HookEventName,
  HookEventPayloads,
  HookFilterDefinitions,
  HookFilterName,
  HookGuardName,
  HookGuardPayloads,
  PluginPermission,
} from '../gen/catalog.js'
import type {
  ExternalUserInfo,
  GuardRejection,
  PluginClosetItemDTO,
  PluginContext,
  PluginDefinition,
  PluginFetchInit,
  PluginFetchResponse,
  PluginHttpRequest,
  PluginHttpResponse,
  PluginPlayerDTO,
  PluginRouteHandler,
  PluginRouteOptions,
  PluginTextureDTO,
  PluginUserDTO,
} from '../server/index.js'

export interface TestManifest {
  id: string
  kind?: 'plugin' | 'theme'
  version?: string
  permissions?: PluginPermission[]
  http?: { allow?: string[] }
  hooks?: {
    guard?: string[]
    filter?: string[]
    event?: string[]
  }
  storage?: {
    collections?: Record<string, { indexes?: Record<string, 'string' | 'string:unique' | 'number' | 'time'> }>
  }
  dependencies?: Record<string, string>
  optionalDependencies?: Record<string, string>
  services?: string[]
}

export interface TestHostFixtures {
  users?: Array<Partial<PluginUserDTO> & { id: number; email: string }>
  players?: Array<Partial<PluginPlayerDTO> & { id: number; userId: number; name: string }>
  textures?: Array<Partial<PluginTextureDTO> & { id: number; name: string }>
  bytes?: Record<number, Uint8Array>
}

export interface CreateTestHostOptions {
  manifest?: Partial<TestManifest>
  settings?: Record<string, unknown>
  locales?: Record<string, Record<string, string>>
  site?: { name?: string; url?: string; description?: string; locales?: string[] }
  fixtures?: TestHostFixtures
  /** When true (default), strictly checks declared manifest permissions & hooks. */
  enforcePermissions?: boolean
}

export interface FetchMockResponse {
  status?: number
  statusText?: string
  headers?: Record<string, string>
  body?: string | Uint8Array
  json?: unknown
}

export class TestFetchMock {
  readonly calls: Array<{ url: string; init?: PluginFetchInit }> = []
  private routes: Array<{ match: string | RegExp; resp: FetchMockResponse | ((url: string, init?: PluginFetchInit) => FetchMockResponse | Promise<FetchMockResponse>) }> = []

  mock(
    match: string | RegExp,
    resp: FetchMockResponse | ((url: string, init?: PluginFetchInit) => FetchMockResponse | Promise<FetchMockResponse>),
  ): this {
    this.routes.push({ match, resp })
    return this
  }

  mockJson(match: string | RegExp, json: unknown, status = 200): this {
    return this.mock(match, { status, statusText: status === 200 ? 'OK' : '', headers: { 'content-type': 'application/json' }, json })
  }

  reset(): void {
    this.calls.length = 0
    this.routes.length = 0
  }

  async resolve(url: string, init?: PluginFetchInit): Promise<PluginFetchResponse> {
    this.calls.push({ url, init })
    for (let i = this.routes.length - 1; i >= 0; i--) {
      const rule = this.routes[i]
      const hit = typeof rule.match === 'string' ? url === rule.match || url.startsWith(rule.match) : rule.match.test(url)
      if (!hit) continue
      const r = typeof rule.resp === 'function' ? await rule.resp(url, init) : rule.resp
      const status = r.status ?? 200
      const ok = status >= 200 && status < 300
      let rawBytes: Uint8Array
      let bodyText: string
      if (r.json !== undefined) {
        bodyText = JSON.stringify(r.json)
        rawBytes = new TextEncoder().encode(bodyText)
      } else if (r.body instanceof Uint8Array) {
        rawBytes = r.body
        bodyText = new TextDecoder().decode(r.body)
      } else {
        bodyText = r.body ?? ''
        rawBytes = new TextEncoder().encode(bodyText)
      }
      return {
        ok,
        status,
        statusText: r.statusText ?? (ok ? 'OK' : 'Error'),
        headers: r.headers ?? {},
        url,
        async text() {
          return bodyText
        },
        async json<T = unknown>() {
          return JSON.parse(bodyText) as T
        },
        async arrayBuffer() {
          return rawBytes.slice().buffer
        },
      }
    }
    throw new Error(`testHost.fetchMock: unexpected unmocked fetch to ${url}`)
  }
}

export type TestGuardResult = { ok: true } | { ok: false; rejected: true; reason: string }

function matchRoutePattern(pattern: string, actual: string): Record<string, string> | null {
  const pSegs = pattern.replace(/^\/+|\/+$/g, '').split('/').filter(Boolean)
  const aSegs = actual.replace(/^\/+|\/+$/g, '').split('/').filter(Boolean)
  if (pSegs.length === 1 && pSegs[0] === '*') return { '*': aSegs.join('/') }
  const params: Record<string, string> = {}
  for (let i = 0; i < pSegs.length; i++) {
    const seg = pSegs[i]
    if (seg === '*' && i === pSegs.length - 1) {
      params['*'] = aSegs.slice(i).join('/')
      return params
    }
    if (i >= aSegs.length) return null
    if (seg.startsWith(':')) params[seg.slice(1)] = aSegs[i]
    else if (seg.startsWith('{') && seg.endsWith('}') && seg.length > 2) params[seg.slice(1, -1)] = aSegs[i]
    else if (seg !== aSegs[i]) return null
  }
  if (aSegs.length !== pSegs.length) return null
  return params
}

function applyDocPatch<TDoc extends Record<string, unknown>>(
  existing: TDoc & { id: string },
  patch: Partial<TDoc> & { $set?: Record<string, unknown>; $unset?: Record<string, unknown> | string[] },
): TDoc & { id: string } {
  const next: Record<string, unknown> = structuredClone(existing)
  if (
    patch &&
    typeof patch === 'object' &&
    ('$set' in patch || '$unset' in patch)
  ) {
    if (patch.$set && typeof patch.$set === 'object') {
      for (const [k, v] of Object.entries(patch.$set)) {
        if (k !== 'id') next[k] = structuredClone(v)
      }
    }
    if (Array.isArray(patch.$unset)) {
      for (const k of patch.$unset) {
        if (k !== 'id') delete next[String(k)]
      }
    } else if (patch.$unset && typeof patch.$unset === 'object') {
      for (const k of Object.keys(patch.$unset)) {
        if (k !== 'id') delete next[k]
      }
    }
  } else {
    Object.assign(next, structuredClone(patch))
  }
  next.id = existing.id
  return next as TDoc & { id: string }
}

function isRejection(v: unknown): v is GuardRejection {
  return typeof v === 'object' && v !== null && (v as { __skinloomReject?: boolean }).__skinloomReject === true
}

function normalizeComparable(val: unknown): number | string | null {
  if (val instanceof Date) return val.getTime()
  if (typeof val === 'number' && !Number.isNaN(val)) return val
  if (typeof val === 'string') return val
  return null
}

function compareOrdered(actual: unknown, cond: unknown, op: '$gt' | '$gte' | '$lt' | '$lte'): boolean {
  const a = normalizeComparable(actual)
  const b = normalizeComparable(cond)
  if (a === null || b === null) return false
  if (typeof a !== typeof b) {
    // Allow Date <-> ISO string comparisons when querying time fields.
    const ta = actual instanceof Date ? actual.getTime() : typeof actual === 'string' ? Date.parse(actual) : Number.NaN
    const tb = cond instanceof Date ? cond.getTime() : typeof cond === 'string' ? Date.parse(cond) : Number.NaN
    if (Number.isNaN(ta) || Number.isNaN(tb)) return false
    return op === '$gt' ? ta > tb : op === '$gte' ? ta >= tb : op === '$lt' ? ta < tb : ta <= tb
  }
  return op === '$gt' ? a > b : op === '$gte' ? a >= b : op === '$lt' ? a < b : a <= b
}

function matchesDocFilter(
  doc: Record<string, unknown>,
  filter: Record<string, unknown>,
  indexed: Record<string, 'string' | 'string:unique' | 'number' | 'time'>,
): boolean {
  for (const [field, cond] of Object.entries(filter)) {
    if (field !== 'id' && indexed && Object.keys(indexed).length > 0 && !(field in indexed)) {
      throw new Error(`unindexed field "${field}" in filter; declare it under storage.collections.<name>.indexes`)
    }
    const actual = doc[field]
    if (cond === null || cond === undefined) {
      if (actual !== null && actual !== undefined) return false
      continue
    }
    if (typeof cond === 'object' && !Array.isArray(cond) && !(cond instanceof Date)) {
      const ops = cond as Record<string, unknown>
      const lhs = actual instanceof Date ? actual.toISOString() : actual
      if ('$eq' in ops) {
        const eqVal = ops.$eq instanceof Date ? ops.$eq.toISOString() : ops.$eq
        if (eqVal === null || eqVal === undefined) {
          if (actual !== null && actual !== undefined) return false
        } else if (lhs !== eqVal) {
          return false
        }
      }
      if ('$ne' in ops) {
        const neVal = ops.$ne instanceof Date ? ops.$ne.toISOString() : ops.$ne
        if (neVal === null || neVal === undefined) {
          if (actual === null || actual === undefined) return false
        } else if (lhs === neVal) {
          return false
        }
      }
      if ('$in' in ops && Array.isArray(ops.$in)) {
        const normIn = ops.$in.map((v) => (v instanceof Date ? v.toISOString() : v))
        if (!normIn.includes(lhs)) return false
      }
      if ('$prefix' in ops && (typeof actual !== 'string' || !actual.startsWith(String(ops.$prefix)))) return false
      if ('$gt' in ops && !compareOrdered(actual, ops.$gt, '$gt')) return false
      if ('$gte' in ops && !compareOrdered(actual, ops.$gte, '$gte')) return false
      if ('$lt' in ops && !compareOrdered(actual, ops.$lt, '$lt')) return false
      if ('$lte' in ops && !compareOrdered(actual, ops.$lte, '$lte')) return false
      continue
    }
    const lhs = actual instanceof Date ? actual.toISOString() : actual
    const rhs = cond instanceof Date ? cond.toISOString() : cond
    if (lhs !== rhs) return false
  }
  return true
}

/**
 * Creates a full in-memory host simulator implementing all 25 ctx namespaces so
 * backend plugin tests run fast in Node / Vitest without needing Wazero or SQL.
 */
export function createTestHost(options: CreateTestHostOptions = {}) {
  const enforce = options.enforcePermissions !== false
  const manifest: TestManifest = {
    id: options.manifest?.id ?? 'test-plugin',
    version: options.manifest?.version ?? '1.0.0',
    permissions: options.manifest?.permissions ?? [],
    http: options.manifest?.http ?? {},
    hooks: options.manifest?.hooks ?? {},
    storage: options.manifest?.storage ?? {},
    dependencies: options.manifest?.dependencies ?? {},
    optionalDependencies: options.manifest?.optionalDependencies ?? {},
    services: options.manifest?.services ?? [],
  }

  const permSet = new Set<string>(manifest.permissions ?? [])
  const checkPerm = (perm: PluginPermission | string) => {
    if (enforce && !permSet.has(perm)) {
      throw new Error(`permission denied: ${perm}`)
    }
  }
  const checkHook = (kind: 'guard' | 'filter' | 'event', name: string) => {
    if (!enforce) return
    const list = manifest.hooks?.[kind] ?? []
    if (!list.includes(name)) {
      throw new Error(`permission denied: hook "${name}" (${kind}) is not declared in manifest`)
    }
  }

  // Mutable stores & captured telemetry
  const kvStore = new Map<string, { value: unknown; expiresAt?: number }>()
  const docCollections = new Map<string, Array<Record<string, unknown> & { id: string }>>()
  const filesStore = new Map<string, Uint8Array>()
  const cacheStore = new Map<string, { value: unknown; expiresAt?: number }>()
  const settingsState: Record<string, unknown> = structuredClone(options.settings ?? {})
  const localesMap = options.locales ?? {}
  const settingsListeners: Array<(next: Record<string, unknown>) => Promise<void> | void> = []

  const users = new Map<number, PluginUserDTO>()
  for (const u of options.fixtures?.users ?? []) {
    users.set(u.id, {
      id: u.id,
      email: u.email,
      nickname: u.nickname ?? `User${u.id}`,
      role: u.role ?? 'user',
      status: u.status ?? 'active',
      score: u.score ?? 0,
      locale: u.locale ?? 'en',
      emailVerified: u.emailVerified ?? true,
      avatarTextureId: u.avatarTextureId ?? null,
      createdAt: u.createdAt ?? new Date().toISOString(),
      lastLoginAt: u.lastLoginAt ?? null,
    })
  }
  const players = new Map<number, PluginPlayerDTO>()
  let nextPlayerId = 1
  for (const p of options.fixtures?.players ?? []) {
    nextPlayerId = Math.max(nextPlayerId, p.id + 1)
    players.set(p.id, {
      id: p.id,
      userId: p.userId,
      name: p.name,
      uuid: p.uuid ?? randomUUID().replaceAll('-', ''),
      skinId: p.skinId ?? null,
      capeId: p.capeId ?? null,
      createdAt: p.createdAt ?? new Date().toISOString(),
      updatedAt: p.updatedAt ?? new Date().toISOString(),
    })
  }
  const textures = new Map<number, PluginTextureDTO>()
  const textureBytes = new Map<number, Uint8Array>()
  let nextTextureId = 1
  for (const t of options.fixtures?.textures ?? []) {
    nextTextureId = Math.max(nextTextureId, t.id + 1)
    textures.set(t.id, {
      id: t.id,
      name: t.name,
      type: t.type ?? 'steve',
      hash: t.hash ?? createHash('sha256').update(String(t.id)).digest('hex'),
      size: t.size ?? 128,
      visibility: t.visibility ?? (t.public === false ? 'private' : 'public'),
      public: t.public ?? true,
      likes: t.likes ?? 0,
      uploaderId: t.uploaderId ?? 1,
      createdAt: t.createdAt ?? new Date().toISOString(),
      updatedAt: t.updatedAt ?? new Date().toISOString(),
    })
  }
  for (const [id, b] of Object.entries(options.fixtures?.bytes ?? {})) {
    textureBytes.set(Number(id), b)
  }
  const closet = new Map<number, PluginClosetItemDTO[]>()
  let nextClosetId = 1

  const guards: Array<{ name: string; order: number; fn: (p: unknown) => Promise<unknown> | unknown }> = []
  const filters: Array<{ name: string; order: number; fn: (v: unknown, ctx: unknown) => Promise<unknown> | unknown }> = []
  const events: Array<{ name: string; order: number; fn: (p: unknown) => Promise<void> | void }> = []
  const routes: Array<{ method: string; pattern: string; auth: string; handler: PluginRouteHandler }> = []
  const cronJobs = new Map<string, () => Promise<void> | void>()
  const exposedServices = new Map<string, (...args: unknown[]) => unknown>()
  const remoteServices = new Map<string, (...args: unknown[]) => unknown>()

  const emittedEvents: Array<{ name: string; payload: unknown }> = []
  const notifications: Array<{ kind: 'send' | 'announce'; target: number[] | string; title: string; markdown: string }> = []
  const sentMails: Array<Record<string, unknown>> = []
  const auditLogs: Array<{ action: string; targetType: string; targetId: string; meta: Record<string, unknown> | null }> = []
  const logs: Array<{ level: string; message: string }> = []
  const authProviders = new Map<string, { def: unknown; verify?: (input: Record<string, unknown>) => Promise<ExternalUserInfo> | ExternalUserInfo }>()
  const passwordVerifiers = new Map<string, (stored: string, pw: string) => Promise<boolean> | boolean>()
  const captchaProviders = new Map<string, unknown>()
  const cdnPurgers = new Map<string, unknown>()
  const fetchMock = new TestFetchMock()

  const translate = (key: string, params?: Record<string, unknown> | null, locale = 'en') => {
    const table = localesMap[locale] ?? localesMap.en ?? localesMap['zh-CN'] ?? {}
    let val = Object.hasOwn(table, key) ? table[key] : key
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        val = val.replaceAll(`{{${k}}}`, String(v)).replaceAll(`{${k}}`, String(v))
      }
    }
    return val
  }

  let docSeq = 1
  const addRoute =
    (method: string) =>
    (path: string, optsOrHandler: PluginRouteOptions | PluginRouteHandler, maybeHandler?: PluginRouteHandler) => {
      const opts = typeof optsOrHandler === 'function' ? {} : optsOrHandler
      const handler = typeof optsOrHandler === 'function' ? optsOrHandler : maybeHandler!
      routes.push({
        method: method.toUpperCase(),
        pattern: path.startsWith('/') ? path : `/${path}`,
        auth: opts.auth ?? 'public',
        handler,
      })
    }

  const ctx: PluginContext = {
    hooks: {
      reject(reason: string): GuardRejection {
        return { __skinloomReject: true, reason: String(reason ?? 'rejected') }
      },
      guard(name, fn, order = 0) {
        checkHook('guard', name)
        guards.push({ name, order, fn: fn as (p: unknown) => unknown })
      },
      filter(name, fn, order = 0) {
        checkHook('filter', name)
        filters.push({ name, order, fn: fn as (v: unknown, c: unknown) => unknown })
      },
    },
    events: {
      on(name: string, fn: (p: unknown) => Promise<void> | void, order = 0) {
        checkHook('event', name)
        events.push({ name, order, fn })
      },
      async emit(localName: string, payload?: unknown) {
        emittedEvents.push({ name: `plugin:${manifest.id}.${localName}`, payload })
      },
    },
    http: {
      get: addRoute('GET'),
      post: addRoute('POST'),
      put: addRoute('PUT'),
      patch: addRoute('PATCH'),
      delete: addRoute('DELETE'),
      route(method, path, optsOrHandler, maybeHandler) {
        addRoute(method)(path, optsOrHandler, maybeHandler)
      },
    },
    kv: {
      async get<T = unknown>(key: string) {
        checkPerm('storage.kv')
        const entry = kvStore.get(key)
        if (!entry) return null
        if (entry.expiresAt && Date.now() >= entry.expiresAt) {
          kvStore.delete(key)
          return null
        }
        return structuredClone(entry.value) as T
      },
      async set(key, value, ttlSeconds = 0) {
        checkPerm('storage.kv')
        kvStore.set(key, {
          value: structuredClone(value),
          expiresAt: ttlSeconds > 0 ? Date.now() + ttlSeconds * 1000 : undefined,
        })
      },
      async delete(key) {
        checkPerm('storage.kv')
        return kvStore.delete(key)
      },
      async list<T = unknown>(prefix = '', limit = 100) {
        checkPerm('storage.kv')
        const out: Array<{ key: string; value: T }> = []
        for (const [k, ent] of [...kvStore.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
          if (ent.expiresAt && Date.now() >= ent.expiresAt) continue
          if (prefix && !k.startsWith(prefix)) continue
          out.push({ key: k, value: structuredClone(ent.value) as T })
          if (out.length >= limit) break
        }
        return out
      },
      async incr(key, delta = 1) {
        checkPerm('storage.kv')
        const existing = kvStore.get(key)
        const expired = Boolean(existing?.expiresAt && Date.now() >= existing.expiresAt)
        const cur = !existing || expired ? 0 : Number(existing.value ?? 0)
        const next = cur + delta
        kvStore.set(key, {
          value: next,
          expiresAt: !existing || expired ? undefined : existing.expiresAt,
        })
        return next
      },
    },
    db: {
      collection<TDoc extends Record<string, unknown> = Record<string, unknown>>(name: string) {
        const colSpec = manifest.storage?.collections?.[name]
        const indexed = colSpec?.indexes ?? {}
        const getRows = () => {
          checkPerm('storage.docs')
          if (enforce && manifest.storage?.collections && !(name in manifest.storage.collections)) {
            throw new Error(`unknown plugin collection: ${name}`)
          }
          let arr = docCollections.get(name)
          if (!arr) {
            arr = []
            docCollections.set(name, arr)
          }
          return arr
        }
        const assertUnique = (rows: Array<Record<string, unknown> & { id: string }>, candidate: Record<string, unknown>, skipId?: string) => {
          for (const [field, kind] of Object.entries(indexed)) {
            if (kind !== 'string:unique') continue
            const val = candidate[field]
            if (val === null || val === undefined) continue
            if (rows.some((r) => r.id !== skipId && r[field] === val)) {
              throw new Error(`unique constraint violation on ${name}.${field}`)
            }
          }
        }
        return {
          async insertOne(doc: TDoc) {
            const rows = getRows()
            const id = String((doc as Record<string, unknown>).id ?? `doc_${docSeq++}`)
            if (rows.some((r) => r.id === id)) {
              throw new Error(`duplicate document id "${id}" in ${name}`)
            }
            const saved = structuredClone({ ...doc, id }) as TDoc & { id: string }
            assertUnique(rows, saved)
            rows.push(saved)
            return structuredClone(saved)
          },
          async findOne(filter: Record<string, unknown> = {}) {
            const rows = getRows()
            const hit = rows.find((r) => matchesDocFilter(r, filter, indexed))
            return hit ? (structuredClone(hit) as TDoc & { id: string }) : null
          },
          async find(filter: Record<string, unknown> = {}, opts: { sort?: Record<string, 1 | -1>; limit?: number; cursor?: string } = {}) {
            let matched = getRows().filter((r) => matchesDocFilter(r, filter, indexed))
            if (opts.sort) {
              for (const key of Object.keys(opts.sort)) {
                if (key !== 'id' && Object.keys(indexed).length > 0 && !(key in indexed)) {
                  throw new Error(`unindexed sort field "${key}" in collection "${name}"`)
                }
              }
              matched = [...matched].sort((a, b) => {
                for (const [k, dir] of Object.entries(opts.sort ?? {})) {
                  const av = a[k] as string | number
                  const bv = b[k] as string | number
                  if (av === bv) continue
                  return av < bv ? -dir : dir
                }
                return 0
              })
            }
            const start = opts.cursor ? Number(opts.cursor) || 0 : 0
            const limit = opts.limit && opts.limit > 0 ? opts.limit : 50
            const page = matched.slice(start, start + limit).map((r) => structuredClone(r) as TDoc & { id: string })
            const nextCursor = start + limit < matched.length ? String(start + limit) : undefined
            return { items: page, nextCursor }
          },
          async updateOne(filter: Record<string, unknown>, patch: Partial<TDoc>) {
            const rows = getRows()
            const idx = rows.findIndex((r) => matchesDocFilter(r, filter, indexed))
            if (idx < 0) return { matched: false, modified: false }
            const next = applyDocPatch(rows[idx], patch)
            assertUnique(rows, next, rows[idx].id)
            rows[idx] = next
            return { matched: true, modified: true }
          },
          async updateMany(filter: Record<string, unknown>, patch: Partial<TDoc>) {
            const rows = getRows()
            const draft = [...rows]
            let count = 0
            for (let i = 0; i < draft.length; i++) {
              if (!matchesDocFilter(rows[i], filter, indexed)) continue
              const next = applyDocPatch(draft[i], patch)
              assertUnique(draft, next, draft[i].id)
              draft[i] = next
              count++
            }
            for (let i = 0; i < rows.length; i++) {
              rows[i] = draft[i]
            }
            return { matched: count, modified: count }
          },
          async deleteOne(filter: Record<string, unknown>) {
            const rows = getRows()
            const idx = rows.findIndex((r) => matchesDocFilter(r, filter, indexed))
            if (idx < 0) return { deleted: false }
            rows.splice(idx, 1)
            return { deleted: true }
          },
          async count(filter: Record<string, unknown> = {}) {
            return getRows().filter((r) => matchesDocFilter(r, filter, indexed)).length
          },
        }
      },
    },
    files: {
      async put(key, data) {
        checkPerm('storage.files')
        if (!key || key.startsWith('/') || key.includes('..') || key.includes('\\')) {
          throw new Error(`invalid plugin file key: ${key}`)
        }
        const buf = typeof data === 'string' ? new TextEncoder().encode(data) : new Uint8Array(data)
        filesStore.set(key, buf)
        return `/plugins-assets/${manifest.id}/_files/${key}`
      },
      async get(key) {
        checkPerm('storage.files')
        if (!key || key.startsWith('/') || key.includes('..') || key.includes('\\')) {
          throw new Error(`invalid plugin file key: ${key}`)
        }
        const val = filesStore.get(key)
        return val ? val.slice() : null
      },
      async delete(key) {
        checkPerm('storage.files')
        return filesStore.delete(key)
      },
      async url(key) {
        checkPerm('storage.files')
        return `/plugins-assets/${manifest.id}/_files/${key}`
      },
    },
    async fetch(url, init) {
      checkPerm('http.fetch')
      const u = new URL(url)
      if (u.protocol !== 'http:' && u.protocol !== 'https:') {
        throw new Error(`outbound fetch denied: disallowed scheme ${u.protocol}`)
      }
      const allow = manifest.http?.allow ?? []
      const host = u.hostname.replace(/^\[|\]$/g, '').toLowerCase().replace(/\.+$/, '')
      if (host === 'localhost' || host === '127.0.0.1' || host === '169.254.169.254' || host === '::1') {
        throw new Error(`outbound fetch denied: private host ${host}`)
      }
      if (enforce) {
        const ok = allow.some((pat) => {
          const p = pat.toLowerCase()
          return p.startsWith('*.') ? host.endsWith(p.slice(1)) : host === p
        })
        if (!ok) {
          throw new Error(`outbound fetch denied: host ${host} not in http.allow`)
        }
      }
      return fetchMock.resolve(url, init)
    },
    settings: {
      async get<T = unknown>(key: string) {
        return structuredClone(settingsState[key]) as T
      },
      async all<T extends Record<string, unknown>>() {
        return structuredClone(settingsState) as T
      },
      async patch<T extends Record<string, unknown>>(patch: Record<string, unknown>) {
        if (!patch || typeof patch !== 'object' || Array.isArray(patch)) {
          throw new Error('settings.patch expects a JSON object')
        }
        Object.assign(settingsState, structuredClone(patch))
        return structuredClone(settingsState) as T
      },
      onChange(fn) {
        settingsListeners.push(fn)
      },
    },
    site: {
      async name() {
        return options.site?.name ?? 'SkinLoom'
      },
      async url() {
        return options.site?.url ?? 'https://skinloom.example'
      },
      async description() {
        return options.site?.description ?? ''
      },
      async locales() {
        return options.site?.locales ?? ['zh-CN', 'en']
      },
      async publicSettings() {
        return {}
      },
    },
    users: {
      async get(id) {
        checkPerm('users.read')
        return users.get(id) ?? null
      },
      async find(email) {
        checkPerm('users.read')
        const norm = email.trim().toLowerCase()
        return [...users.values()].find((u) => u.email.toLowerCase() === norm) ?? null
      },
      async list(opts = {}) {
        checkPerm('users.read')
        let items = [...users.values()]
        if (opts.q) {
          const q = opts.q.toLowerCase()
          items = items.filter((u) => u.email.toLowerCase().includes(q) || u.nickname.toLowerCase().includes(q))
        }
        return { items: items.slice(0, opts.limit ?? 50), nextBefore: 0 }
      },
      async update(id, patch) {
        checkPerm('users.write')
        const u = users.get(id)
        if (!u) throw new Error(`user ${id} not found`)
        if (u.role === 'admin' || u.role === 'superadmin') {
          throw new Error('cannot modify admin user')
        }
        const updated: PluginUserDTO = {
          ...u,
          nickname: patch.nickname ?? u.nickname,
          locale: patch.locale ?? u.locale,
          emailVerified: patch.emailVerified ?? patch.verified ?? u.emailVerified,
        }
        users.set(id, updated)
        return updated
      },
      async ban(id, banned = true) {
        checkPerm('users.ban')
        const u = users.get(id)
        if (!u) throw new Error(`user ${id} not found`)
        if (u.role === 'admin' || u.role === 'superadmin') {
          throw new Error('cannot ban administrator')
        }
        if (u.status === 'deleted') {
          throw new Error('cannot modify deleted user')
        }
        const updated: PluginUserDTO = { ...u, status: banned ? 'banned' : 'active' }
        users.set(id, updated)
        return updated
      },
    },
    players: {
      async get(id) {
        checkPerm('players.read')
        return players.get(id) ?? null
      },
      async findByName(name) {
        checkPerm('players.read')
        const low = name.toLowerCase()
        return [...players.values()].find((p) => p.name.toLowerCase() === low) ?? null
      },
      async list(userId) {
        checkPerm('players.read')
        return [...players.values()].filter((p) => p.userId === userId)
      },
      async create(userId, name) {
        checkPerm('players.write')
        const id = nextPlayerId++
        const p: PluginPlayerDTO = {
          id,
          userId,
          name,
          uuid: randomUUID().replaceAll('-', ''),
          skinId: null,
          capeId: null,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        }
        players.set(id, p)
        return { ...p, balance: users.get(userId)?.score ?? 0 }
      },
      async rename(_userId, playerId, name) {
        checkPerm('players.write')
        const p = players.get(playerId)
        if (!p) throw new Error(`player ${playerId} not found`)
        const updated = { ...p, name, updatedAt: new Date().toISOString() }
        players.set(playerId, updated)
        return updated
      },
      async setTextures(_userId, playerId, tex) {
        checkPerm('players.write')
        const p = players.get(playerId)
        if (!p) throw new Error(`player ${playerId} not found`)
        const updated = {
          ...p,
          skinId: tex.skin !== undefined ? tex.skin : p.skinId,
          capeId: tex.cape !== undefined ? tex.cape : p.capeId,
          updatedAt: new Date().toISOString(),
        }
        players.set(playerId, updated)
        return updated
      },
      async delete(userId, playerId) {
        checkPerm('players.write')
        players.delete(playerId)
        return { deleted: true as const, balance: users.get(userId)?.score ?? 0 }
      },
    },
    textures: {
      async get(id) {
        checkPerm('textures.read')
        return textures.get(id) ?? null
      },
      async list(opts = {}) {
        checkPerm('textures.read')
        let items = [...textures.values()]
        if (opts.uploaderId) items = items.filter((t) => t.uploaderId === opts.uploaderId)
        return { items: items.slice(0, opts.limit ?? 50), nextCursor: '' }
      },
      async create(input) {
        checkPerm('textures.write')
        const id = nextTextureId++
        const data = input.data ?? (input.dataBase64 ? Uint8Array.from(Buffer.from(input.dataBase64, 'base64')) : new Uint8Array(8))
        const tex: PluginTextureDTO = {
          id,
          name: input.name,
          type: input.type,
          hash: createHash('sha256').update(data).digest('hex'),
          size: data.length,
          visibility: input.public === false ? 'private' : 'public',
          public: input.public !== false,
          likes: 0,
          uploaderId: input.uploaderId,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        }
        textures.set(id, tex)
        textureBytes.set(id, data)
        return { texture: tex, duplicate: false, cost: 0, balance: users.get(input.uploaderId)?.score ?? 0 }
      },
      async update(id, patch) {
        checkPerm('textures.write')
        const t = textures.get(id)
        if (!t) throw new Error(`texture ${id} not found`)
        const next: PluginTextureDTO = {
          ...t,
          name: patch.name ?? t.name,
          type: patch.type ?? t.type,
          public: patch.public ?? t.public,
          visibility: (patch.public ?? t.public) ? 'public' : 'private',
          updatedAt: new Date().toISOString(),
        }
        textures.set(id, next)
        return next
      },
      async delete(id) {
        checkPerm('textures.write')
        textures.delete(id)
        return { deleted: true as const }
      },
      async readBytes(id) {
        checkPerm('textures.read')
        const bytes = textureBytes.get(id)
        if (!bytes) throw new Error(`texture ${id} bytes not found`)
        return bytes.slice()
      },
    },
    closet: {
      async list(userId) {
        checkPerm('closet.write')
        return { items: closet.get(userId) ?? [], nextBefore: 0 }
      },
      async add(userId, textureId, name) {
        checkPerm('closet.write')
        const t = textures.get(textureId)
        const item: PluginClosetItemDTO = {
          id: nextClosetId++,
          userId,
          textureId,
          name: name || t?.name || `Texture ${textureId}`,
          createdAt: new Date().toISOString(),
          texture: t,
        }
        const list = closet.get(userId) ?? []
        list.push(item)
        closet.set(userId, list)
        return { item, balance: users.get(userId)?.score ?? 0 }
      },
      async rename(userId, textureId, name) {
        checkPerm('closet.write')
        const list = closet.get(userId) ?? []
        const item = list.find((i) => i.textureId === textureId)
        if (!item) throw new Error('closet item not found')
        item.name = name
        return item
      },
      async remove(userId, textureId) {
        checkPerm('closet.write')
        closet.set(
          userId,
          (closet.get(userId) ?? []).filter((i) => i.textureId !== textureId),
        )
        return { removed: true as const, balance: users.get(userId)?.score ?? 0 }
      },
    },
    score: {
      async balance(userId) {
        if (enforce && !permSet.has('score.write') && !permSet.has('users.read')) {
          throw new Error('permission denied: score.write or users.read')
        }
        return users.get(userId)?.score ?? 0
      },
      async apply(userId, delta) {
        checkPerm('score.write')
        const u = users.get(userId)
        if (!u) throw new Error(`user ${userId} not found`)
        if (u.score + delta < 0) throw new Error('insufficient score')
        u.score += delta
        return u.score
      },
    },
    notify: {
      async send(userIds, title, markdown) {
        checkPerm('notify.send')
        const ids = Array.isArray(userIds) ? userIds : [userIds]
        notifications.push({ kind: 'send', target: ids, title, markdown })
        return { sent: ids.length }
      },
      async announce(audience, title, markdown) {
        checkPerm('notify.send')
        notifications.push({ kind: 'announce', target: audience, title, markdown })
        return { id: notifications.length, audience }
      },
    },
    mail: {
      async send(opts) {
        checkPerm('mail.send')
        sentMails.push(opts)
        return { sent: true as const }
      },
    },
    auth: {
      registerProvider(def, verify) {
        checkPerm('auth.provider')
        authProviders.set(def.id, { def, verify })
      },
      registerPasswordVerifier(algo, verify) {
        checkPerm('auth.password')
        passwordVerifiers.set(algo, verify)
      },
    },
    captcha: {
      registerProvider(def, verify) {
        checkPerm('captcha.provider')
        captchaProviders.set(def.id, { def, verify })
      },
    },
    cdn: {
      registerPurger(def, purge) {
        checkPerm('cdn.provider')
        cdnPurgers.set(def.id, { def, purge })
      },
    },
    yggdrasil: {
      async publicKey() {
        checkPerm('yggdrasil.extend')
        return '-----BEGIN PUBLIC KEY-----\nTEST\n-----END PUBLIC KEY-----'
      },
      async lookupName(name) {
        checkPerm('yggdrasil.extend')
        const p = [...players.values()].find((x) => x.name.toLowerCase() === name.toLowerCase())
        return p ? { id: p.uuid, name: p.name } : null
      },
      async lookupProfile(uuid) {
        checkPerm('yggdrasil.extend')
        const p = [...players.values()].find((x) => x.uuid === uuid)
        return p ? { id: p.uuid, name: p.name, properties: [] } : null
      },
      extendMeta() {
        checkPerm('yggdrasil.extend')
      },
      registerMetaProvider() {
        checkPerm('yggdrasil.extend')
      },
    },
    cron: {
      schedule(name, _expr, handler) {
        checkPerm('cron')
        cronJobs.set(name, handler)
      },
    },
    services: {
      expose(name, fn) {
        if (enforce && !(manifest.services ?? []).includes(name)) {
          throw new Error(`service "${name}" is not declared in manifest.services`)
        }
        exposedServices.set(name, fn as (...args: unknown[]) => unknown)
      },
      use(pluginId, svcName) {
        return async (...args: unknown[]) => {
          if (enforce && !(pluginId in (manifest.dependencies ?? {})) && !(pluginId in (manifest.optionalDependencies ?? {}))) {
            throw new Error(`permission denied: dependency "${pluginId}" not declared`)
          }
          const fn = remoteServices.get(`${pluginId}.${svcName}`)
          if (!fn) throw new Error(`remote service ${pluginId}.${svcName} not registered in test host`)
          return (await fn(...args)) as never
        }
      },
      async discover(serviceName) {
        const trimmed = String(serviceName ?? '').trim()
        if (!trimmed) {
          throw new Error('services.discover requires serviceName')
        }
        const suffix = `.${trimmed}`
        const ids: string[] = []
        for (const key of remoteServices.keys()) {
          if (key.endsWith(suffix)) {
            const pid = key.slice(0, -suffix.length)
            if (pid && !ids.includes(pid)) ids.push(pid)
          }
        }
        return ids.sort()
      },
    },
    audit: {
      async record(action, targetType = '', targetId = '', meta = null) {
        auditLogs.push({ action, targetType, targetId, meta })
      },
    },
    crypto: {
      async randomString(len = 16) {
        return randomBytes(Math.ceil(len / 2)).toString('hex').slice(0, len)
      },
      async randomBytes(len) {
        return new Uint8Array(randomBytes(len))
      },
      async sha256(data) {
        return createHash('sha256').update(data).digest('hex')
      },
      async hmac(algo, key, data) {
        return createHmac(algo, key).update(data).digest('hex')
      },
      async timingSafeEqual(a, b) {
        const ba = typeof a === 'string' ? Buffer.from(a) : Buffer.from(a)
        const bb = typeof b === 'string' ? Buffer.from(b) : Buffer.from(b)
        return ba.length === bb.length && nodeTimingSafeEqual(ba, bb)
      },
      async uuid() {
        return randomUUID()
      },
    },
    t: async (key, params, locale) => translate(key, params, locale),
    i18n: {
      t: async (key, params, locale) => translate(key, params, locale),
      locales: async () => Object.keys(localesMap).length > 0 ? Object.keys(localesMap) : ['en'],
    },
    log: {
      debug: (...a) => void logs.push({ level: 'debug', message: a.map(String).join(' ') }),
      info: (...a) => void logs.push({ level: 'info', message: a.map(String).join(' ') }),
      warn: (...a) => void logs.push({ level: 'warn', message: a.map(String).join(' ') }),
      error: (...a) => void logs.push({ level: 'error', message: a.map(String).join(' ') }),
    },
    cache: {
      async get<T = unknown>(key: string) {
        const hit = cacheStore.get(key)
        if (!hit) return null
        if (hit.expiresAt && Date.now() >= hit.expiresAt) {
          cacheStore.delete(key)
          return null
        }
        return hit.value as T
      },
      async set(key, val, ttlSeconds = 0) {
        cacheStore.set(key, {
          value: val,
          expiresAt: ttlSeconds > 0 ? Date.now() + ttlSeconds * 1000 : undefined,
        })
      },
      async delete(key) {
        return cacheStore.delete(key)
      },
    },
  }

  let loadedPlugin: PluginDefinition | null = null

  return {
    ctx,
    fetchMock,
    notifications,
    sentMails,
    auditLogs,
    emittedEvents,
    logs,
    authProviders,
    passwordVerifiers,
    captchaProviders,
    cdnPurgers,

    mockService<TArgs extends unknown[] = unknown[], TResult = unknown>(
      pluginId: string,
      serviceName: string,
      impl: (...args: TArgs) => Promise<TResult> | TResult,
    ) {
      remoteServices.set(`${pluginId}.${serviceName}`, impl as (...args: unknown[]) => unknown)
    },

    async setup(plugin: PluginDefinition) {
      loadedPlugin = plugin
      await plugin.setup(ctx)
      await plugin.onEnable?.(ctx)
    },

    async guard<K extends HookGuardName>(name: K, payload: HookGuardPayloads[K]): Promise<TestGuardResult> {
      const matched = guards.filter((g) => g.name === name).sort((a, b) => a.order - b.order)
      for (const g of matched) {
        const res = await g.fn(payload)
        if (isRejection(res)) {
          return { ok: false, rejected: true, reason: res.reason }
        }
      }
      return { ok: true }
    },

    async filter<K extends HookFilterName>(
      name: K,
      value: HookFilterDefinitions[K]['value'],
      context: HookFilterDefinitions[K]['context'],
    ): Promise<HookFilterDefinitions[K]['value']> {
      let cur = value
      const matched = filters.filter((f) => f.name === name).sort((a, b) => a.order - b.order)
      for (const f of matched) {
        cur = (await f.fn(cur, context)) as HookFilterDefinitions[K]['value']
      }
      return cur
    },

    async emit<K extends HookEventName>(name: K | `plugin:${string}.${string}`, payload?: K extends HookEventName ? HookEventPayloads[K] : unknown): Promise<void> {
      const matched = events.filter((e) => e.name === name).sort((a, b) => a.order - b.order)
      for (const e of matched) {
        await e.fn(payload)
      }
    },

    async request<TBody = Record<string, unknown> | string | null>(
      method: string,
      path: string,
      opts?: {
        query?: Record<string, string>
        headers?: Record<string, string>
        body?: TBody
        user?: PluginHttpRequest['user']
        locale?: string
      },
    ): Promise<{ status: number; headers: Record<string, string>; body: unknown }> {
      const m = method.toUpperCase()
      const cleanPath = path.startsWith('/') ? path : `/${path}`
      let pathMatched = false
      for (const r of routes) {
        const params = matchRoutePattern(r.pattern, cleanPath)
        if (!params) continue
        pathMatched = true
        if (r.method !== m && r.method !== 'ANY' && r.method !== '*') continue
        if (r.auth === 'user' && !opts?.user) return { status: 401, headers: {}, body: { status: 401, detail: 'authentication required' } }
        if (r.auth === 'admin' && (!opts?.user || (opts.user.role !== 'admin' && opts.user.role !== 'superadmin'))) {
          return { status: opts?.user ? 403 : 401, headers: {}, body: { status: opts?.user ? 403 : 401 } }
        }
        if (r.auth === 'superadmin' && opts?.user?.role !== 'superadmin') {
          return { status: opts?.user ? 403 : 401, headers: {}, body: { status: opts?.user ? 403 : 401 } }
        }
        const hdrs: Record<string, string> = {}
        for (const [k, v] of Object.entries(opts?.headers ?? {})) {
          const lk = k.toLowerCase()
          if (lk === 'content-type' || lk === 'accept' || lk === 'user-agent') {
            hdrs[lk] = v
          }
        }
        let rawBody = ''
        let parsedBody: Record<string, unknown> | null = null
        if (typeof opts?.body === 'string') {
          rawBody = opts.body
          if (rawBody.trim().length > 0) {
            const ct = (hdrs['content-type'] ?? '').toLowerCase()
            if (ct.includes('application/json')) {
              try {
                parsedBody = JSON.parse(rawBody) as Record<string, unknown>
              } catch {
                return { status: 400, headers: {}, body: { status: 400, detail: 'invalid JSON request body' } }
              }
            } else {
              try {
                parsedBody = JSON.parse(rawBody) as Record<string, unknown>
              } catch {
                parsedBody = null
              }
            }
          }
        } else if (opts?.body !== undefined && opts?.body !== null) {
          rawBody = JSON.stringify(opts.body)
          parsedBody = opts.body as Record<string, unknown>
        }
        const out = await r.handler({
          method: m,
          path: cleanPath,
          params,
          query: opts?.query ?? {},
          headers: hdrs,
          body: parsedBody,
          rawBody,
          locale: opts?.locale ?? 'en',
          user: opts?.user ?? null,
        })
        if (isRejection(out)) {
          return { status: 422, headers: {}, body: { status: 422, detail: out.reason } }
        }
        if (typeof out === 'object' && out !== null && !Array.isArray(out)) {
          const rec = out as Record<string, unknown>
          const keys = Object.keys(rec)
          const isEnvelope =
            'status' in rec &&
            typeof rec.status === 'number' &&
            rec.status >= 100 &&
            rec.status <= 599 &&
            ('body' in rec || keys.every((k) => k === 'status' || k === 'headers' || k === 'body'))
          if (isEnvelope) {
            const resp = out as PluginHttpResponse
            const cleanHeaders: Record<string, string> = {}
            for (const [hk, hv] of Object.entries(resp.headers ?? {})) {
              const lhk = hk.toLowerCase()
              if (lhk === 'set-cookie' || lhk === 'set-cookie2' || lhk === 'content-security-policy') continue
              cleanHeaders[hk] = String(hv)
            }
            return {
              status: resp.status ?? 200,
              headers: cleanHeaders,
              body: resp.body,
            }
          }
        }
        return { status: 200, headers: {}, body: out }
      }
      if (pathMatched) {
        return { status: 405, headers: {}, body: { status: 405, detail: 'method not allowed' } }
      }
      return { status: 404, headers: {}, body: { status: 404, detail: 'plugin route not found' } }
    },

    async cron(name: string): Promise<void> {
      const fn = cronJobs.get(name)
      if (!fn) throw new Error(`cron job "${name}" not scheduled`)
      await fn()
    },

    async service<T = unknown>(name: string, ...args: unknown[]): Promise<T> {
      const fn = exposedServices.get(name)
      if (!fn) throw new Error(`exposed service "${name}" not found`)
      return (await fn(...args)) as T
    },

    async updateSettings(next: Record<string, unknown>): Promise<void> {
      Object.assign(settingsState, next)
      for (const fn of settingsListeners) {
        await fn(structuredClone(settingsState))
      }
    },

    async teardown(purge = false): Promise<void> {
      if (!loadedPlugin) return
      await loadedPlugin.onDisable?.(ctx)
      if (purge) await loadedPlugin.onUninstall?.(ctx, { purge: true })
    },
  }
}
