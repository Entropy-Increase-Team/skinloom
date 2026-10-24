import { headers } from 'next/headers'

/**
 * Request options for API calls made while rendering a page (server only): the
 * visitor's cookie and language are forwarded, so they see what they may see (their
 * own private textures, for instance) in their language.
 */
export async function asVisitor(): Promise<{ headers: Record<string, string> }> {
  const h = await headers()
  const forward: Record<string, string> = {}
  for (const name of ['accept-language', 'cookie']) {
    const v = h.get(name)
    if (v) forward[name] = v
  }
  return { headers: forward }
}
