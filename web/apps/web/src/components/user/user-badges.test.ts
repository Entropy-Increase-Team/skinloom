import { describe, expect, it } from 'vitest'

import { badgeVariant } from './user-badges'

describe('badgeVariant', () => {
  it('maps semantic and plugin color names to design-token badge variants', () => {
    expect(badgeVariant('primary')).toBe('default')
    expect(badgeVariant('highlight')).toBe('highlight')
    expect(badgeVariant('gold')).toBe('highlight')
    expect(badgeVariant('success')).toBe('success')
    expect(badgeVariant('warning')).toBe('warning')
    expect(badgeVariant('destructive')).toBe('destructive')
    expect(badgeVariant('outline')).toBe('outline')
    expect(badgeVariant('#2563eb')).toBe('secondary')
    expect(badgeVariant(undefined)).toBe('secondary')
  })
})
