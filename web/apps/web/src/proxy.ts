import { type NextRequest, NextResponse } from 'next/server'

import { contentSecurityPolicy, newNonce } from '@/lib/csp'

/**
 * Gives every page request a fresh nonce and the Content-Security-Policy that names it
 * (ADR 0039). Next.js reads the nonce from the policy on the request and puts it on its
 * own scripts; the layout reads x-nonce for custom JS and next-themes' inline script.
 * Every page is rendered per request already (the layout reads the bootstrap with the
 * visitor's cookies), so no page is prerendered without one.
 */
export function proxy(request: NextRequest) {
  const nonce = newNonce()
  const policy = contentSecurityPolicy({
    nonce,
    dev: process.env.NODE_ENV === 'development',
    // Set by the gateway from the trusted proxy configuration.
    https: request.headers.get('x-forwarded-proto') === 'https',
  })
  const headers = new Headers(request.headers)
  headers.set('x-nonce', nonce) // replaces whatever the client sent
  headers.set('Content-Security-Policy', policy)
  const response = NextResponse.next({ request: { headers } })
  response.headers.set('Content-Security-Policy', policy)
  return response
}

export const config = {
  // Not the build's files and icons: they are not pages. Backend paths never reach
  // Next.js (the gateway owns them).
  matcher: ['/((?!_next/static|_next/image|favicon.ico|icon.svg).*)'],
}
