import type {
  HookEventName,
  HookEventPayloads,
  HookFilterDefinitions,
  HookFilterName,
  HookGuardName,
  HookGuardPayloads,
  PluginCustomEventName,
} from '../gen/catalog.js'

export type {
  HookEventName,
  HookEventPayloads,
  HookFilterDefinitions,
  HookFilterName,
  HookGuardName,
  HookGuardPayloads,
  PluginCustomEventName,
  PluginPermission,
} from '../gen/catalog.js'

export interface GuardRejection {
  readonly __skinloomReject: true
  readonly reason: string
}

/** Rejects a guard hook execution cleanly with an RFC 9457 reason shown to users. */
export function reject(reason: string): GuardRejection {
  return {
    __skinloomReject: true,
    reason: String(reason ?? 'rejected'),
  }
}

export type PluginRouteAuth = 'public' | 'user' | 'admin' | 'superadmin'

export interface PluginRouteOptions {
  auth?: PluginRouteAuth
}

export interface PluginHttpRequest<TBody = Record<string, unknown> | null> {
  method: string
  path: string
  params: Record<string, string>
  query: Record<string, string>
  headers: Record<string, string>
  body: TBody
  rawBody: string
  locale: string
  user: {
    id: number
    email: string
    nickname: string
    role: 'user' | 'admin' | 'superadmin'
  } | null
}

export interface PluginHttpResponse<T = unknown> {
  status?: number
  headers?: Record<string, string>
  body?: T
}

export type PluginRouteHandler<TBody = Record<string, unknown> | null, TOut = unknown> = (
  req: PluginHttpRequest<TBody>,
) => Promise<TOut | PluginHttpResponse<TOut> | GuardRejection> | TOut | PluginHttpResponse<TOut> | GuardRejection

export type HttpRegistrar = (
  path: string,
  optsOrHandler: PluginRouteOptions | PluginRouteHandler,
  maybeHandler?: PluginRouteHandler,
) => Promise<void> | void

export type DocIndexOp<T = unknown> = {
  $eq?: T | null
  $gt?: T
  $gte?: T
  $lt?: T
  $lte?: T
  $in?: T[]
  $prefix?: string
  $ne?: T | null
}

export type DocFilter<TDoc extends Record<string, unknown> = Record<string, unknown>> = {
  id?: string | DocIndexOp<string>
} & {
  [K in keyof TDoc]?: TDoc[K] | null | DocIndexOp<NonNullable<TDoc[K]>>
}

export interface DocFindOptions<TDoc extends Record<string, unknown> = Record<string, unknown>> {
  sort?: { id?: 1 | -1 } & { [K in keyof TDoc]?: 1 | -1 }
  limit?: number
  cursor?: string
}

export interface DocFindResult<TDoc extends Record<string, unknown> = Record<string, unknown>> {
  items: Array<TDoc & { id: string }>
  nextCursor?: string
}

export interface PluginCollection<TDoc extends Record<string, unknown> = Record<string, unknown>> {
  insertOne(doc: TDoc): Promise<TDoc & { id: string }>
  findOne(filter?: DocFilter<TDoc>): Promise<(TDoc & { id: string }) | null>
  find(filter?: DocFilter<TDoc>, opts?: DocFindOptions<TDoc>): Promise<DocFindResult<TDoc>>
  updateOne(filter: DocFilter<TDoc>, patch: Partial<TDoc>): Promise<{ matched: boolean; modified: boolean }>
  updateMany(filter: DocFilter<TDoc>, patch: Partial<TDoc>): Promise<{ matched: number; modified: number }>
  deleteOne(filter: DocFilter<TDoc>): Promise<{ deleted: boolean }>
  count(filter?: DocFilter<TDoc>): Promise<number>
}

export interface PluginFetchInit {
  method?: string
  headers?: Record<string, string>
  body?: string
  timeoutMs?: number
}

