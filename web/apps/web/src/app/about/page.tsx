import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@skinloom/ui'
import type { Metadata } from 'next'
import { getTranslations } from 'next-intl/server'

import { getBootstrap } from '@/lib/bootstrap'

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations('about')
  return { title: t('title') }
}

const projects = [
  { name: 'skinview3d', href: 'https://github.com/bs-community/skinview3d', key: 'skinview3d' },
  { name: 'authlib-injector', href: 'https://github.com/yushijinhun/authlib-injector', key: 'authlib' },
  { name: 'CustomSkinLoader', href: 'https://github.com/xfl03/MCCustomSkinLoader', key: 'csl' },
] as const

export default async function AboutPage() {
  const boot = await getBootstrap()
  const t = await getTranslations('about')
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 py-12">
      <h1 className="text-3xl font-bold tracking-tight">{t('title')}</h1>
      <p className="leading-relaxed text-muted-foreground">{t('intro')}</p>
      <p className="text-sm text-muted-foreground">
        {t('version')}: <code className="font-mono">{boot.server.version}</code>
      </p>

      <h2 className="mt-4 text-xl font-semibold">{t('thanksTitle')}</h2>
      <Card className="border-primary/40">
        <CardHeader>
          <CardTitle>
            <a href="https://github.com/bs-community/blessing-skin-server" target="_blank" rel="noreferrer" className="hover:underline">
              Blessing Skin Server
            </a>
          </CardTitle>
          <CardDescription className="leading-relaxed">{t('blessing')}</CardDescription>
        </CardHeader>
      </Card>
      <p className="text-sm text-muted-foreground">{t('thanksIntro')}</p>
      <Card>
        <CardContent>
          <ul className="flex flex-col gap-2 text-sm">
            {projects.map((p) => (
              <li key={p.name}>
                <a href={p.href} target="_blank" rel="noreferrer" className="font-medium hover:underline">
                  {p.name}
                </a>
                <span className="text-muted-foreground"> — {t(p.key)}</span>
              </li>
            ))}
            <li>
              <a href="https://nextjs.org" target="_blank" rel="noreferrer" className="font-medium hover:underline">
                Next.js
              </a>
              <span className="text-muted-foreground">, Go, ent, Huma, chi, shadcn/ui, Tailwind CSS …</span>
            </li>
          </ul>
        </CardContent>
      </Card>
      <p className="text-sm text-muted-foreground">
        {t('license')}:{' '}
        <a href="https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE" className="hover:underline">
          MIT
        </a>
      </p>
    </div>
  )
}
