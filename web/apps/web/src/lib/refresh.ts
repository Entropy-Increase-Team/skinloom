/** Upper bound of the random delay before an open tab refreshes after a config change. */
export const MAX_REFRESH_JITTER_MS = 1500

export function nextRefreshDelay(random: () => number = Math.random): number {
  return Math.floor(random() * MAX_REFRESH_JITTER_MS)
}
