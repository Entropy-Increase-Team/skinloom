import type { Metadata } from 'next'
import { NextIntlClientProvider } from 'next-intl'
import { headers } from 'next/headers'
import { getLocale, getMessages } from 'next-intl/server'

import { LiveRefresh } from '@/components/live-refresh'
import { Providers } from '@/components/providers'
import { getBootstrap } from '@/lib/bootstrap'
import { safeImageUrl } from '@/lib/safe-url'
import { themeSettingAttrs, themeStyle } from '@/lib/theme'

// Interface fonts, self-hosted from npm and served from /_next/static (ADR 0042): Inter for
// Latin and figures, Noto Sans SC for Chinese (unicode-range slices: only the parts a page
// uses are downloaded), JetBrains Mono for hashes, UUIDs and tokens. All SIL OFL 1.1.
import '@fontsource-variable/inter'
import '@fontsource-variable/noto-sans-sc'
import '@fontsource-variable/jetbrains-mono'
import './globals.css'

export async function generateMetadata(): Promise<Metadata> {
  const boot = await getBootstrap()
  let metadataBase: URL | undefined
  try {
    metadataBase = boot.site.url ? new URL(boot.site.url) : undefined // makes og:image URLs absolute
  } catch {
    metadataBase = undefined
  }
  const favicon = safeImageUrl(boot.site.favicon)
  return {
    title: { default: boot.site.name, template: `%s · ${boot.site.name}` },
    description: boot.site.description,
    keywords: boot.site.keywords || undefined,
    generator: 'SkinLoom',
    metadataBase,
    ...(favicon ? { icons: { icon: favicon } } : {}),
  }
}

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const boot = await getBootstrap()
  const locale = await getLocale()
  const messages = await getMessages()
  // Set by src/proxy.ts with the page's Content-Security-Policy (ADR 0039).
  const nonce = (await headers()).get('x-nonce') ?? undefined
  const forcedClass = boot.theme.colorScheme === 'dark' ? 'dark' : boot.theme.colorScheme === 'light' ? 'light' : undefined
  return (
    <html lang={locale} className={forcedClass} suppressHydrationWarning data-sl-theme={boot.theme.id} {...themeSettingAttrs(boot.theme.settings)}>
      <head suppressHydrationWarning>
        <style id="sl-theme-tokens" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: themeStyle(boot.theme.tokens, boot.theme.settings) }} />
        {boot.theme.css.map((href) => (
          <link key={href} rel="stylesheet" href={href} />
        ))}
        {/* Custom JS (theme.custom_js): a file of its own, run once the page is parsed. */}
        {(boot.theme.js ?? []).map((src) => (
          <script key={src} src={src} nonce={nonce} defer />
        ))}
      </head>
      <body className="min-h-svh antialiased">
        <NextIntlClientProvider locale={locale} messages={messages}>
          {/* The home page renders its blocks itself; in the client bootstrap they would
              ride along with every page. */}
          <Providers bootstrap={{ ...boot, theme: { ...boot.theme, home: [] } }} nonce={nonce}>
            {/* Each area brings its frame: (site) the public header and footer, (app) the
                sidebar shell of the user center and the admin area (design.md §9.2). */}
            {children}
            <LiveRefresh version={boot.version} />
          </Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  )
}
