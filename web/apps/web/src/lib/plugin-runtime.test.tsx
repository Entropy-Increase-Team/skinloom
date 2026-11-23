import { readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { FORM_SCOPES, PLUGIN_SLOTS, THEME_OVERRIDE_TARGETS } from '@skinloom/plugin-sdk/catalog'
import { PluginWebProvider, useT } from '@skinloom/plugin-sdk/web'
import { NextIntlClientProvider } from 'next-intl'
import type { ReactElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { findMissingPluginFormErrors, PluginForms } from '@/components/plugin-forms'
import { findPluginPage, isPluginPageAreaAllowed } from '@/components/plugin-page-host'
import { BootstrapProvider, resolveSafeNavigateTarget } from '@/components/providers'
import { Slot } from '@/components/slot'
import { ThemeOverride } from '@/components/theme-override'
import type { SiteBootstrap } from '@/lib/bootstrap'
import { initSharedGlobals, PluginErrorBoundary, PluginRuntimeProvider, usePluginRuntime } from '@/lib/plugin-runtime'
import enMessages from '../../messages/en.json'

function collectTsxFiles(dir: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const abs = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      out.push(...collectTsxFiles(abs))
    } else if (entry.isFile() && entry.name.endsWith('.tsx')) {
      out.push(abs)
    }
  }
  return out
}

function makeBootstrap(overrides: Partial<SiteBootstrap> = {}): SiteBootstrap {
  return {
    version: 1,
    locale: 'en',
    locales: [
      { code: 'zh-CN', name: '简体中文' },
      { code: 'en', name: 'English' },
    ],
    site: {
      name: 'SkinLoom',
      description: 'Test',
      url: 'https://skinloom.example',
      announcement: '',
      announcementHtml: '',
      logo: '',
      favicon: '',
      copyrightHtml: '',
      keywords: '',
    },
    theme: {
      id: 'default',
      version: '1.0.0',
      colorScheme: 'both',
      tokens: { light: {}, dark: {} },
      css: [],
      js: [],
      layout: {},
      home: [],
    },
    menus: { explore: [], user: [], admin: [] },
    plugins: [],
    server: { name: 'SkinLoom', version: '1.0.0' },
    needsSetup: false,
    auth: {
      registration: 'open',
      captcha: { provider: 'none' },
      mail: false,
      requireVerification: false,
      social: [],
    },
    messages: {},
    ...overrides,
  }
}

