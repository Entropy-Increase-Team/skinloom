import { describe, expect, it } from 'vitest'

import { formatRelative } from './relative-time'
import { fill } from './labels'

describe('formatRelative', () => {
  const now = new Date('2026-11-09T12:00:00Z')
  const ago = (s: number) => new Date(now.getTime() - s * 1000)

  it('picks the largest whole unit', () => {
    expect(formatRelative(ago(20), now, 'en')).toBe('now')
    expect(formatRelative(ago(3 * 60), now, 'en')).toBe('3 minutes ago')
    expect(formatRelative(ago(5 * 3600), now, 'zh-CN')).toBe('5小时前')
    expect(formatRelative(ago(26 * 3600), now, 'en')).toBe('yesterday')
    expect(formatRelative(ago(-2 * 86400), now, 'en')).toBe('in 2 days')
  })
})

describe('fill', () => {
  it('replaces known placeholders and keeps the rest', () => {
    expect(fill('{count} 项未保存', { count: 3 })).toBe('3 项未保存')
    expect(fill('{current} / {total} {x}', { current: 1, total: 5 })).toBe('1 / 5 {x}')
  })
})
