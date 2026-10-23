import type { Metadata } from 'next'
import { NextIntlClientProvider } from 'next-intl'
import { getLocale, getMessages } from 'next-intl/server'

import { LiveRefresh } from '@/components/live-refresh'
import { Providers } from '@/components/providers'
import { SiteFooter } from '@/components/site-footer'
import { SiteHeader } from '@/components/site-header'
import { getBootstrap } from '@/lib/bootstrap'
import { themeStyle } from '@/lib/theme'

import './globals.css'

export async function generateMetadata(): Promise<Metadata> {
  const boot = await getBootstrap()
  return {
    title: { default: boot.site.name, template: `%s · ${boot.site.name}` },
    description: boot.site.description,
    generator: 'SkinLoom',
  }
}

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const boot = await getBootstrap()
  const locale = await getLocale()
  const messages = await getMessages()
  return (
    <html lang={locale} suppressHydrationWarning data-sl-theme={boot.theme.id}>
      <head>
        <style id="sl-theme-tokens" dangerouslySetInnerHTML={{ __html: themeStyle(boot.theme.tokens) }} />
        {boot.theme.css.map((href) => (
          <link key={href} rel="stylesheet" href={href} />
        ))}
      </head>
      <body className="min-h-screen antialiased">
        <NextIntlClientProvider locale={locale} messages={messages}>
          <Providers bootstrap={boot}>
            <div className="flex min-h-screen flex-col">
              <SiteHeader />
              <main className="flex-1">{children}</main>
              <SiteFooter />
            </div>
            <LiveRefresh version={boot.version} />
          </Providers>
        </NextIntlClientProvider>
      </body>
    </html>
  )
}
