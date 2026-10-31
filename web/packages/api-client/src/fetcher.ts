/**
 * Transport used by every generated API function.
 *
 * In the browser requests go to the same origin (the Go gateway serves /api). On the
 * server (React Server Components) they go straight to the backend's internal address,
 * which the launcher passes as SKINLOOM_API_INTERNAL.
 */
export function apiBase(): string {
  if (typeof window !== 'undefined') return ''
  return process.env.SKINLOOM_API_INTERNAL ?? 'http://127.0.0.1:17546'
}

/** Server-side calls give up after this long so a stuck backend cannot hang page renders. */
export const SERVER_TIMEOUT_MS = 5000

/** RFC 9457 problem details returned by the API on errors. */
export interface ApiProblem {
  /** Problem type URI; set where the frontend reacts to one kind of refusal. */
  type?: string
  title?: string
  status?: number
  detail?: string
  errors?: { message?: string; location?: string; value?: unknown }[]
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly problem: ApiProblem | undefined,
    message?: string,
  ) {
    super(message ?? problem?.detail ?? problem?.title ?? `HTTP ${status}`)
    this.name = 'ApiError'
  }
}

/**
 * Sent with every state-changing request. The backend refuses cookie-authenticated
 * POST/PUT/PATCH/DELETE without it; other sites cannot add it (CSRF protection).
 */
export const CSRF_HEADER = 'X-SkinLoom-Request'

export async function apiFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const signal = init?.signal ?? (typeof window === 'undefined' ? AbortSignal.timeout(SERVER_TIMEOUT_MS) : undefined)
  const headers = new Headers(init?.headers)
  const method = (init?.method ?? 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') headers.set(CSRF_HEADER, '1')
  const res = await fetch(apiBase() + url, { cache: 'no-store', ...init, headers, signal })
  const text = res.status === 204 ? '' : await res.text()
  let body: unknown
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      // e.g. an HTML error page from a proxy in front of the backend
      throw new ApiError(res.status, undefined, `HTTP ${res.status}: response is not JSON`)
    }
  }
  if (!res.ok) throw new ApiError(res.status, body as ApiProblem | undefined)
  return { data: body, status: res.status, headers: res.headers } as T
}
