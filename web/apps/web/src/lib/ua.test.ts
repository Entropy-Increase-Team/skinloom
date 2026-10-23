import { describe, expect, it } from 'vitest'

import { describeUA } from './ua'

describe('describeUA', () => {
  it('names common browsers and systems', () => {
    expect(describeUA('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0')).toBe('Edge · Windows')
    expect(describeUA('Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15')).toBe('Safari · macOS')
    expect(describeUA('Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36')).toBe('Chrome · Android')
    expect(describeUA('Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0')).toBe('Firefox · Linux')
  })
  it('falls back to the raw string', () => {
    expect(describeUA('SkinLoomLauncher/1.0')).toBe('SkinLoomLauncher/1.0')
    expect(describeUA('')).toBe('?')
  })
})
