import { getRequestConfig } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'
import { type Messages, withOverrides } from '@/lib/messages'

const available = ['zh-CN', 'en'] as const
type Available = (typeof available)[number]

// The backend decides the locale (query, cookie, Accept-Language) so pages and API
// responses always agree; this config loads the matching messages with the
// administrator's wording (bootstrap.messages) laid over them.
export default getRequestConfig(async () => {
  const boot = await getBootstrap()
  const locale: Available = (available as readonly string[]).includes(boot.locale) ? (boot.locale as Available) : 'zh-CN'
  const messages = (await import(`../../messages/${locale}.json`)).default as Messages
  return {
    locale,
    // The overrides are the bootstrap locale's: laid over another language they would mix two.
    messages: locale === boot.locale ? withOverrides(messages, boot.messages) : messages,
  }
})