export interface PluginFetchResponse {
  ok: boolean
  status: number
  statusText: string
  headers: Record<string, string>
  url: string
  text(): Promise<string>
  json<T = unknown>(): Promise<T>
  arrayBuffer(): Promise<ArrayBuffer>
}

export interface PluginUserDTO {
  id: number
  email: string
  nickname: string
  role: 'user' | 'admin' | 'superadmin'
  status: 'active' | 'banned' | 'deleted'
  score: number
  locale: string
  emailVerified: boolean
  avatarTextureId: number | null
  createdAt: string
  lastLoginAt: string | null
}

export interface PluginPlayerDTO {
  id: number
  userId: number
  name: string
  uuid: string
  skinId: number | null
  capeId: number | null
  createdAt: string
  updatedAt: string
}

export interface PluginTextureDTO {
  id: number
  name: string
  type: 'steve' | 'alex' | 'cape'
  hash: string
  size: number
  visibility: 'public' | 'private'
  public: boolean
  likes: number
  uploaderId: number | null
  createdAt: string
  updatedAt: string
}

export interface PluginClosetItemDTO {
  id: number
  userId: number
  textureId: number
  name: string
  createdAt: string
  texture?: PluginTextureDTO
}

export interface PluginAuthProviderDef {
  id: string
  name: string
  icon?: string
  authUrl?: string
  mode?: 'credentials' | 'redirect'
}

export interface ExternalUserInfo {
  subject: string
  email?: string
  emailVerified?: boolean
  nickname?: string
  next?: string
}

export interface PluginContext {
  hooks: {
    reject: typeof reject
    guard<K extends HookGuardName>(
      name: K,
      handler: (payload: HookGuardPayloads[K]) => Promise<void | GuardRejection> | void | GuardRejection,
      order?: number,
    ): Promise<void> | void
    filter<K extends HookFilterName>(
      name: K,
      handler: (
        value: HookFilterDefinitions[K]['value'],
        context: HookFilterDefinitions[K]['context'],
      ) => Promise<HookFilterDefinitions[K]['value']> | HookFilterDefinitions[K]['value'],
      order?: number,
    ): Promise<void> | void
  }

  events: {
    on<K extends HookEventName>(
      name: K,
      handler: (payload: HookEventPayloads[K]) => Promise<void> | void,
      order?: number,
    ): Promise<void> | void
    on<T = unknown>(
      name: PluginCustomEventName,
      handler: (payload: T) => Promise<void> | void,
      order?: number,
    ): Promise<void> | void
    emit<T = unknown>(localName: string, payload?: T): Promise<void>
  }

  http: {
    get: HttpRegistrar
    post: HttpRegistrar
    put: HttpRegistrar
    patch: HttpRegistrar
    delete: HttpRegistrar
    route(
      method: string,
      path: string,
      optsOrHandler: PluginRouteOptions | PluginRouteHandler,
      maybeHandler?: PluginRouteHandler,
    ): Promise<void> | void
  }

  kv: {
    get<T = unknown>(key: string): Promise<T | null>
    set<T = unknown>(key: string, value: T, ttlSeconds?: number): Promise<void>
    delete(key: string): Promise<boolean>
    list<T = unknown>(prefix?: string, limit?: number): Promise<Array<{ key: string; value: T }>>
    incr(key: string, delta?: number): Promise<number>
  }

  db: {
    collection<TDoc extends Record<string, unknown> = Record<string, unknown>>(name: string): PluginCollection<TDoc>
  }

  files: {
    put(key: string, data: Uint8Array | string): Promise<string>
    get(key: string): Promise<Uint8Array | null>
    delete(key: string): Promise<boolean>
    url(key: string): Promise<string>
  }

  fetch(url: string, init?: PluginFetchInit): Promise<PluginFetchResponse>

  settings: {
    get<T = unknown>(key: string): Promise<T | undefined>
    all<T extends Record<string, unknown> = Record<string, unknown>>(): Promise<T>
    patch<T extends Record<string, unknown> = Record<string, unknown>>(patch: Record<string, unknown>): Promise<T>
    onChange(listener: (next: Record<string, unknown>) => Promise<void> | void): Promise<void> | void
  }

