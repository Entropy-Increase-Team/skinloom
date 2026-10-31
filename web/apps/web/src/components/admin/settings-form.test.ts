import type { SettingView } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { changesOf, draftOf, groupSettings } from './settings-form'

const view = (key: string, type: string, value: unknown, extra: Partial<SettingView> = {}): SettingView => ({
  key,
  type,
  value,
  default: value,
  isSet: false,
  public: false,
  secret: false,
  superadmin: false,
  doc: '',
  ...extra,
})

describe('groupSettings', () => {
  const views = [
    view('mail.driver', 'string', 'none'),
    view('auth.social.providers', 'json', []),
    view('auth.registration', 'string', 'open'),
    view('captcha.provider', 'string', 'image'),
    view('plugin.x', 'bool', true),
    view('seo.keywords', 'localized', {}),
    view('site.favicon', 'string', ''),
    view('site.logo', 'string', ''),
    view('site.name', 'localized', {}),
    view('theme.custom_css', 'text', ''),
    view('theme.active', 'string', 'default'),
    view('home.blocks', 'json', null),
  ]

  it('puts each setting in the section of its longest prefix, sections in page order', () => {
    expect(groupSettings(views, 'settings').map((g) => [g.id, g.settings.map((s) => s.key)])).toEqual([
      ['site', ['site.name']],
      ['auth', ['auth.registration', 'captcha.provider']],
      ['social', ['auth.social.providers']],
      ['mail', ['mail.driver']],
      ['other', ['plugin.x']],
    ])
  })

  it('leaves the appearance settings to the appearance page, theme and home to their own editors', () => {
    // In the order of the section's prefixes, not alphabetical.
    expect(groupSettings(views, 'appearance').map((g) => [g.id, g.settings.map((s) => s.key)])).toEqual([
      ['branding', ['site.logo', 'site.favicon', 'seo.keywords']],
      ['customCss', ['theme.custom_css']],
    ])
  })
})

describe('changesOf', () => {
  it('sends only what changed, typed as the setting', () => {
    const views = [
      view('site.name', 'localized', { 'zh-CN': '云织', en: 'SkinLoom' }),
      view('site.announcement', 'localized', {}),
      view('score.initial', 'int', 1000, { min: 0 }),
      view('auth.require_email_verification', 'bool', false),
      view('mail.driver', 'string', 'none', { enum: ['none', 'smtp', 'log'] }),
    ]
    const drafts = Object.fromEntries(views.map((v) => [v.key, draftOf(v)]))
    // Untouched: the empty languages of a blank announcement are no change.
    expect(changesOf(views, drafts)).toEqual({ changes: {}, problems: {} })

    drafts['site.name'] = { value: { 'zh-CN': '云织', en: '' } }
    drafts['score.initial'] = { value: ' 500 ' }
    drafts['auth.require_email_verification'] = { value: true }
    drafts['mail.driver'] = { value: 'smtp' }
    expect(changesOf(views, drafts).changes).toEqual({
      'site.name': { 'zh-CN': '云织' },
      'score.initial': 500,
      'auth.require_email_verification': true,
      'mail.driver': 'smtp',
    })
  })

  it('sends a secret only when one is typed in, and null to clear it', () => {
    const pw = view('mail.smtp.password', 'string', null, { secret: true, isSet: true })
    expect(draftOf(pw).value).toBe('')
    expect(changesOf([pw], { [pw.key]: { value: '' } }).changes).toEqual({})
    expect(changesOf([pw], { [pw.key]: { value: 'new secret ' } }).changes).toEqual({ [pw.key]: 'new secret ' })
    expect(changesOf([pw], { [pw.key]: { value: '', reset: true } }).changes).toEqual({ [pw.key]: null })
  })

  it('resets only settings that are set', () => {
    const port = view('mail.smtp.port', 'int', 465)
    expect(changesOf([port], { [port.key]: { value: '465', reset: true } }).changes).toEqual({})
    const set = { ...port, isSet: true, value: 587 }
    expect(changesOf([set], { [set.key]: { value: '587', reset: true } }).changes).toEqual({ [set.key]: null })
  })

  it('checks the declared range, choices and JSON before saving', () => {
    const views = [
      view('mail.smtp.port', 'int', 465, { min: 1, max: 65535 }),
      view('score.sign_gap_hours', 'int', 24, { min: 1, max: 8760 }),
      view('texture.private_status', 'int', 403, { enum: [403, 404] }),
      view('auth.social.providers', 'json', []),
    ]
    const { changes, problems } = changesOf(views, {
      'mail.smtp.port': { value: '70000' },
      'score.sign_gap_hours': { value: '1.5' },
      'texture.private_status': { value: '404' },
      'auth.social.providers': { value: '[{"id": ' },
    })
    expect(changes).toEqual({ 'texture.private_status': 404 })
    expect(problems).toEqual({
      'mail.smtp.port': { code: 'range', min: 1, max: 65535 },
      'score.sign_gap_hours': { code: 'integer' },
      'auth.social.providers': { code: 'json' },
    })
  })

  it('leaves a redacted JSON value alone until it is edited', () => {
    const providers = view('auth.social.providers', 'json', [{ id: 'github', clientSecret: '********' }])
    const d = draftOf(providers)
    expect(changesOf([providers], { [providers.key]: d }).changes).toEqual({})
    const edited = String(d.value).replace('github', 'gh')
    expect(changesOf([providers], { [providers.key]: { value: edited } }).changes).toEqual({
      [providers.key]: [{ id: 'gh', clientSecret: '********' }],
    })
  })
})
