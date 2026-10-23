import { getRequestConfig } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

const available = ['zh-CN', 'en'] as const
type Available = (typeof available)[number]

// The backend decides the locale (query, cookie, Accept-Language) so pages and API
// responses always agree; this config only loads the matching messages.
export default getRequestConfig(async () => {
  const boot = await getBootstrap()
  const locale: Available = (available as readonly string[]).includes(boot.locale) ? (boot.locale as Available) : 'zh-CN'
  return {
    locale,
    messages: (await import(`../../messages/${locale}.json`)).default,
  }
})