  site: {
    name(locale?: string): Promise<string>
    url(): Promise<string>
    description(locale?: string): Promise<string>
    locales(): Promise<string[]>
    publicSettings(): Promise<Record<string, unknown>>
  }

  users: {
    get(id: number): Promise<PluginUserDTO | null>
    find(email: string): Promise<PluginUserDTO | null>
    list(opts?: {
      q?: string
      role?: 'user' | 'admin' | 'superadmin'
      status?: 'active' | 'banned' | 'deleted'
      before?: number
      offset?: number
      limit?: number
      page?: number
      perPage?: number
    }): Promise<{ items: PluginUserDTO[]; nextBefore: number }>
    update(id: number, patch: { nickname?: string; locale?: string; emailVerified?: boolean; verified?: boolean }): Promise<PluginUserDTO>
    ban(id: number, banned?: boolean): Promise<PluginUserDTO>
  }

  players: {
    get(id: number): Promise<PluginPlayerDTO | null>
    findByName(name: string): Promise<PluginPlayerDTO | null>
    list(userId: number): Promise<PluginPlayerDTO[]>
    create(userId: number, name: string, form?: Record<string, unknown> | null): Promise<PluginPlayerDTO & { balance: number }>
    rename(userId: number, playerId: number, name: string): Promise<PluginPlayerDTO>
    setTextures(userId: number, playerId: number, textures: { skin?: number | null; cape?: number | null }): Promise<PluginPlayerDTO>
    delete(userId: number, playerId: number, refund?: boolean): Promise<{ deleted: true; balance: number }>
  }

  textures: {
    get(id: number): Promise<PluginTextureDTO | null>
    list(opts?: {
      q?: string
      category?: '' | 'skin' | 'steve' | 'alex' | 'cape'
      uploaderId?: number
      sort?: 'time' | 'likes'
      cursor?: string
      limit?: number
    }): Promise<{ items: PluginTextureDTO[]; nextCursor: string }>
    create(input: {
      uploaderId: number
      name: string
      type: 'steve' | 'alex' | 'cape'
      public?: boolean
      data?: Uint8Array
      dataBase64?: string
      form?: Record<string, unknown>
    }): Promise<{ texture: PluginTextureDTO; duplicate: boolean; cost: number; balance: number }>
    update(id: number, patch: { name?: string; type?: 'steve' | 'alex' | 'cape'; public?: boolean }): Promise<PluginTextureDTO>
    delete(id: number, refund?: boolean): Promise<{ deleted: true }>
    readBytes(id: number): Promise<Uint8Array>
  }

  closet: {
    list(userId: number, opts?: { q?: string; category?: '' | 'skin' | 'cape'; before?: number; limit?: number }): Promise<{
      items: PluginClosetItemDTO[]
      nextBefore: number
    }>
    add(userId: number, textureId: number, name?: string): Promise<{ item: PluginClosetItemDTO; balance: number }>
    rename(userId: number, textureId: number, name: string): Promise<PluginClosetItemDTO>
    remove(userId: number, textureId: number, refund?: boolean): Promise<{ removed: true; balance: number }>
  }

  score: {
    balance(userId: number): Promise<number>
    apply(userId: number, delta: number, reason?: string, refType?: string, refId?: string): Promise<number>
  }

  notify: {
    send(userIds: number | number[], title: string, markdown: string): Promise<{ sent: number }>
    announce(audience: 'all' | 'users', title: string, markdown: string): Promise<{ id: number; audience: string }>
  }

  mail: {
    send(opts: {
      to: string
      subject?: string
      text?: string
      html?: string
      template?: 'verify_email' | 'reset_password' | 'email_changed' | 'test' | string
      locale?: string
      data?: Record<string, unknown>
    }): Promise<{ sent: true }>
  }

