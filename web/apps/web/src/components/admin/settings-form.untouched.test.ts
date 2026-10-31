import type { SettingView } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { changesOf, draftOf } from './settings-form'

// An untouched form reports and sends no changes.

function view(over: Partial<SettingView>): SettingView {
  return { key: 'x', type: 'string', value: '', default: '', isSet: true, public: false, secret: false, superadmin: false, doc: '', ...over }
}

function untouched(views: SettingView[]) {
  return changesOf(views, Object.fromEntries(views.map((v) => [v.key, draftOf(v)])))
}

describe('changesOf on an untouched section', () => {
  it('does not report a text setting whose stored value ends in a newline (custom CSS set via the API or CLI)', () => {
    // The server stores text as given; theme.custom_css is superadmin-only, so another
    // admin would see "unsaved" and Save would send it, which the server refuses (403).
    const css = view({ key: 'theme.custom_css', type: 'text', value: 'body { color: red }\n', superadmin: true })
    expect(untouched([css]).changes).toEqual({})
  })

  it('does not report or drop a localized value with a language the editor has no field for', () => {
    const name = view({ key: 'site.name', type: 'localized', value: { 'zh-CN': '云织', en: 'SkinLoom', ja: 'スキン' } })
    expect(untouched([name]).changes).toEqual({})
  })
})

describe('changesOf on an edited localized value', () => {
  it('keeps the languages the editor has no field for, and drops a cleared one', () => {
    const name = view({ key: 'site.name', type: 'localized', value: { 'zh-CN': '云织', en: 'SkinLoom', ja: 'スキン' } })
    const draft = { ...draftOf(name), value: { 'zh-CN': '云织站', en: '' } }
    expect(changesOf([name], { 'site.name': draft }).changes).toEqual({ 'site.name': { 'zh-CN': '云织站', ja: 'スキン' } })
  })
})
