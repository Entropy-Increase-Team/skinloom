import { describe, expect, it } from 'vitest'

import { contentSecurityPolicy, newNonce } from './csp'

function directives(policy: string): Map<string, string[]> {
  return new Map(
    policy.split('; ').map((d) => {
      const [name, ...values] = d.split(' ')
      return [name, values]
    }),
  )
}

describe('contentSecurityPolicy', () => {
  it('runs only scripts with the nonce or loaded by one', () => {
    const d = directives(contentSecurityPolicy({ nonce: 'abc' }))
    expect(d.get('script-src')).toEqual(["'self'", "'nonce-abc'", "'strict-dynamic'"])
    expect(d.get('object-src')).toEqual(["'none'"])
    expect(d.get('base-uri')).toEqual(["'self'"])
    expect(d.get('frame-ancestors')).toEqual(["'self'"])
    expect(d.get('script-src')).not.toContain("'unsafe-inline'")
    expect(d.get('script-src')).not.toContain("'unsafe-eval'")
  })

  it('lets the captcha providers frame their widgets', () => {
    const d = directives(contentSecurityPolicy({ nonce: 'abc' }))
    expect(d.get('frame-src')).toEqual(
      expect.arrayContaining(['https://challenges.cloudflare.com', 'https://www.recaptcha.net', 'https://*.hcaptcha.com']),
    )
  })

  it('lets scripts send to any HTTPS server (analytics, hCaptcha)', () => {
    expect(directives(contentSecurityPolicy({ nonce: 'abc' })).get('connect-src')).toEqual(["'self'", 'https:'])
  })

  it('lets forms post only to the site', () => {
    expect(directives(contentSecurityPolicy({ nonce: 'abc' })).get('form-action')).toEqual(["'self'"])
  })

  it('adds eval only in development and the upgrade only over HTTPS', () => {
    expect(directives(contentSecurityPolicy({ nonce: 'n', dev: true })).get('script-src')).toContain("'unsafe-eval'")
    expect(directives(contentSecurityPolicy({ nonce: 'n' })).has('upgrade-insecure-requests')).toBe(false)
    expect(directives(contentSecurityPolicy({ nonce: 'n', https: true })).has('upgrade-insecure-requests')).toBe(true)
  })
})

describe('newNonce', () => {
  it('is 128 random bits in base64, different every time', () => {
    const a = newNonce()
    expect(a).toMatch(/^[A-Za-z0-9+/]{22}==$/)
    expect(newNonce()).not.toBe(a)
  })
})
