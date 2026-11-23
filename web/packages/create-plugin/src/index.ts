import { existsSync, mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

export const PLUGIN_TEMPLATES = ['basic', 'page', 'auth-provider', 'block', 'theme', 'native-go'] as const
export type PluginTemplate = (typeof PLUGIN_TEMPLATES)[number]

export interface ScaffoldOptions {
  targetDir: string
  template?: PluginTemplate
  id?: string
  name?: string
  force?: boolean
}

export interface ScaffoldResult {
  id: string
  template: PluginTemplate
  targetDir: string
  files: string[]
}

const SCHEMA_URL = 'https://skinloom.cn/schema/plugin.v1.json'

/** Normalizes a folder or display name into a valid SkinLoom kebab-case plugin ID. */
export function normalizePluginId(raw: string, maxLen = 40): string {
  const cleaned = raw
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .replace(/-{2,}/g, '-')
  if (cleaned.length >= 3) {
    return cleaned.slice(0, maxLen).replace(/-+$/g, '') || 'my-plugin'
  }
  return cleaned ? `${cleaned}-plugin`.slice(0, maxLen) : 'my-plugin'
}

function toDisplayName(id: string): string {
  return id
    .split('-')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function basePackageJson(id: string, displayName: string): string {
  return `${JSON.stringify(
    {
      name: id,
      version: '0.1.0',
      description: `${displayName} SkinLoom extension`,
      private: true,
      type: 'module',
      scripts: {
        build: 'skinloom-plugin build',
        dev: 'skinloom-plugin dev --url http://127.0.0.1:8080',
        lint: 'skinloom-plugin lint',
        pack: 'skinloom-plugin pack',
        typecheck: 'tsc --noEmit',
        test: 'vitest run',
      },
      devDependencies: {
        '@skinloom/cli': '^1.0.0',
        '@skinloom/plugin-sdk': '^1.0.0',
        '@skinloom/ui': '*',
        '@tanstack/react-query': '^5.0.0',
        '@types/node': '^22.0.0',
        '@types/react': '^19.0.0',
        react: '^19.3.1',
        typescript: '^5.9.0',
        vitest: '^5.0.0',
      },
    },
    null,
    2,
  )}\n`
}

function baseTsConfig(includeJsx: boolean): string {
  return `${JSON.stringify(
    {
      compilerOptions: {
        target: 'ES2023',
        lib: includeJsx ? ['ES2023', 'DOM', 'DOM.Iterable'] : ['ES2023'],
        module: 'ESNext',
        moduleResolution: 'Bundler',
        ...(includeJsx ? { jsx: 'react-jsx' } : {}),
        strict: true,
        noEmit: true,
        skipLibCheck: true,
      },
      include: ['src'],
    },
    null,
    2,
  )}\n`
}

function baseGitignore(): string {
  return ['dist/', 'node_modules/', '*.slp', '*.key.pem', ''].join('\n')
}

function buildTemplateFiles(template: PluginTemplate, id: string, displayName: string): Record<string, string> {
  const displayLiteral = JSON.stringify(displayName)
  const common: Record<string, string> = {
    'package.json': basePackageJson(id, displayName),
    'tsconfig.json': baseTsConfig(template !== 'basic'),
    '.gitignore': baseGitignore(),
    'README.md': `# ${displayName}\n\nScaffolded with \`create-skinloom-plugin --template ${template}\`.\n\n\`\`\`bash\npnpm install\npnpm run lint\npnpm run build\npnpm run pack\n\`\`\`\n`,
  }

  switch (template) {
    case 'basic':
      return {
        ...common,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'plugin',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            description: { en: `${displayName} plugin`, 'zh-CN': `${displayName} 插件` },
            apiVersion: 1,
            engines: { skinloom: '>=1.0.0' },
            server: 'server.js',
            permissions: ['storage.kv'],
            hooks: {
              guard: ['auth.login'],
              event: ['user.registered'],
            },
            settings: {
              blockedDomains: {
                type: 'list',
                label: { en: 'Blocked Email Domains', 'zh-CN': '拦截邮箱域名' },
                default: [],
              },
            },
          },
          null,
          2,
        )}\n`,
        'src/server.ts': `import { definePlugin, reject } from '@skinloom/plugin-sdk/server'

export default definePlugin({
  async setup(ctx) {
    ctx.hooks.guard('auth.login', async (payload) => {
      const blocked = (await ctx.settings.get<string[]>('blockedDomains')) ?? []
      const domain = payload.identifier.split('@')[1]?.toLowerCase() ?? ''
      if (domain && blocked.includes(domain)) {
        return reject('Login from this email domain is not allowed.')
      }
      return
    })

    ctx.events.on('user.registered', async (payload) => {
      const count = (await ctx.kv.get<number>('stats:registrations')) ?? 0
      await ctx.kv.set('stats:registrations', count + 1)
      ctx.log.info(\`Welcome user #\${payload.userId} (\${payload.method ?? 'email'})\`)
    })
  },
})
`,
        'src/server.test.ts': `import { describe, expect, it } from 'vitest'
import { createTestHost } from '@skinloom/plugin-sdk/testing'
import plugin from './server.js'

describe('${id}', () => {
  it('blocks disallowed domains and counts registrations', async () => {
    const host = createTestHost({
      manifest: {
        id: '${id}',
        kind: 'plugin',
        version: '0.1.0',
        permissions: ['storage.kv'],
        hooks: { guard: ['auth.login'], event: ['user.registered'] },
      },
      settings: { blockedDomains: ['spam.example'] },
    })
    await host.setup(plugin)

    const blocked = await host.guard('auth.login', {
      identifier: 'alice@spam.example',
      userId: 1,
    })
    expect(blocked).toMatchObject({ ok: false, rejected: true })

    await host.emit('user.registered', {
      userId: 1,
      method: 'email',
    })
    expect(await host.ctx.kv.get<number>('stats:registrations')).toBe(1)
  })
})
`,
      }

    case 'page':
      return {
        ...common,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'plugin',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            apiVersion: 1,
            engines: { skinloom: '>=1.0.0' },
            server: 'server.js',
            web: 'web/index.js',
            permissions: ['players.read', 'textures.read'],
            menus: [
              {
                area: 'user',
                path: `/${id}`,
                title: { en: displayName, 'zh-CN': displayName },
                icon: 'sparkles',
                order: 50,
              },
            ],
            slots: ['user.dashboard.widgets'],
          },
          null,
          2,
        )}\n`,
        'locales/en.json': `${JSON.stringify({ title: displayName, playersLabel: 'Players', texturesLabel: 'Textures' }, null, 2)}\n`,
        'locales/zh-CN.json': `${JSON.stringify({ title: displayName, playersLabel: '角色数', texturesLabel: '材质数' }, null, 2)}\n`,
        'src/server.ts': `import { definePlugin } from '@skinloom/plugin-sdk/server'

export default definePlugin({
  setup(ctx) {
    ctx.http.get('/summary', async (req) => {
      if (!req.user) {
        return { status: 401, body: { detail: 'Authentication required' } }
      }
      const [players, textures] = await Promise.all([
        ctx.players.list(req.user.id),
        ctx.textures.list({ uploaderId: req.user.id, limit: 50 }),
      ])
      return {
        status: 200,
        body: {
          playerCount: players.length,
          textureCount: textures.items.length,
          hasMore: Boolean(textures.nextCursor),
        },
      }
    })
  },
})
`,
        'src/web/index.tsx': `import { Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { defineWebPlugin, usePluginApi, useT } from '@skinloom/plugin-sdk/web'

interface SummaryData {
  playerCount: number
  textureCount: number
  hasMore?: boolean
}

function PluginPage() {
  const t = useT()
  const api = usePluginApi()
  const { data } = api.useQuery<SummaryData>('/summary')

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
      </CardHeader>
      <CardContent className="grid grid-cols-2 gap-4 text-sm">
        <div>
          <div className="text-muted-foreground">{t('playersLabel')}</div>
          <div className="text-2xl font-semibold tabular-nums">{data?.playerCount ?? 0}</div>
        </div>
        <div>
          <div className="text-muted-foreground">{t('texturesLabel')}</div>
          <div className="text-2xl font-semibold tabular-nums">{data?.textureCount ?? 0}</div>
        </div>
      </CardContent>
    </Card>
  )
}

export default defineWebPlugin({
  pages: {
    '/${id}': PluginPage,
  },
  slots: {
    'user.dashboard.widgets': PluginPage,
  },
})
`,
        'src/server.test.ts': `import { describe, expect, it } from 'vitest'
import { createTestHost } from '@skinloom/plugin-sdk/testing'
import plugin from './server.js'

describe('${id}', () => {
  it('returns summary counts for signed-in user', async () => {
    const host = createTestHost({
      manifest: {
        id: '${id}',
        kind: 'plugin',
        version: '0.1.0',
        permissions: ['players.read', 'textures.read'],
      },
    })
    await host.setup(plugin)
    const res = await host.request('GET', '/summary', {
      user: { id: 1, email: 'u@example.com', nickname: 'PlayerOne', role: 'user' },
    })
    expect(res.status).toBe(200)
  })
})
`,
      }

    case 'auth-provider':
      return {
        ...common,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'plugin',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            apiVersion: 1,
            engines: { skinloom: '>=1.0.0' },
            server: 'server.js',
            web: 'web/index.js',
            permissions: ['auth.provider', 'http.fetch', 'storage.kv'],
            http: { allow: ['accounts.example.com'] },
            settings: {
              clientId: {
                type: 'string',
                label: { en: 'Client ID', 'zh-CN': '客户端 ID' },
                required: true,
              },
              clientSecret: {
                type: 'secret',
                label: { en: 'Client Secret', 'zh-CN': '客户端密钥' },
                required: true,
              },
            },
          },
          null,
          2,
        )}\n`,
        'src/server.ts': `import { definePlugin } from '@skinloom/plugin-sdk/server'

export default definePlugin({
  setup(ctx) {
    ctx.auth.registerProvider(
      {
        id: '${id}',
        name: ${displayLiteral},
        authUrl: '/api/plugins/${id}/oauth/start',
        mode: 'redirect',
      },
      async (input) => {
        const code = typeof input.code === 'string' ? input.code : ''
        const state = typeof input.state === 'string' ? input.state : ''
        if (!code || !state) {
          throw new Error('Missing OAuth code or state')
        }
        const storedNext = await ctx.kv.get<string>(\`state:\${state}\`)
        if (!storedNext) {
          throw new Error('Invalid or expired OAuth state')
        }
        await ctx.kv.delete(\`state:\${state}\`)

        const res = await ctx.fetch(\`https://accounts.example.com/userinfo?code=\${encodeURIComponent(code)}\`)
        if (!res.ok) {
          throw new Error(\`Upstream auth failed (\${res.status})\`)
        }
        const user = (await res.json()) as { sub: string; email?: string; name?: string }
        return {
          subject: user.sub,
          email: user.email,
          emailVerified: Boolean(user.email),
          nickname: user.name ?? 'Player',
          next: storedNext,
        }
      },
    )

    ctx.http.get('/oauth/start', { auth: 'public' }, async (req) => {
      const clientId = (await ctx.settings.get<string>('clientId')) ?? ''
      const state = await ctx.crypto.randomString(24)
      const rawNext = req.query.next ?? '/user'
      const safeNext = /^\\/(?![/\\\\])/.test(rawNext) ? rawNext : '/user'
      await ctx.kv.set(\`state:\${state}\`, safeNext, 600)
      const siteUrl = (await ctx.site.url()).replace(/\\/+$/, '')
      const redirectUri = \`\${siteUrl}/api/v1/auth/providers/${id}/callback\`
      const url =
        \`https://accounts.example.com/authorize?client_id=\${encodeURIComponent(clientId)}\` +
        \`&redirect_uri=\${encodeURIComponent(redirectUri)}&state=\${encodeURIComponent(state)}\`
      return { status: 302, headers: { Location: url } }
    })
  },
})
`,
        'src/web/index.tsx': `import { defineWebPlugin } from '@skinloom/plugin-sdk/web'

// SkinLoom automatically renders a sign-in button on /auth/login and /auth/register
// for redirect providers registered with authUrl. Use auth.login.providers only when
// you need bespoke UI (such as a custom credentials form or QR login widget).
export default defineWebPlugin({})
`,
        'src/server.test.ts': `import { describe, expect, it } from 'vitest'
import { createTestHost } from '@skinloom/plugin-sdk/testing'
import plugin from './server.js'

describe('${id}', () => {
  it('redirects on /oauth/start and verifies state/code through auth provider callback', async () => {
    const host = createTestHost({
      manifest: {
        id: '${id}',
        kind: 'plugin',
        version: '0.1.0',
        permissions: ['auth.provider', 'http.fetch', 'storage.kv'],
        http: { allow: ['accounts.example.com'] },
      },
      settings: { clientId: 'demo-client' },
      site: { url: 'https://skins.example.com' },
    })
    host.fetchMock.mockJson('https://accounts.example.com/userinfo', {
      sub: 'ext-42',
      email: 'player@example.com',
      name: 'Steve',
    })
    await host.setup(plugin)
    const start = await host.request('GET', '/oauth/start', { query: { next: '/user/closet' } })
    expect(start.status).toBe(302)
    const loc = new URL(String(start.headers?.Location ?? ''))
    expect(loc.origin + loc.pathname).toBe('https://accounts.example.com/authorize')
    expect(loc.searchParams.get('redirect_uri')).toBe('https://skins.example.com/api/v1/auth/providers/${id}/callback')
    const state = loc.searchParams.get('state')
    expect(state).toBeTruthy()

    const registered = host.authProviders.get('${id}') as
      | { verify?: (input: Record<string, unknown>) => Promise<Record<string, unknown>> }
      | undefined
    expect(registered?.verify).toBeTypeOf('function')
    const profile = await registered!.verify!({ code: 'abc123', state })
    expect(profile).toMatchObject({
      subject: 'ext-42',
      email: 'player@example.com',
      emailVerified: true,
      nickname: 'Steve',
      next: '/user/closet',
    })
  })
})
`,
      }

    case 'block':
      return {
        ...common,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'plugin',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            apiVersion: 1,
            engines: { skinloom: '>=1.0.0' },
            web: 'web/index.js',
            blocks: {
              spotlight: {
                name: { en: `${displayName} Spotlight`, 'zh-CN': `${displayName} 展示区块` },
                defaultProps: {
                  title: 'Featured Community Server',
                  subtitle: 'Join our unified Yggdrasil skin network.',
                },
              },
            },
          },
          null,
          2,
        )}\n`,
        'locales/en.json': `${JSON.stringify({ cta: 'Explore Textures' }, null, 2)}\n`,
        'locales/zh-CN.json': `${JSON.stringify({ cta: '浏览材质库' }, null, 2)}\n`,
        'src/web/index.tsx': `import { ButtonLink, Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { defineWebPlugin, useT } from '@skinloom/plugin-sdk/web'

function SpotlightBlock({ props }: { props: Record<string, unknown> }) {
  const t = useT()
  const title = typeof props.title === 'string' ? props.title : ${displayLiteral}
  const subtitle = typeof props.subtitle === 'string' ? props.subtitle : ''

  return (
    <section className="mx-auto max-w-5xl px-4 py-8">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-sm text-muted-foreground">{subtitle}</p>
          <ButtonLink href="/skinlib">{t('cta')}</ButtonLink>
        </CardContent>
      </Card>
    </section>
  )
}

export default defineWebPlugin({
  blocks: {
    spotlight: SpotlightBlock,
  },
})
`,
        'src/block.test.ts': `import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import path from 'node:path'

describe('${id} block manifest', () => {
  it('declares spotlight block metadata', () => {
    const raw = JSON.parse(readFileSync(path.resolve(import.meta.dirname, '../plugin.json'), 'utf8'))
    expect(raw.blocks?.spotlight).toBeDefined()
  })
})
`,
      }

    case 'theme':
      return {
        ...common,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'theme',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            apiVersion: 1,
            extends: 'default',
            engines: { skinloom: '>=1.0.0' },
            web: 'web/index.js',
            theme: {
              colorScheme: 'both',
              tokens: {
                light: {
                  primary: '#0e7490',
                  'primary-foreground': '#ffffff',
                  accent: '#ecfeff',
                },
                dark: {
                  primary: '#22d3ee',
                  'primary-foreground': '#083344',
                },
                fonts: {
                  sans: 'Inter, system-ui, sans-serif',
                },
              },
              layout: {
                auth: 'card',
                skinlib: 'comfortable',
                user: 'sidebar',
                texture: 'split',
              },
              overrides: ['site.footer'],
            },
          },
          null,
          2,
        )}\n`,
        'src/web/index.tsx': `import { defineWebPlugin, useSite } from '@skinloom/plugin-sdk/web'

function CustomFooter() {
  const site = useSite()
  return (
    <footer className="border-t border-border bg-card py-6 text-center text-sm text-muted-foreground">
      <div className="mx-auto max-w-6xl px-4">
        <span>{site.name}</span>
        <span className="mx-2">·</span>
        <span>Theme: {${displayLiteral}}</span>
      </div>
    </footer>
  )
}

export default defineWebPlugin({
  overrides: {
    'site.footer': CustomFooter,
  },
})
`,
        'src/theme.test.ts': `import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import path from 'node:path'

describe('${id} theme manifest', () => {
  it('declares Level 5 site.footer override', () => {
    const raw = JSON.parse(readFileSync(path.resolve(import.meta.dirname, '../plugin.json'), 'utf8'))
    expect(raw.kind).toBe('theme')
    expect(raw.theme?.overrides).toContain('site.footer')
  })
})
`,
      }

    case 'native-go':
      return {
        ...common,
        'package.json': `${JSON.stringify(
          {
            name: id,
            version: '0.1.0',
            description: `${displayName} SkinLoom Tier 2 native Go plugin`,
            private: true,
            type: 'module',
            scripts: {
              build: 'skinloom-plugin build --native',
              'build:all':
                'skinloom-plugin build --native --targets linux/amd64,linux/arm64,windows/amd64,darwin/arm64,darwin/amd64',
              dev: 'skinloom-plugin dev --url http://127.0.0.1:8080',
              lint: 'skinloom-plugin lint',
              pack: 'skinloom-plugin pack --split-platforms',
              'pack:fat': 'skinloom-plugin pack --fat',
              typecheck: 'tsc --noEmit',
              test: 'vitest run',
              'test:go': 'go test -v ./...',
            },
            devDependencies: {
              '@skinloom/cli': '^1.0.0',
              '@skinloom/plugin-sdk': '^1.0.0',
              '@skinloom/ui': '*',
              '@tanstack/react-query': '^5.0.0',
              '@types/node': '^22.0.0',
              '@types/react': '^19.0.0',
              react: '^19.3.1',
              typescript: '^5.9.0',
              vitest: '^5.0.0',
            },
          },
          null,
          2,
        )}\n`,
        'README.md': `# ${displayName}\n\nTier 2 SkinLoom Native Go Plugin scaffolded with \`create-skinloom-plugin --template native-go\`.\n\n## Development\n\n\`\`\`bash\n# Run Go unit tests with the in-memory mock host\ngo test -v ./...\n\n# Install web tooling & build for current OS/Arch\npnpm install\npnpm run lint\npnpm run build\n\n# Cross-compile & emit split platform .slp packages + artifacts.json\npnpm run build:all\npnpm run pack\n\`\`\`\n`,
        'plugin.json': `${JSON.stringify(
          {
            $schema: SCHEMA_URL,
            id,
            kind: 'plugin',
            runtime: 'native',
            version: '0.1.0',
            name: { en: displayName, 'zh-CN': displayName },
            description: {
              en: `${displayName} native Go plugin with isolated SQL storage and streaming HTTP proxy`,
              'zh-CN': `${displayName} 原生 Go 插件（含隔离 SQL 数据库与流式反代示例）`,
            },
            apiVersion: 1,
            engines: { skinloom: '>=1.0.0', go: '>=1.26' },
            web: 'web/index.js',
            native: {
              protocolVersion: 1,
              executable: `bin/${id}`,
              platforms: ['linux/amd64', 'linux/arm64', 'windows/amd64', 'darwin/arm64', 'darwin/amd64'],
              httpProxy: true,
              database: { required: true },
            },
            permissions: ['storage.sql', 'http.stream', 'users.read'],
            menus: [
              {
                area: 'user',
                path: `/${id}`,
                title: { en: displayName, 'zh-CN': displayName },
                icon: 'cpu',
                order: 60,
              },
            ],
            slots: ['user.dashboard.widgets'],
          },
          null,
          2,
        )}\n`,
        'go.mod': `module ${id}\n\ngo 1.26.10\n\nrequire github.com/Entropy-Increase-Team/skinloom v0.1.0\n`,
        'migrations/001_init.sql': `-- +goose Up
CREATE TABLE IF NOT EXISTS {{prefix}}entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id BIGINT NOT NULL,
  message TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS {{prefix}}entries;
`,
        'migrations/sqlite/001_init.sql': `-- +goose Up
CREATE TABLE IF NOT EXISTS {{prefix}}entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id BIGINT NOT NULL,
  message TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS {{prefix}}entries;
`,
        'migrations/mysql/001_init.sql': `-- +goose Up
CREATE TABLE IF NOT EXISTS {{prefix}}entries (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  user_id BIGINT NOT NULL,
  message TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS {{prefix}}entries;
`,
        'migrations/postgres/001_init.sql': `-- +goose Up
CREATE TABLE IF NOT EXISTS {{prefix}}entries (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  message TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS {{prefix}}entries;
`,
        'main.go': `package main

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"strings"

	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
)

//go:embed migrations
var migrationsFS embed.FS

type NativePlugin struct{}

func (p *NativePlugin) Setup(ctx *plugin.Context) error {
	db := ctx.DB()
	if db != nil {
		if err := db.Migrate(context.Background(), migrationsFS, "migrations"); err != nil {
			return fmt.Errorf("migrate isolated db: %w", err)
		}
	}

	ctx.HTTP.Get("/entries", func(reqCtx context.Context, _ *plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		type entry struct {
			ID      int64  \`json:"id"\`
			UserID  int64  \`json:"userId"\`
			Message string \`json:"message"\`
		}
		items := []entry{}
		if db != nil {
			rows, err := db.Query(reqCtx, \`SELECT id, user_id, message FROM {{prefix}}entries ORDER BY id DESC LIMIT 20\`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			for rows.Next() {
				var it entry
				if err := rows.Scan(&it.ID, &it.UserID, &it.Message); err != nil {
					return nil, err
				}
				items = append(items, it)
			}
		}
		return plugin.JSON(http.StatusOK, map[string]any{"items": items}), nil
	}, plugin.WithAuth(plugin.AuthPublic))

	ctx.HTTP.Post("/entries", func(reqCtx context.Context, req *plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		if req.User == nil {
			return nil, plugin.Reject("authentication required")
		}
		var body struct {
			Message string \`json:"message"\`
		}
		if err := req.DecodeJSON(&body); err != nil {
			return nil, plugin.Reject("invalid JSON payload")
		}
		msg := strings.TrimSpace(body.Message)
		if msg == "" {
			return nil, plugin.Reject("message is required")
		}
		if db != nil {
			if _, err := db.Exec(reqCtx, \`INSERT INTO {{prefix}}entries (user_id, message) VALUES (?, ?)\`, req.User.UserID, msg); err != nil {
				return nil, err
			}
		}
		return plugin.JSON(http.StatusCreated, map[string]any{"ok": true, "message": msg}), nil
	}, plugin.WithAuth(plugin.AuthUser))

	ctx.HTTP.StreamFunc(http.MethodGet, "/stream/events", func(w http.ResponseWriter, r *http.Request) {
		viewer := plugin.ProxyViewer(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = fmt.Fprintf(w, "data: ready (user=%d)\\n\\n", viewer.UserID)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}, plugin.WithAuth(plugin.AuthPublic))

	return nil
}

func main() {
	plugin.Serve(&NativePlugin{})
}
`,
        'main_test.go': `package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
	"github.com/Entropy-Increase-Team/skinloom/sdk/go/plugintest"
)

func TestNativePluginEntriesAndStream(t *testing.T) {
	ctx := context.Background()
	h := plugintest.New(t, &NativePlugin{},
		plugintest.WithPluginID("${id}"),
		plugintest.WithSQL(),
	)

	postResp, err := h.DoHTTP(ctx, &plugin.HTTPRequest{
		Method: http.MethodPost,
		Path:   "/entries",
		Body:   []byte(\`{"message":"hello native"}\`),
		User:   &plugin.Viewer{Authenticated: true, UserID: 1, Role: "user"},
	})
	if err != nil || postResp.Status != http.StatusCreated {
		t.Fatalf("POST /entries: resp=%+v err=%v", postResp, err)
	}

	listResp, err := h.DoHTTP(ctx, &plugin.HTTPRequest{
		Method: http.MethodGet,
		Path:   "/entries",
	})
	if err != nil || listResp.Status != http.StatusOK {
		t.Fatalf("GET /entries: resp=%+v err=%v", listResp, err)
	}
	var out struct {
		Items []struct {
			Message string \`json:"message"\`
		} \`json:"items"\`
	}
	if err := json.Unmarshal(listResp.RawBody, &out); err != nil {
		t.Fatalf("decode /entries: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Message != "hello native" {
		t.Fatalf("unexpected entries: %+v", out.Items)
	}

	req := httptest.NewRequest(http.MethodGet, "/stream/events", nil)
	req.Header.Set("X-SkinLoom-User-ID", "1")
	rec := h.DoStreamHTTP(req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "data: ready (user=1)") {
		t.Fatalf("GET /stream/events: code=%d body=%q", rec.Code, rec.Body.String())
	}
}
`,
        'locales/en.json': `${JSON.stringify({ title: displayName, empty: 'No entries yet.' }, null, 2)}\n`,
        'locales/zh-CN.json': `${JSON.stringify({ title: displayName, empty: '暂无记录。' }, null, 2)}\n`,
        'src/web/index.tsx': `import { Card, CardContent, CardHeader, CardTitle } from '@skinloom/ui'
import { defineWebPlugin, usePluginApi, useT } from '@skinloom/plugin-sdk/web'

interface EntryItem {
  id: number
  userId: number
  message: string
}

function NativePluginPage() {
  const t = useT()
  const api = usePluginApi()
  const { data } = api.useQuery<{ items: EntryItem[] }>('/entries')
  const items = data?.items ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {items.length === 0 ? (
          <p className="text-muted-foreground">{t('empty')}</p>
        ) : (
          items.map((item) => (
            <div key={item.id} className="rounded-md border border-border px-3 py-2">
              {item.message}
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}

export default defineWebPlugin({
  pages: {
    '/${id}': NativePluginPage,
  },
  slots: {
    'user.dashboard.widgets': NativePluginPage,
  },
})
`,
        'src/web/manifest.test.ts': `import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import path from 'node:path'

describe('${id} native manifest', () => {
  it('declares Tier 2 native Go runtime with SQL and HTTP stream capabilities', () => {
    const raw = JSON.parse(readFileSync(path.resolve(import.meta.dirname, '../../plugin.json'), 'utf8'))
    expect(raw.runtime).toBe('native')
    expect(raw.native?.protocolVersion).toBe(1)
    expect(raw.permissions).toContain('storage.sql')
    expect(raw.permissions).toContain('http.stream')
  })
})
`,
      }
  }
}

