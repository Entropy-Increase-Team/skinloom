import type { BlockType } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import {
  buildThemeZip,
  localizedOf,
  localizedValue,
  moved,
  safeToken,
  savedBlocks,
  withThemeSetting,
  withToken,
} from './appearance-form'

describe('withToken', () => {
  it('sets and removes one token, dropping groups left empty', () => {
    const o = withToken({}, 'light', 'primary', ' #336699 ')
    expect(o).toEqual({ light: { primary: '#336699' } })
    expect(withToken(o, 'dark', 'primary', 'red')).toEqual({ light: { primary: '#336699' }, dark: { primary: 'red' } })
    expect(withToken(o, 'light', 'primary', '')).toEqual({})
  })

  it('leaves the input alone', () => {
    const o = { light: { primary: 'red' } }
    withToken(o, 'light', 'border', 'blue')
    expect(o).toEqual({ light: { primary: 'red' } })
  })
})

describe('safeToken', () => {
  it('refuses what could leave the CSS declaration', () => {
    expect(safeToken('oklch(0.6 0.2 300 / 50%)')).toBe(true)
    expect(safeToken('red;}body{display:none')).toBe(false)
    expect(safeToken('x</style>')).toBe(false)
    expect(safeToken('red /* x */')).toBe(false)
  })
})

describe('localized text', () => {
  it('edits a plain string in every language and stores only the filled ones', () => {
    expect(localizedOf('Hi')).toEqual({ 'zh-CN': 'Hi', en: 'Hi' })
    expect(localizedOf({ en: 'Hi', fr: 'Salut' })).toEqual({ 'zh-CN': '', en: 'Hi' })
    expect(localizedOf(undefined)).toEqual({ 'zh-CN': '', en: '' })
    expect(localizedValue({ 'zh-CN': '你好', en: ' ' })).toEqual({ 'zh-CN': '你好' })
    expect(localizedValue({ 'zh-CN': '', en: '' })).toBeUndefined()
  })
})

describe('savedBlocks', () => {
  const types: BlockType[] = [
    { type: 'hero', props: [{ name: 'title', type: 'text' }, { name: 'background', type: 'image' }] },
    { type: 'buttons', props: [{ name: 'items', type: 'buttons' }] },
    { type: 'stats', props: [] },
  ]

  it('leaves out properties without a value and props objects left empty', () => {
    expect(
      savedBlocks(
        [
          { type: 'hero', props: { title: { en: 'Hi' }, background: '' } },
          { type: 'buttons', props: { items: [] } },
          { type: 'stats', props: { stale: 1 } },
        ],
        types,
      ),
    ).toEqual([{ type: 'hero', props: { title: { en: 'Hi' } } }, { type: 'buttons' }, { type: 'stats' }])
  })
})

describe('moved', () => {
  it('swaps with a neighbour and stops at the ends', () => {
    expect(moved(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c'])
    expect(moved(['a', 'b', 'c'], 2, -1)).toEqual(['a', 'c', 'b'])
    const list = ['a', 'b']
    expect(moved(list, 0, -1)).toBe(list)
  })
})

describe('withThemeSetting & buildThemeZip', () => {
  it('updates and cleans up per-theme settings', () => {
    const s = withThemeSetting({}, 'brocade', 'lattice', false)
    expect(s).toEqual({ brocade: { lattice: false } })
    expect(withThemeSetting(s, 'brocade', 'lattice', null)).toEqual({})
  })

  it('packages overrides into a valid zip archive with plugin.json', () => {
    const zip = buildThemeZip('default', { light: { primary: '#336699' } })
    expect(zip[0]).toBe(0x50)
    expect(zip[1]).toBe(0x4b)
    const text = new TextDecoder().decode(zip)
    expect(text).toContain('default-custom/plugin.json')
    expect(text).toContain('"extends": "default"')
    expect(text).toContain('"#336699"')
  })

  it('flattens single-level inheritance when exporting from an extended theme', () => {
    const zip = buildThemeZip(
      {
        id: 'my-child',
        extends: 'default',
        tokens: {
          light: { background: '#ffffff', primary: '#112233' },
          dark: { background: '#111111', primary: '#445566' },
        },
      },
      { light: { primary: '#998877' } },
    )
    const text = new TextDecoder().decode(zip)
    expect(text).toContain('my-child-custom/plugin.json')
    expect(text).toContain('"extends": "default"')
    expect(text).not.toContain('"extends": "my-child"')
    expect(text).toContain('"#998877"')
    expect(text).toContain('"#ffffff"')
  })
})