  auth: {
    registerProvider(
      def: PluginAuthProviderDef,
      verify?: (input: Record<string, unknown>) => Promise<ExternalUserInfo> | ExternalUserInfo,
    ): Promise<void> | void
    registerPasswordVerifier(
      algo: string,
      verify: (storedHash: string, password: string) => Promise<boolean> | boolean,
    ): Promise<void> | void
  }

  captcha: {
    registerProvider(
      def: { id: string; name?: string },
      verify: (input: { id?: string; value: string; remoteIp: string }) => Promise<boolean | { ok: boolean } | void> | boolean | { ok: boolean } | void,
    ): Promise<void> | void
  }

  cdn: {
    registerPurger(
      def: { id: string; name?: string },
      purge: (urls: string[]) => Promise<void> | void,
    ): Promise<void> | void
  }

  yggdrasil: {
    publicKey(): Promise<string>
    lookupName(name: string): Promise<{ id: string; name: string } | null>
    lookupProfile(
      uuid: string,
      baseUrl?: string,
    ): Promise<{
      id: string
      name: string
      properties: Array<{ name: string; value: string; signature?: string }>
    } | null>
    extendMeta(ext: { skinDomains?: string[]; meta?: Record<string, unknown> }): Promise<void> | void
    registerMetaProvider(
      provider: () => Promise<{ skinDomains?: string[]; meta?: Record<string, unknown> }> | { skinDomains?: string[]; meta?: Record<string, unknown> },
    ): Promise<void> | void
  }

  cron: {
    schedule(name: string, cronExpr: string, handler: () => Promise<void> | void): Promise<void> | void
  }

  services: {
    expose<TArgs extends unknown[] = unknown[], TResult = unknown>(
      name: string,
      handler: (...args: TArgs) => Promise<TResult> | TResult,
    ): Promise<void> | void
    use<TArgs extends unknown[] = unknown[], TResult = unknown>(
      pluginId: string,
      serviceName: string,
    ): (...args: TArgs) => Promise<TResult>
    discover(serviceName: string): Promise<string[]>
  }

  audit: {
    record(action: string, targetType?: string, targetId?: string, meta?: Record<string, unknown> | null): Promise<void>
  }

  crypto: {
    randomString(length?: number): Promise<string>
    randomBytes(length: number): Promise<Uint8Array>
    sha256(data: Uint8Array | string): Promise<string>
    hmac(algo: 'sha256' | 'sha512' | 'sha1', key: Uint8Array | string, data: Uint8Array | string): Promise<string>
    timingSafeEqual(a: Uint8Array | string, b: Uint8Array | string): Promise<boolean>
    uuid(): Promise<string>
  }

  t(key: string, params?: Record<string, unknown> | null, locale?: string): Promise<string>
  i18n: {
    t(key: string, params?: Record<string, unknown> | null, locale?: string): Promise<string>
    locales(): Promise<string[]>
  }

  log: {
    debug(...args: unknown[]): Promise<void> | void
    info(...args: unknown[]): Promise<void> | void
    warn(...args: unknown[]): Promise<void> | void
    error(...args: unknown[]): Promise<void> | void
  }

  cache: {
    get<T = unknown>(key: string): Promise<T | null>
    set<T = unknown>(key: string, value: T, ttlSeconds?: number): Promise<void>
    delete(key: string): Promise<boolean>
  }
}

export interface PluginDefinition {
  setup(ctx: PluginContext): Promise<void> | void
  onInstall?(ctx: PluginContext): Promise<void> | void
  onEnable?(ctx: PluginContext): Promise<void> | void
  onDisable?(ctx: PluginContext): Promise<void> | void
  onUpgrade?(ctx: PluginContext, info: { from: string }): Promise<void> | void
  onUninstall?(ctx: PluginContext, info: { purge: boolean }): Promise<void> | void
}

/** Declares a SkinLoom backend plugin with full ctx type inference. */
export function definePlugin(def: PluginDefinition): PluginDefinition {
  return def
}
