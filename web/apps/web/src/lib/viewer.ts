import { getMe, type UserView } from '@skinloom/api-client'
import { headers } from 'next/headers'
import { cache } from 'react'

export type Viewer = UserView

/**
 * The signed-in user for the current request (server only), or null. The browser's
 * cookie is forwarded to the backend; deduplicated per request like getBootstrap.
 */
export const getViewer = cache(async (): Promise<Viewer | null> => {
  const cookie = (await headers()).get('cookie')
  if (!cookie?.includes('skinloom_session=')) return null
  try {
    const res = await getMe({ headers: { cookie } })
    return res.data as Viewer
  } catch {
    return null // signed out, expired, or backend unreachable: render as a guest
  }
})