describe('P2-3 frontend plugin runtime & mount coverage', () => {
  const srcRoot = path.resolve(import.meta.dirname, '..')
  const tsxContents = collectTsxFiles(srcRoot).map((f) => readFileSync(f, 'utf8'))

  it('mounts every canonical UI <Slot> from the SDK catalog (21/21)', () => {
    const found = new Set<string>()
    for (const content of tsxContents) {
      for (const match of content.matchAll(/<Slot\b[^>]*\bname="([^"]+)"/g)) {
        found.add(match[1])
      }
    }
    for (const slot of PLUGIN_SLOTS) {
      expect(found.has(slot), `Missing <Slot name="${slot}" /> in frontend`).toBe(true)
    }
  })

  it('mounts every canonical Level-5 <ThemeOverride> target (7/7)', () => {
    const found = new Set<string>()
    for (const content of tsxContents) {
      for (const match of content.matchAll(/<ThemeOverride\b[^>]*\bname="([^"]+)"/g)) {
        found.add(match[1])
      }
    }
    for (const target of THEME_OVERRIDE_TARGETS) {
      expect(found.has(target), `Missing <ThemeOverride name="${target}" /> in frontend`).toBe(true)
    }
  })

  it('mounts <PluginForms> for all declarative form scopes (5/5)', () => {
    const found = new Set<string>()
    for (const content of tsxContents) {
      for (const match of content.matchAll(/<PluginForms\b[^>]*\bscope="([^"]+)"/g)) {
        found.add(match[1])
      }
    }
    for (const scope of FORM_SCOPES) {
      expect(found.has(scope), `Missing <PluginForms scope="${scope}" /> in frontend`).toBe(true)
    }
  })

  it('populates globalThis.__SKINLOOM_SHARED__ with host singletons', () => {
    initSharedGlobals()
    const shared = (globalThis as { __SKINLOOM_SHARED__?: Record<string, unknown> }).__SKINLOOM_SHARED__
    expect(shared).toBeDefined()
    for (const id of [
      'react',
      'react-dom',
      'react-dom/client',
      'react/jsx-runtime',
      'react/jsx-dev-runtime',
      '@tanstack/react-query',
      '@skinloom/ui',
      '@skinloom/plugin-sdk/web',
    ]) {
      expect(shared![id], `Missing shared singleton "${id}"`).toBeDefined()
    }
  })

  it('renders ThemeOverride SSR fallback, Slot fallback, declarative forms, and preview bar cleanly', () => {
    const boot = makeBootstrap({
      theme: {
        id: 'brocade',
        version: '1.0.0',
        colorScheme: 'both',
        tokens: { light: {}, dark: {} },
        css: [],
        js: [],
        layout: {},
        home: [],
        previewing: true,
        previewName: 'Brocade',
        overrides: ['site.footer'],
      },
      forms: {
        'auth.register': [
          { pluginId: 'invite-code', name: 'inviteCode', type: 'text', label: 'Invite Code', required: true },
          { pluginId: 'tos-extra', name: 'acceptRules', type: 'boolean', label: 'Accept Server Rules' },
        ],
      },
    })

    const html = renderToStaticMarkup(
      <NextIntlClientProvider locale="en" timeZone="Asia/Shanghai" messages={enMessages}>
        <BootstrapProvider value={boot}>
          <PluginRuntimeProvider bootstrap={boot}>
            <ThemeOverride name="site.footer" fallback={<footer>SSR Footer Fallback</footer>} />
            <Slot name="site.header.actions">
              <span>Fallback Action</span>
            </Slot>
            <PluginForms
              scope="auth.register"
              values={{ inviteCode: 'VIP-2026', acceptRules: true }}
              onChange={() => {}}
              errors={{ inviteCode: 'Invalid code' }}
            />
          </PluginRuntimeProvider>
        </BootstrapProvider>
      </NextIntlClientProvider>,
    )

    expect(html).toContain('SSR Footer Fallback')
    expect(html).toContain('data-sl-slot="site.header.actions"')
    expect(html).toContain('Fallback Action')
    expect(html).toContain('Invite Code')
    expect(html).toContain('Invalid code')
    expect(html).toContain('Accept Server Rules')
    expect(html).toContain('Previewing theme Brocade')
  })

  it('renders empty Slot as null for empty:hidden, validates PluginForms, isolates areas/pages, and guards navigation', () => {
    const boot = makeBootstrap({
      forms: {
        'auth.register': [
          {
            pluginId: 'p1',
            name: 'token',
            type: 'text',
            label: 'Token A',
            description: 'From plugin A',
            placeholder: 'Enter A',
            required: true,
          },
          {
            pluginId: 'p2',
            name: 'token',
            type: 'text',
            label: 'Token B',
            placeholder: 'Enter B',
          },
        ],
      },
    })

    const emptySlotHtml = renderToStaticMarkup(
      <NextIntlClientProvider locale="en" timeZone="Asia/Shanghai" messages={enMessages}>
        <BootstrapProvider value={boot}>
          <PluginRuntimeProvider bootstrap={boot}>
            <Slot name="user.closet.item.actions" />
          </PluginRuntimeProvider>
        </BootstrapProvider>
      </NextIntlClientProvider>,
    )
    expect(emptySlotHtml).toBe('')

    const formHtml = renderToStaticMarkup(
      <NextIntlClientProvider locale="en" timeZone="Asia/Shanghai" messages={enMessages}>
        <BootstrapProvider value={boot}>
          <PluginRuntimeProvider bootstrap={boot}>
            <PluginForms
              scope="auth.register"
              values={{ 'p1:token': 'alpha', 'p2:token': 'beta' }}
              onChange={() => {}}
            />
          </PluginRuntimeProvider>
        </BootstrapProvider>
      </NextIntlClientProvider>,
    )
    expect(formHtml).toContain('From plugin A')
    expect(formHtml).toContain('placeholder="Enter A"')
    expect(formHtml).toContain('value="alpha"')
    expect(formHtml).toContain('value="beta"')

    const formErrors = findMissingPluginFormErrors(boot.forms, 'auth.register', { 'p2:token': 'only-b' }, 'Required')
    expect(formErrors['p1:token']).toBe('Required')
    expect(formErrors['form.p1.token']).toBe('Required')
    expect(formErrors['form.token']).toBeUndefined()

    const Home = () => null
    const AdminOnly = () => null
    expect(findPluginPage({ '/': Home }, 'demo', '/missing')).toBeUndefined()
    expect(findPluginPage({ '/sub': Home }, 'demo', '/sub')).toBe(Home)
    expect(findPluginPage({ '/sub': Home, 'admin:/sub': AdminOnly }, 'demo', '/sub', 'admin')).toBe(AdminOnly)

    const menus = {
      user: [{ id: 'plugin:demo/panel', title: 'Panel', href: '/user/p/demo/panel', icon: '', order: 1 }],
      explore: [],
      admin: [],
    }
    expect(isPluginPageAreaAllowed(menus, 'demo', 'user')).toBe(true)
    expect(isPluginPageAreaAllowed(menus, 'demo', 'user', '/panel')).toBe(true)
    expect(isPluginPageAreaAllowed(menus, 'demo', 'user', '/panel/sub')).toBe(true)
    expect(isPluginPageAreaAllowed(menus, 'demo', 'user', '/superadmin-only')).toBe(false)
    expect(isPluginPageAreaAllowed(menus, 'demo', 'explore')).toBe(false)
    expect(isPluginPageAreaAllowed(menus, 'demo', 'admin')).toBe(false)

    expect(resolveSafeNavigateTarget('/user/closet')).toEqual({ kind: 'internal', path: '/user/closet' })
    expect(resolveSafeNavigateTarget('https://skinloom.example/ok', 'https://skinloom.example')).toEqual({
      kind: 'internal',
      path: '/ok',
    })
    expect(resolveSafeNavigateTarget('https://oauth.example/auth', 'https://skinloom.example')).toEqual({
      kind: 'external',
      url: 'https://oauth.example/auth',
    })
    expect(resolveSafeNavigateTarget('javascript:alert(1)')).toBeNull()
    expect(resolveSafeNavigateTarget('data:text/html,hi')).toBeNull()
    expect(resolveSafeNavigateTarget('//evil.example')).toBeNull()
    expect(resolveSafeNavigateTarget('/\\evil.example')).toBeNull()
  })

  it('isolates crashing plugin components inside PluginErrorBoundary', () => {
    expect(PluginErrorBoundary.getDerivedStateFromError()).toEqual({ hasError: true })
    const boundary = new PluginErrorBoundary({
      pluginId: 'broken-plugin',
      fallback: <span>Safe Fallback</span>,
      children: <span>OK</span>,
    })
    boundary.state = { hasError: true }
    const html = renderToStaticMarkup(boundary.render() as ReactElement)
    expect(html).toContain('Safe Fallback')
  })

  it('provides localized theme messages to Level-5 themeRuntimeContext and renders textarea PluginForms', () => {
    const boot = makeBootstrap({
      locale: 'zh-CN',
      theme: {
        id: 'theme-atelier',
        version: '1.0.0',
        colorScheme: 'both',
        tokens: { light: {}, dark: {} },
        css: [],
        js: [],
        layout: {},
        home: [],
        overrides: ['site.footer'],
        settings: { cardStyle: 'editorial' },
      },
      plugins: [
        {
          id: 'theme-atelier',
          kind: 'theme',
          version: '1.0.0',
          name: '工坊 · Atelier',
          messages: {
            poweredBy: '由 SkinLoom 工坊主题呈现',
            likes: '{count} 次收藏',
          },
        },
      ],
      forms: {
        'texture.upload': [
          {
            pluginId: 'texture-description',
            name: 'description',
            type: 'textarea',
            label: '材质说明',
            placeholder: '支持 Markdown 格式',
            maxLength: 2000,
          },
        ],
      },
    })

    function ThemeChild() {
      const t = useT()
      return (
        <div>
          <span>{t('poweredBy')}</span>
          <span>{t('likes', { count: 42 })}</span>
        </div>
      )
    }

    function ProbeThemeI18n() {
      const rt = usePluginRuntime()
      if (!rt.themeRuntimeContext) return <span>missing</span>
      return (
        <PluginWebProvider value={rt.themeRuntimeContext}>
          <ThemeChild />
        </PluginWebProvider>
      )
    }

    const html = renderToStaticMarkup(
      <NextIntlClientProvider locale="en" timeZone="Asia/Shanghai" messages={enMessages}>
        <BootstrapProvider value={boot}>
          <PluginRuntimeProvider bootstrap={boot}>
            <ProbeThemeI18n />
            <PluginForms scope="texture.upload" values={{ description: 'Hello world' }} onChange={() => {}} />
          </PluginRuntimeProvider>
        </BootstrapProvider>
      </NextIntlClientProvider>,
    )

    expect(html).toContain('由 SkinLoom 工坊主题呈现')
    expect(html).toContain('42 次收藏')
    expect(html).toContain('<textarea')
    expect(html).toContain('maxLength="2000"')
    expect(html).toContain('Hello world')
  })
})
