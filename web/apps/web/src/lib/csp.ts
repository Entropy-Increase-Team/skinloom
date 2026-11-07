/**
 * The pages' Content-Security-Policy (ADR 0039, design 03 S16). Scripts run only when
 * they carry the request's nonce, or when a script that does loads them
 * ('strict-dynamic'): Next.js puts the nonce on its own scripts, the layout on custom
 * JS, and the captcha widgets are loaded by our code. An injected <script> or inline
 * event handler runs nowhere.
 *
 * Styles and images stay open: themes and custom CSS may use web fonts and pictures
 * from anywhere, inline style attributes are everywhere in React, and none of that runs
 * code.
 */
export interface PolicyOptions {
  nonce: string
  /** next dev: React rebuilds server error stacks with eval. */
  dev?: boolean
  /** The page was requested over HTTPS: upgrade the http:// pictures it links. */
  https?: boolean
}

/** Frames of the captcha providers (components/auth/captcha.tsx). */
const CAPTCHA_FRAMES = [
  'https://challenges.cloudflare.com',
  'https://www.recaptcha.net',
  'https://recaptcha.net',
  'https://www.google.com',
  'https://hcaptcha.com',
  'https://*.hcaptcha.com',
]

export function contentSecurityPolicy({ nonce, dev = false, https = false }: PolicyOptions): string {
  const directives: [string, ...string[]][] = [
    ['default-src', "'self'"],
    ['script-src', "'self'", `'nonce-${nonce}'`, "'strict-dynamic'", ...(dev ? ["'unsafe-eval'"] : [])],
    ['style-src', "'self'", "'unsafe-inline'", 'https:'],
    ['img-src', "'self'", 'data:', 'blob:', 'https:', 'http:'],
    ['font-src', "'self'", 'data:', 'https:'],
    // Any HTTPS server: analytics loaded by custom JS send with fetch or sendBeacon, and
    // hCaptcha talks to its servers from the page. Pictures may already go anywhere, so
    // this lets nothing out that img-src does not.
    ['connect-src', "'self'", 'https:'],
    ['frame-src', ...CAPTCHA_FRAMES],
    ['object-src', "'none'"],
    ['base-uri', "'self'"],
    // Forms post only here. The OAuth consent and social sign-in leave the site with
    // location.assign, not a form: Chromium would hold a form's redirect to form-action.
    ['form-action', "'self'"],
    // Same as X-Frame-Options: SAMEORIGIN from the backend.
    ['frame-ancestors', "'self'"],
  ]
  if (https) directives.push(['upgrade-insecure-requests'])
  return directives.map((d) => d.join(' ')).join('; ')
}

/** A fresh nonce: 128 random bits, base64. */
export function newNonce(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return btoa(String.fromCharCode(...bytes))
}
