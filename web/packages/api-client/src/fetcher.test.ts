import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError, apiBase, apiFetch, CSRF_HEADER, SERVER_TIMEOUT_MS } from './fetcher'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
})

function stubFetch(status: number, body: string) {
  const fn = vi.fn(async (_url: string, init?: RequestInit) => {
    void init
    return new Response(status === 204 ? null : body, { status })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('apiBase', () => {
  it('uses the internal backend address on the server', () => {
    vi.stubEnv('SKINLOOM_API_INTERNAL', 'http://127.0.0.1:9999')
    expect(apiBase()).toBe('http://127.0.0.1:9999')
  })
})

describe('apiFetch', () => {
  it('returns data, status and headers', async () => {
    const fetch = stubFetch(200, '{"a":1}')
    const res = await apiFetch<{ data: { a: number }; status: number }>('/api/x')
    expect(res.data.a).toBe(1)
    expect(res.status).toBe(200)
    expect(fetch.mock.calls[0]?.[0]).toMatch(/\/api\/x$/)
  })

  it('marks state-changing requests with the CSRF header and keeps caller headers', async () => {
    const fetch = stubFetch(200, '{}')
    await apiFetch('/api/x', { method: 'POST', headers: { 'Content-Type': 'application/json' } })
    await apiFetch('/api/x', { headers: { cookie: 'a=b' } })
    const post = new Headers(fetch.mock.calls[0]?.[1]?.headers)
    const get = new Headers(fetch.mock.calls[1]?.[1]?.headers)
    expect(post.get(CSRF_HEADER)).toBe('1')
    expect(post.get('content-type')).toBe('application/json')
    expect(get.get(CSRF_HEADER)).toBeNull()
    expect(get.get('cookie')).toBe('a=b')
  })

  it('sets a timeout signal on server-side calls', async () => {
    const fetch = stubFetch(200, '{}')
    await apiFetch('/api/x')
    const init = fetch.mock.calls[0]?.[1]
    expect(init?.signal).toBeInstanceOf(AbortSignal)
    expect(SERVER_TIMEOUT_MS).toBeGreaterThan(0)
  })

  it('keeps a caller-provided signal', async () => {
    const fetch = stubFetch(200, '{}')
    const ctl = new AbortController()
    await apiFetch('/api/x', { signal: ctl.signal })
    expect(fetch.mock.calls[0]?.[1]?.signal).toBe(ctl.signal)
  })

  it('handles 204 No Content', async () => {
    stubFetch(204, '')
    const res = await apiFetch<{ data: unknown; status: number }>('/api/x')
    expect(res.status).toBe(204)
    expect(res.data).toBeUndefined()
  })

  it('throws ApiError with problem details on errors', async () => {
    stubFetch(422, '{"title":"Unprocessable Entity","detail":"invalid settings","errors":[{"location":"body.x"}]}')
    const err = await apiFetch('/api/x').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(422)
    expect((err as ApiError).message).toBe('invalid settings')
    expect((err as ApiError).problem?.errors?.[0]?.location).toBe('body.x')
  })

  it('turns non-JSON bodies (proxy error pages) into ApiError instead of a SyntaxError', async () => {
    stubFetch(502, '<html>Bad Gateway</html>')
    const err = await apiFetch('/api/x').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(502)
  })
})
