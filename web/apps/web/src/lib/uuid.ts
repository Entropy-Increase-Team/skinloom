/** 32 hex digits (as the API sends UUIDs) in the usual 8-4-4-4-12 form. */
export function dashedUUID(u: string): string {
  return /^[0-9a-f]{32}$/i.test(u) ? `${u.slice(0, 8)}-${u.slice(8, 12)}-${u.slice(12, 16)}-${u.slice(16, 20)}-${u.slice(20)}` : u
}
