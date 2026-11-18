import { afterEach, describe, expect, it, vi } from 'vitest'

import { CSRF_HEADER } from './fetcher'
import { uploadSetupArchive } from './upload'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('uploadSetupArchive', () => {
  it('uploads chunks sequentially with CSRF header and retries transient 503', async () => {
    const calls: { url: string; method: string; csrf: string | null; size: number }[] = []
    let putAttempts = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        const headers = new Headers(init?.headers)
        const method = init?.method ?? 'GET'
        const size = init?.body instanceof Blob ? init.body.size : 0
        calls.push({ url, method, csrf: headers.get(CSRF_HEADER), size })
        if (method === 'POST') {
          return new Response(JSON.stringify({ uploadId: 'u-1', chunkSize: 4, maxUploadBytes: 100 }), { status: 200 })
        }
        putAttempts++
        if (putAttempts === 1) {
          return new Response(JSON.stringify({ detail: 'busy' }), { status: 503 })
        }
        return new Response(JSON.stringify({ receivedBytes: size }), { status: 200 })
      }),
    )

    const progress: number[] = []
    const file = new File([new Uint8Array([1, 2, 3, 4, 5, 6])], 'backup.tar.gz', { type: 'application/gzip' })
    const res = await uploadSetupArchive('backup', file, (p) => progress.push(p.loaded))

    expect(res.uploadId).toBe('u-1')
    expect(calls[0]?.method).toBe('POST')
    expect(calls[0]?.csrf).toBe('1')
    expect(calls[1]?.url).toContain('/api/v1/setup/uploads/u-1/chunks?offset=0')
    expect(calls[2]?.url).toContain('/api/v1/setup/uploads/u-1/chunks?offset=0')
    expect(calls[2]?.size).toBe(4)
    expect(calls[3]?.url).toContain('/api/v1/setup/uploads/u-1/chunks?offset=4')
    expect(calls[3]?.size).toBe(2)
    expect(progress).toEqual([0, 4, 6])
  })
})
