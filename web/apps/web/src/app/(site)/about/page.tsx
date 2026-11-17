import { ButtonLink } from '@skinloom/ui'
import { ArrowUpRight } from 'lucide-react'
import type * as React from 'react'
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

function External({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 font-medium hover:text-primary">
      {children}
      <ArrowUpRight className="size-3.5 text-faint-foreground" aria-hidden />
    </a>
  )
}

/** About the software: what it is, the version, the open-source repository, and the projects it stands on. */
export default async function AboutPage() {
  const boot = await getBootstrap()
  const t = await getTranslations('about')
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-10 px-4 py-10 sm:px-6 lg:py-16">
      <div className="flex flex-col gap-4">
        <h1 className="text-2xl font-medium">{t('title')}</h1>
        <p className="text-body text-muted-foreground">{t('intro')}</p>
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">Entropy-Increase-Team/skinloom</span>
            <dl className="flex flex-wrap gap-x-5 gap-y-1 text-caption text-muted-foreground">
              <div className="flex gap-1.5">
                <dt>{t('version')}</dt>
                <dd className="font-mono text-foreground">{boot.server.version}</dd>
              </div>
              <div className="flex gap-1.5">
                <dt>{t('license')}</dt>
                <dd>
                  <External href="https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE">MIT</External>
                </dd>
              </div>
            </dl>
          </div>
          <ButtonLink variant="outline" size="sm" className="w-fit shrink-0" render={<a href="https://github.com/Entropy-Increase-Team/skinloom" target="_blank" rel="noreferrer" />}>
            GitHub
            <ArrowUpRight aria-hidden />
          </ButtonLink>
        </div>
      </div>

      <section className="flex flex-col gap-4">
        <h2 className="text-lg font-medium">{t('thanksTitle')}</h2>
        <p className="text-sm text-muted-foreground">{t('thanksIntro')}</p>
        <ul className="flex flex-col border-t text-sm">
          {projects.map((p) => (
            <li key={p.name} className="flex flex-col gap-0.5 border-b py-3 sm:flex-row sm:gap-4">
              <span className="sm:w-44 sm:shrink-0">
                <External href={p.href}>{p.name}</External>
              </span>
              <span className="text-muted-foreground">{t(p.key)}</span>
            </li>
          ))}
          <li className="flex flex-col gap-0.5 border-b py-3 sm:flex-row sm:gap-4">
            <span className="sm:w-44 sm:shrink-0">
              <External href="https://nextjs.org">Next.js</External>
            </span>
            <span className="text-muted-foreground">Go, ent, Huma, chi, shadcn/ui, Base UI, Tailwind CSS …</span>
          </li>
        </ul>
        <div className="flex flex-col gap-1.5 rounded-xl border bg-card p-5">
          <External href="https://github.com/bs-community/blessing-skin-server">Blessing Skin Server</External>
          <p className="text-sm leading-relaxed text-muted-foreground">{t('blessing')}</p>
        </div>
      </section>
    </div>
  )
}
