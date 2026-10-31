import { describe, expect, it } from 'vitest'

import { flatten, overrideProblem, savedOverrides, withOverrides } from './messages'

describe('withOverrides', () => {
  const base = { nav: { skinlib: '皮肤库', about: '关于' }, hero: { join: '加入' } }

  it('replaces existing texts and copies the path, leaving the shared messages alone', () => {
    const out = withOverrides(base, { 'nav.skinlib': '皮肤广场' })
    expect(out).toEqual({ nav: { skinlib: '皮肤广场', about: '关于' }, hero: { join: '加入' } })
    expect(base.nav.skinlib).toBe('皮肤库')
    expect(out.hero).toBe(base.hero)
  })

  it('ignores keys that are missing or name a group', () => {
    expect(withOverrides(base, { 'nav.gone': 'x', nav: 'x', 'hero.join.deeper': 'x' })).toBe(base)
    expect(withOverrides(base, undefined)).toBe(base)
  })
})

describe('flatten', () => {
  it('lists texts by dotted key', () => {
    expect(flatten({ a: { b: 'x', c: { d: 'y' } }, e: 'z' })).toEqual([
      ['a.b', 'x'],
      ['a.c.d', 'y'],
      ['e', 'z'],
    ])
  })
})

describe('overrideProblem', () => {
  it('wants a well-formed message with the same placeholders as the default', () => {
    expect(overrideProblem('登录 {site}', '欢迎来到 {site}')).toBeNull()
    expect(overrideProblem('登录 {site}', '欢迎')).toBe('placeholders')
    expect(overrideProblem('登录', '登录 {site}')).toBe('placeholders')
    expect(overrideProblem('登录 {site}', '登录 {site')).toBe('syntax')
    expect(overrideProblem('x', '}{')).toBe('syntax')
  })

  // Texts next-intl cannot render show as their key: the editor refuses them.
  it('refuses broken ICU that still pairs its braces, and tags the default does not have', () => {
    expect(overrideProblem('Hello {name}', 'Hello {name, foo}')).toBe('syntax')
    expect(overrideProblem('Sign in to {site}', 'Sign in to <b>{site}</b>')).toBe('placeholders')
    expect(overrideProblem('Set up in <code>{dir}</code>', 'In <code>{dir}</code> einrichten')).toBeNull()
  })

  it('reads plural forms: a one-word branch is no placeholder', () => {
    const def = '{count, plural, =0 {No launcher signed in.} one {One launcher.} other {# launchers.}}'
    expect(overrideProblem(def, '{count, plural, =0 {None} one {One launcher.} other {# launchers.}}')).toBeNull()
    expect(overrideProblem(def, '{n, plural, other {# launchers.}}')).toBe('placeholders')
  })
})

describe('savedOverrides', () => {
  it('drops empty texts and languages', () => {
    expect(savedOverrides({ 'zh-CN': { a: '甲', b: ' ' }, en: { a: '' } })).toEqual({ 'zh-CN': { a: '甲' } })
    expect(savedOverrides({ en: { a: '' } })).toBeNull()
  })
})