/** Creates a complete SkinLoom plugin or theme starter directory from one of the 5 templates. */
export function scaffoldPlugin(options: ScaffoldOptions): ScaffoldResult {
  const template: PluginTemplate = options.template ?? 'basic'
  if (!PLUGIN_TEMPLATES.includes(template)) {
    throw new Error(`Unknown template "${template}" (expected one of: ${PLUGIN_TEMPLATES.join(', ')})`)
  }
  const targetDir = path.resolve(options.targetDir)
  const manifestPath = path.join(targetDir, 'plugin.json')
  if (existsSync(manifestPath) && !options.force) {
    throw new Error(`Refusing to overwrite existing plugin.json in ${targetDir} (pass --force to overwrite)`)
  }

  const baseName = path.basename(targetDir)
  let id = normalizePluginId(options.id ?? baseName, template === 'theme' ? 32 : 40)
  if (template === 'theme' && (id === 'default' || id === 'brocade' || id === 'craft' || id === '_custom')) {
    id = normalizePluginId(`${id}-theme`, 32)
  }
  const displayName = options.name ?? toDisplayName(id)
  const filesMap = buildTemplateFiles(template, id, displayName)
  const written: string[] = []

  for (const [rel, content] of Object.entries(filesMap)) {
    const abs = path.join(targetDir, ...rel.split('/'))
    mkdirSync(path.dirname(abs), { recursive: true })
    writeFileSync(abs, content, 'utf8')
    written.push(rel)
  }

  return {
    id,
    template,
    targetDir,
    files: written.sort(),
  }
}
