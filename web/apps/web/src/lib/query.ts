import { ApiError } from '@skinloom/api-client'
import { QueryClient } from '@tanstack/react-query'

/**
 * The browser tab's query cache (ADR 0042). First-page data comes from the server
 * components as initialData, so queries start fresh; the site's own changes reach the
 * cache through the config.changed event (LiveRefresh) and mutations' invalidations,
 * not through refetching whenever the window regains focus.
 */
export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        // Retry once on network trouble or a server error; a 4xx answer will not change.
        retry: (failures, err) => failures < 1 && !(err instanceof ApiError && err.status < 500),
      },
    },
  })
}
