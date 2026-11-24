import { describe, expect, it } from 'vitest'

import type { PackageInspectResult, PluginSettingFieldView } from '@skinloom/api-client'

import {
  apiProblemMessage,
  buildPluginSettingsDraft,
  buildPluginSettingsPatch,
  canSubmitPackageInstall,
  collectInitialGrantedPermissions,
  formatDuration,
  isPluginSettingsDirty,
  permissionMessageKey,
  resolveLocalizedFieldText,
} from './plugins-settings-form'
import { summarizePlatformFamilies } from './plugins-market'

describe('plugins-settings-form', () => {
  const fields: PluginSettingFieldView[] = [
    { key: 'title', type: 'string', value: 'Hello', default: 'Default', isSet: true, secret: false },
    { key: 'desc', type: 'markdown', value: '# Hi', isSet: true, secret: false },
    { key: 'count', type: 'number', value: 5, min: 1, max: 10, isSet: true, secret: false },
    { key: 'enabled', type: 'boolean', value: true, isSet: true, secret: false },
    { key: 'mode', type: 'select', options: ['a', 'b'], value: 'a', isSet: true, secret: false },
    { key: 'tags', type: 'multiselect', options: ['x', 'y'], value: ['x'], isSet: true, secret: false },
    { key: 'accent', type: 'color', value: '#0f766e', isSet: true, secret: false },
    { key: 'logo', type: 'image', value: '/logo.png', isSet: true, secret: false },
    { key: 'token', type: 'secret', value: null, isSet: true, secret: true },
    { key: 'extra', type: 'json', value: { k: 1 }, isSet: true, secret: false },
    { key: 'allow', type: 'list', value: ['one', 'two'], isSet: true, secret: false },
    {
      key: 'nested',
      type: 'group',
      value: { host: 'localhost', tls: true },
      fields: { host: { type: 'string' }, tls: { type: 'boolean' } },
      isSet: true,
      secret: false,
    },
    { key: 'themeMode', type: 'choice', options: ['light', 'dark'], value: 'dark', isSet: true, secret: false },
  ]

  it('never populates secret values into draft and omits untouched secrets from patch', () => {
    const draft = buildPluginSettingsDraft(fields)
    expect(draft.token).toBe('')
    expect(draft.count).toBe('5')
    expect(draft.enabled).toBe(true)

    const { patch, errors } = buildPluginSettingsPatch(fields, draft, new Set())
    expect(errors).toEqual({})
    expect('token' in patch).toBe(false)
    expect(patch.count).toBe(5)
    expect(patch.extra).toEqual({ k: 1 })
    expect(patch.allow).toEqual(['one', 'two'])
    expect(patch.nested).toEqual({ host: 'localhost', tls: true })
  })

  it('sends null when secret is explicitly cleared and validates JSON/number ranges', () => {
    const draft = buildPluginSettingsDraft(fields)
    draft.count = '99'
    draft.extra = '{bad-json'
    const res = buildPluginSettingsPatch(fields, draft, new Set(['token']))
    expect(res.patch.token).toBeNull()
    expect(res.errors.count).toBeDefined()
    expect(res.errors.extra).toBeDefined()
  })

  it('enforces untrusted checkbox and ID typing confirmation', () => {
    const inspect: PackageInspectResult = {
      id: 'oauth-discord',
      kind: 'plugin',
      runtime: 'js',
      hostPlatform: 'linux/amd64',
      platformCompatible: true,
      version: '1.0.0',
      name: 'Discord',
      isUpgrade: false,
      hasServer: true,
      hasWeb: false,
      hooksCount: 1,
      routesCount: 1,
      settingsCount: 0,
      archiveBytes: 1024,
      archiveSha256: 'a'.repeat(64),
      verification: { hasChecksums: true, signed: false, verified: false, trusted: false },
      permissions: {
        normal: [{ name: 'storage.kv', tier: 'normal', description: 'KV', granted: true }],
        sensitive: [{ name: 'http.fetch', tier: 'sensitive', description: 'Fetch', granted: false }],
        high_risk: [{ name: 'auth.provider', tier: 'high_risk', description: 'Auth', granted: false }],
      },
      requiresHighRiskConfirm: true,
    }
    const initPerms = collectInitialGrantedPermissions(inspect.permissions)
    expect(initPerms).toEqual(['storage.kv'])
    expect(canSubmitPackageInstall(inspect, initPerms, false, 'oauth-discord')).toBe(false)
    expect(canSubmitPackageInstall(inspect, initPerms, true, 'wrong')).toBe(false)
    expect(canSubmitPackageInstall(inspect, initPerms, true, 'oauth-discord')).toBe(true)

    // Production unsigned native plugin is hard-blocked regardless of allowUnsigned / typed ID.
    const blockedNative: PackageInspectResult = {
      ...inspect,
      id: 'storage-s3',
      runtime: 'native',
      nativeInstallBlocked: true,
      nativeBlockReason: 'Production requires official signature',
      permissions: {
        normal: [],
        sensitive: [],
        high_risk: [{ name: 'native.exec', tier: 'high_risk', description: 'Native subprocess', granted: false }],
      },
    }
    expect(canSubmitPackageInstall(blockedNative, ['native.exec'], true, 'storage-s3')).toBe(false)

    // Incompatible platform native plugin is also blocked.
    const wrongArchNative: PackageInspectResult = {
      ...blockedNative,
      nativeInstallBlocked: false,
      platformCompatible: false,
      verification: { hasChecksums: true, signed: true, verified: true, trusted: true, official: true },
    }
    expect(canSubmitPackageInstall(wrongArchNative, ['native.exec'], false, 'storage-s3')).toBe(false)

    // Signed & compatible native plugin requires typed ID confirmation.
    const validNative: PackageInspectResult = {
      ...wrongArchNative,
      platformCompatible: true,
    }
    expect(canSubmitPackageInstall(validNative, ['native.exec'], false, '')).toBe(false)
    expect(canSubmitPackageInstall(validNative, ['native.exec'], false, 'storage-s3')).toBe(true)
    expect(formatDuration(93784)).toBe('1d 2h 3m')
    expect(formatDuration(125)).toBe('2m 5s')
  })

  it('tracks dirty fields, sends sparse group patches, and rejects empty number list items or invalid secrets', () => {
    const schema: PluginSettingFieldView[] = [
      { key: 'title', type: 'string', value: 'Same', isSet: true, secret: false },
      { key: 'optionalSelect', type: 'select', options: ['a', 'b'], value: 'a', isSet: true, secret: false },
      { key: 'nums', type: 'list', item: { type: 'number' }, value: [1], isSet: true, secret: false },
      {
        key: 'nested',
        type: 'group',
        value: { host: 'db.local', port: 5432, tls: true },
        fields: {
          host: { type: 'string' },
          port: { type: 'number', min: 1 },
          tls: { type: 'boolean' },
          apiSecret: { type: 'secret', isSet: true },
        },
        isSet: true,
        secret: false,
      },
    ]

    const initialDraft = buildPluginSettingsDraft(schema)
    expect(isPluginSettingsDirty(schema, initialDraft, new Set(), initialDraft)).toBe(false)

    // Unchanged draft produces empty sparse patch.
    expect(buildPluginSettingsPatch(schema, initialDraft, new Set(), initialDraft)).toEqual({ patch: {}, errors: {} })

    // Editing optional select to '' -> null, editing nested.tls leaves host/port/apiSecret untouched.
    const edited = {
      ...initialDraft,
      optionalSelect: '',
      nested: { ...(initialDraft.nested as Record<string, unknown>), tls: false },
    }
    expect(isPluginSettingsDirty(schema, edited, new Set(), initialDraft)).toBe(true)
    const sparse = buildPluginSettingsPatch(schema, edited, new Set(), initialDraft)
    expect(sparse.errors).toEqual({})
    expect(sparse.patch).toEqual({
      optionalSelect: null,
      nested: { tls: false },
    })

    // Empty string inside number list item triggers error instead of silently becoming 0.
    const badList = { ...initialDraft, nums: ['1', ''] }
    const badListRes = buildPluginSettingsPatch(schema, badList, new Set(), initialDraft)
    expect(badListRes.errors.nums).toBeDefined()

    // Invalid number range inside group triggers nested path error without crashing.
    const badNestedNum = {
      ...initialDraft,
      nested: { ...(initialDraft.nested as Record<string, unknown>), port: '0' },
    }
    expect(buildPluginSettingsPatch(schema, badNestedNum, new Set(), initialDraft).errors['nested.port']).toBeDefined()

    // Clearing nested secret sets nested.apiSecret = null.
    const cleared = buildPluginSettingsPatch(schema, initialDraft, new Set(['nested.apiSecret']), initialDraft)
    expect(cleared.patch).toEqual({ nested: { apiSecret: null } })

    // Typing placeholder ******** into a secret keeps form clean (matching patch omission).
    const maskedNested = {
      ...initialDraft,
      nested: { ...(initialDraft.nested as Record<string, unknown>), apiSecret: '********' },
    }
    expect(isPluginSettingsDirty(schema, maskedNested, new Set(), initialDraft)).toBe(false)

    // Problem message falls back cleanly without leaking raw English Error text,
    // while preserving HTTP status codes from proxy/gateway ApiErrors (e.g. 504/524/413).
    expect(apiProblemMessage('Server reason', new Error('Request failed 500'), 'Fallback')).toBe('Server reason')
    expect(apiProblemMessage(undefined, new Error('Request failed 500'), 'Fallback')).toBe('Fallback')
    expect(apiProblemMessage(undefined, { status: 504 }, 'Fallback')).toBe('Fallback (HTTP 504)')
    expect(apiProblemMessage(undefined, { status: 524 }, 'Fallback')).toBe('Fallback (HTTP 524)')

    // Platform family summarization for market cards
    expect(summarizePlatformFamilies(undefined)).toEqual(['any'])
    expect(summarizePlatformFamilies(['any'])).toEqual(['any'])
    expect(
      summarizePlatformFamilies(['linux/amd64', 'linux/arm64', 'windows/amd64', 'darwin/arm64']),
    ).toEqual(['linux', 'windows', 'macos'])

    // Localized text resolution prioritizes site UI language and family matches
    const labels = { 'zh-CN': '中文标题', en: 'English Title' }
    expect(resolveLocalizedFieldText(labels, 'zh-CN', 'fallback')).toBe('中文标题')
    expect(resolveLocalizedFieldText(labels, 'zh', 'fallback')).toBe('中文标题')
    expect(resolveLocalizedFieldText(labels, 'zh_Hans', 'fallback')).toBe('中文标题')
    expect(resolveLocalizedFieldText(labels, 'en', 'fallback')).toBe('English Title')
    expect(resolveLocalizedFieldText(labels, 'en-US', 'fallback')).toBe('English Title')
    // Canonical zh-CN wins over other zh-* variants for generic zh / zh-Hans, while exact zh-TW still wins
    const zhVariants = { 'zh-AU': '澳中文', 'zh-TW': '繁體標題', 'zh-CN': '简体标题' }
    expect(resolveLocalizedFieldText(zhVariants, 'zh', 'fallback')).toBe('简体标题')
    expect(resolveLocalizedFieldText(zhVariants, 'zh-Hans', 'fallback')).toBe('简体标题')
    expect(resolveLocalizedFieldText(zhVariants, 'zh-TW', 'fallback')).toBe('繁體標題')
    expect(permissionMessageKey('storage.docs')).toBe('storage_docs')
    expect(permissionMessageKey('native.exec')).toBe('native_exec')
  })
})
