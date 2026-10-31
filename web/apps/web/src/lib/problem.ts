import { ApiError } from '@skinloom/api-client'

export interface Problem {
  /** HTTP status, or 0 when the request never got an answer. */
  status: number
  detail?: string
  /** Validation messages keyed by body field, e.g. { email: 'not a valid email address' }. */
  fields: Record<string, string>
  /** The site requires a verified email address and the account has not verified its own. */
  unverified: boolean
}

/** Problem type of a refusal for want of a verified email (httpapi.ProblemEmailNotVerified). */
const EMAIL_NOT_VERIFIED = 'tag:skinloom.cn,2026:email-not-verified'

/** Turns a failed API call into something a form can show. */
export function problemOf(err: unknown): Problem {
  if (!(err instanceof ApiError)) return { status: 0, fields: {}, unverified: false }
  const fields: Record<string, string> = {}
  for (const e of err.problem?.errors ?? []) {
    const field = e.location?.startsWith('body.') ? e.location.slice(5) : undefined
    if (field && e.message && !fields[field]) fields[field] = e.message
  }
  return { status: err.status, detail: err.problem?.detail, fields, unverified: err.problem?.type === EMAIL_NOT_VERIFIED }
}
