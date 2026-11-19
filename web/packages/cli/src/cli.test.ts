import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { afterEach, describe, expect, it } from 'vitest'
import {
  buildPlugin,
  devPlugin,
  generateKeyPair,
  lintPlugin,
  packPlugin,
  pushDevReload,
  runCli,
  signPackage,
  verifyPackage,
} from './index.js'
import { readZip, writeZip } from './zip.js'

const tempDirs: string[] = []

function makeTempDir(prefix: string): string {
  const dir = mkdtempSync(path.join(os.tmpdir(), prefix))
  tempDirs.push(dir)
  return dir
}

afterEach(() => {
  while (tempDirs.length > 0) {
    const dir = tempDirs.pop()!
    rmSync(dir, { recursive: true, force: true })
  }
})

describe('@skinloom/cli', () => {
  it('builds server.js and web/index.js with host external shims, assets, and locales', async () => {
    const dir = makeTempDir('sl-cli-build-')
    mkdirSync(path.join(dir, 'src', 'web'), { recursive: true })
    mkdirSync(path.join(dir, 'locales'), { recursive: true })
    mkdirSync(path.join(dir, 'assets'), { recursive: true })

    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify(
        {
          id: 'hello-sample',
          kind: 'plugin',
          version: '1.0.0',
          name: { en: 'Hello Sample' },
          apiVersion: 1,
          server: 'server.js',
          web: 'web/index.js',
          permissions: ['storage.kv'],
          hooks: { guard: ['auth.register'] },
          slots: ['site.footer'],
        },
        null,
        2,
      ),
    )

    writeFileSync(
      path.join(dir, 'src', 'server.ts'),
      `export default function register(ctx: any) {
        ctx.hooks.guard('auth.register', async (p: any) => {
          await ctx.kv.set('last', p.email)
          return null
        })
      }`,
    )

    writeFileSync(
      path.join(dir, 'src', 'web', 'index.tsx'),
      `import { useState } from 'react'
       import { Button } from '@skinloom/ui'
       export default function FooterWidget() {
         const [n] = useState(42)
         return <Button>{String(n)}</Button>
       }`,
    )

    writeFileSync(path.join(dir, 'locales', 'en.json'), JSON.stringify({ title: 'Hello' }))
    writeFileSync(path.join(dir, 'assets', 'badge.txt'), 'svg-placeholder')

    const built = await buildPlugin({ dir })
    expect(built.serverBytes).toBeGreaterThan(0)
    expect(built.webBytes).toBeGreaterThan(0)
    expect(built.copiedAssets).toBe(1)
    expect(built.copiedLocales).toBe(1)

    const webJs = readFileSync(built.webFile!, 'utf8')
    expect(webJs).toContain('__SKINLOOM_SHARED__')

    // Execute bundled web module against mock host shared dependencies to prove named exports work.
    ;(globalThis as Record<string, unknown>).__SKINLOOM_SHARED__ = {
      react: { useState: (v: unknown) => [v, () => {}] },
      'react/jsx-runtime': {
        jsx: (type: unknown, props: Record<string, unknown>) => ({ type, props }),
        jsxs: (type: unknown, props: Record<string, unknown>) => ({ type, props }),
      },
      '@skinloom/ui': { Button: 'MockButton' },
    }
    try {
      const mod = (await import(pathToFileURL(built.webFile!).href)) as { default: () => unknown }
      const rendered = mod.default() as { type: unknown; props: { children: string } }
      expect(rendered.type).toBe('MockButton')
      expect(rendered.props.children).toBe('42')
    } finally {
      delete (globalThis as Record<string, unknown>).__SKINLOOM_SHARED__
    }

    const lint = lintPlugin(dir)
    expect(lint.ok).toBe(true)
  })

  it('rejects Node built-in imports in server bundle during build and lint', async () => {
    const dir = makeTempDir('sl-cli-ban-')
    mkdirSync(path.join(dir, 'src'), { recursive: true })
    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify({
        id: 'bad-node-plugin',
        kind: 'plugin',
        version: '0.1.0',
        name: 'Bad Plugin',
        apiVersion: 1,
        server: 'server.js',
      }),
    )
    writeFileSync(
      path.join(dir, 'src', 'server.ts'),
      `import fs from 'node:fs'
       export default function () { return fs.existsSync('.') }`,
    )

    await expect(buildPlugin({ dir })).rejects.toThrow(/Node built-in|QJS|node:fs/i)

    const lint = lintPlugin(dir)
    expect(lint.ok).toBe(false)
    expect(lint.errors.some((e) => e.code === 'source.forbiddenImport')).toBe(true)
  })

  it('detects missing permissions, undeclared hooks, and over-quota collection indexes in lint', () => {
    const dir = makeTempDir('sl-cli-lint-')
    mkdirSync(path.join(dir, 'src'), { recursive: true })
    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify({
        id: 'lint-mismatch',
        kind: 'plugin',
        version: '0.1.0',
        name: 'Lint Mismatch',
        apiVersion: 1,
        server: 'server.js',
        permissions: [],
        hooks: { guard: [] },
        storage: {
          collections: {
            items: {
              indexes: {
                a: 'string:unique',
                b: 'string:unique',
                c: 'string:unique',
              },
            },
          },
        },
      }),
    )
    writeFileSync(
      path.join(dir, 'src', 'server.ts'),
      `export default function (ctx: any) {
        ctx.hooks.guard('auth.login', async () => {
          await ctx.fetch('https://example.com')
        })
      }`,
    )

    const lint = lintPlugin(dir)
    expect(lint.ok).toBe(false)
    const codes = new Set(lint.errors.map((e) => e.code))
    expect(codes.has('manifest.storage.quota')).toBe(true)
    expect(codes.has('source.missingPermission')).toBe(true)
    expect(codes.has('source.undeclaredHook')).toBe(true)
  })

  it('packs, signs, verifies Ed25519 .slp packages, and detects tampered bytes', async () => {
    const dir = makeTempDir('sl-cli-pack-')
    mkdirSync(path.join(dir, 'src'), { recursive: true })
    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify({
        id: 'signed-plugin',
        kind: 'plugin',
        version: '1.2.3',
        name: 'Signed Plugin',
        apiVersion: 1,
        server: 'server.js',
      }),
    )
    writeFileSync(path.join(dir, 'src', 'server.ts'), `export default function () {}`)

    const keysDir = path.join(dir, 'keys')
    const keys = generateKeyPair(keysDir, 'release')

    const packed = await packPlugin({ dir, privateKeyFile: keys.privateKeyPath })
    expect(packed.signed).toBe(true)
    expect(packed.checksums).toContain('plugin.json')
    expect(packed.checksums).toContain('server.js')

    const verified = verifyPackage({ archivePath: packed.archivePath, publicKeyFile: keys.publicKeyPath })
    expect(verified.ok).toBe(true)
    expect(verified.signed).toBe(true)

    // Re-sign under another key and verify failure with old key, success with new key.
    const rotated = generateKeyPair()
    signPackage({ archivePath: packed.archivePath, privateKeyPem: rotated.privateKeyPem })
    const oldVerify = verifyPackage({ archivePath: packed.archivePath, publicKeyPem: keys.publicKeyPem })
    expect(oldVerify.ok).toBe(false)
    const newVerify = verifyPackage({ archivePath: packed.archivePath, publicKeyPem: rotated.publicKeyPem })
    expect(newVerify.ok).toBe(true)

    // Tamper with plugin.json inside the zip and confirm checksum failure.
    const entries = readZip(readFileSync(packed.archivePath))
    const tampered = entries.map((e) =>
      e.name === 'server.js' ? { name: e.name, data: Buffer.from('// tampered') } : { name: e.name, data: e.data },
    )
    const tamperedPath = path.join(dir, 'tampered.slp')
    writeFileSync(tamperedPath, writeZip(tampered))
    const tamperVerify = verifyPackage({ archivePath: tamperedPath, publicKeyPem: rotated.publicKeyPem })
    expect(tamperVerify.ok).toBe(false)
    expect(tamperVerify.errors.some((err) => err.includes('Checksum mismatch'))).toBe(true)
  })

  it('pushes manifest, serverJs, and base64 static files to admin dev-reload', async () => {
    const dir = makeTempDir('sl-cli-dev-')
    mkdirSync(path.join(dir, 'src', 'web'), { recursive: true })
    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify({
        id: 'dev-demo',
        kind: 'plugin',
        version: '0.0.1',
        name: 'Dev Demo',
        apiVersion: 1,
        server: 'server.js',
        web: 'web/index.js',
      }),
    )
    writeFileSync(path.join(dir, 'src', 'server.ts'), `export default function () {}`)
    writeFileSync(path.join(dir, 'src', 'web', 'index.tsx'), `export default function Widget() { return null }`)

    const built = await buildPlugin({ dir })
    let capturedUrl = ''
    let capturedAuth = ''
    let capturedBody: { manifest?: { id?: string }; serverJs?: string; files?: Record<string, string> } = {}

    const result = await pushDevReload({
      url: 'http://127.0.0.1:8080/',
      token: 'secret-token',
      distDir: built.outDir,
      fetchImpl: (async (input, init) => {
        capturedUrl = String(input)
        capturedAuth = String((init?.headers as Record<string, string>)['Authorization'] ?? '')
        capturedBody = JSON.parse(String(init?.body ?? '{}'))
        return new Response(JSON.stringify({ id: 'dev-demo', enabled: true }), { status: 200 })
      }) as typeof fetch,
    })

    expect(capturedUrl).toBe('http://127.0.0.1:8080/api/v1/admin/plugins/dev-reload')
    expect(capturedAuth).toBe('Bearer secret-token')
    expect(capturedBody.manifest?.id).toBe('dev-demo')
    expect(capturedBody.serverJs).toContain('function')
    expect(Object.keys(capturedBody.files ?? {})).toContain('web/index.js')
    expect(result).toEqual({ id: 'dev-demo', enabled: true })
  })

  it('rejects duplicate, traversal, case-colliding, and corrupted zip/checksum/signature archives', () => {
    expect(() =>
      writeZip([
        { name: 'dup.txt', data: Buffer.from('a') },
        { name: 'dup.txt', data: Buffer.from('b') },
      ]),
    ).toThrow(/duplicate/i)

    const buildBadNamedZip = (entryName: string, extraName?: string): Buffer => {
      const makeEntry = (name: string) => {
        const nameBuf = Buffer.from(name, 'utf8')
        const data = Buffer.from('x')
        const local = Buffer.alloc(30 + nameBuf.length + data.length)
        local.writeUInt32LE(0x04034b50, 0)
        local.writeUInt16LE(nameBuf.length, 26)
        nameBuf.copy(local, 30)
        data.copy(local, 30 + nameBuf.length)
        return { nameBuf, data, local }
      }
      const items = [makeEntry(entryName), ...(extraName ? [makeEntry(extraName)] : [])]
      let offset = 0
      const locals: Buffer[] = []
      const cds: Buffer[] = []
      for (const it of items) {
        const cd = Buffer.alloc(46 + it.nameBuf.length)
        cd.writeUInt32LE(0x02014b50, 0)
        cd.writeUInt32LE(it.data.length, 20)
        cd.writeUInt32LE(it.data.length, 24)
        cd.writeUInt16LE(it.nameBuf.length, 28)
        cd.writeUInt32LE(offset, 42)
        it.nameBuf.copy(cd, 46)
        locals.push(it.local)
        cds.push(cd)
        offset += it.local.length
      }
      const cdBuf = Buffer.concat(cds)
      const eocd = Buffer.alloc(22)
      eocd.writeUInt32LE(0x06054b50, 0)
      eocd.writeUInt16LE(items.length, 8)
      eocd.writeUInt16LE(items.length, 10)
      eocd.writeUInt32LE(cdBuf.length, 12)
      eocd.writeUInt32LE(offset, 16)
      return Buffer.concat([...locals, cdBuf, eocd])
    }

    expect(() => readZip(buildBadNamedZip('../escape.txt'))).toThrow()
    expect(() => readZip(buildBadNamedZip('foo/..'))).toThrow()
    expect(() => readZip(buildBadNamedZip('dir\\file.txt'))).toThrow()
    expect(() => readZip(buildBadNamedZip('Case.txt', 'case.txt'))).toThrow(/case|duplicate/i)

    // generateKeyPair refuses overwriting existing keys unless force=true
    const keyDir = makeTempDir('sl-cli-keygen-force-')
    generateKeyPair(keyDir, 'testkey')
    expect(() => generateKeyPair(keyDir, 'testkey')).toThrow(/already exists/i)
    expect(() => generateKeyPair(keyDir, 'testkey', true)).not.toThrow()

    const dir = makeTempDir('sl-cli-verify-')
    const badZipPath = path.join(dir, 'bad-checksum.slp')
    const mf = Buffer.from(
      JSON.stringify({ id: 'bad-pkg', kind: 'plugin', version: '1.0.0', name: 'Bad', apiVersion: 1 }),
    )
    const dupChecksums = Buffer.from(`${'a'.repeat(64)}  plugin.json\n${'a'.repeat(64)}  plugin.json\n`)
    writeFileSync(
      badZipPath,
      writeZip([
        { name: 'plugin.json', data: mf },
        { name: 'checksums.txt', data: dupChecksums },
        { name: 'signature.sig', data: Buffer.from('***not-base64***') },
      ]),
    )
    const badRes = verifyPackage({ archivePath: badZipPath, publicKeyPem: 'not-a-valid-pem' })
    expect(badRes.ok).toBe(false)
    expect(badRes.errors.length).toBeGreaterThanOrEqual(2)
  })

  it('enforces build/lint security guards, boolean CLI flags, and devPlugin lifecycle', async () => {
    expect(await runCli(['verify', '--pub'])).toBe(1)
    const dir = makeTempDir('sl-cli-guards-')
    mkdirSync(path.join(dir, 'src', 'web'), { recursive: true })
    writeFileSync(
      path.join(dir, 'plugin.json'),
      JSON.stringify({
        id: 'guard-test',
        kind: 'plugin',
        version: '1.0.0',
        name: 'Guard Test',
        apiVersion: 1,
        server: 'server.js',
        web: 'web/index.js',
        routes: [{ method: 'GET', path: '/ping', auth: 'public' }],
      }),
    )
    writeFileSync(path.join(dir, 'src', 'server.ts'), `export default { setup() {} }\n`)
    writeFileSync(path.join(dir, 'src', 'web', 'index.tsx'), `export default {}\n`)

    // routes.auth='public' is allowed in lintPlugin
    expect(lintPlugin(dir).ok).toBe(true)

    // --out . must fail rather than wiping source directory
    await expect(buildPlugin({ dir, outDir: '.' })).rejects.toThrow()

    // bun: import in web bundle is forbidden
    writeFileSync(path.join(dir, 'src', 'web', 'index.tsx'), `import 'bun:sqlite'; export default {}\n`)
    await expect(buildPlugin({ dir })).rejects.toThrow(/bun:/i)
    writeFileSync(path.join(dir, 'src', 'web', 'index.tsx'), `export default {}\n`)

    // @skinloom/ui import in server bundle triggers lint error and build error
    writeFileSync(path.join(dir, 'src', 'server.ts'), `import { Button } from '@skinloom/ui'; export default { setup() { void Button } }\n`)
    expect(lintPlugin(dir).ok).toBe(false)
    await expect(buildPlugin({ dir })).rejects.toThrow(/@skinloom\/ui/i)
    writeFileSync(path.join(dir, 'src', 'server.ts'), `export default { setup() {} }\n`)

    // runCli keeps positional dir after boolean --json and reports unsigned verify accurately
    let outText = ''
    const origWrite = process.stdout.write.bind(process.stdout)
    process.stdout.write = ((chunk: string | Uint8Array) => {
      outText += typeof chunk === 'string' ? chunk : Buffer.from(chunk).toString('utf8')
      return true
    }) as typeof process.stdout.write
    try {
      const lintCode = await runCli(['lint', '--json', dir])
      expect(lintCode).toBe(0)
      expect(JSON.parse(outText).ok).toBe(true)

      const keys = generateKeyPair(makeTempDir('sl-cli-keys-'), 'k')
      const packed = await packPlugin({ dir, privateKeyFile: keys.privateKeyPath })
      outText = ''
      const verifyCode = await runCli(['verify', packed.archivePath])
      expect(verifyCode).toBe(0)
      expect(outText).toContain('unverified')
      expect(outText).not.toContain('signature valid')
      outText = ''
      const verifiedWithPub = await runCli(['verify', '--pub', keys.publicKeyPath!, packed.archivePath])
      expect(verifiedWithPub).toBe(0)
      expect(outText).toContain('signature valid')
    } finally {
      process.stdout.write = origWrite
    }

    // devPlugin initial reload + close()
    const reloads: unknown[] = []
    const session = await devPlugin({
      dir,
      url: 'http://127.0.0.1:8080',
      watch: false,
      onReload: (info) => reloads.push(info),
      fetchImpl: (async () => new Response(JSON.stringify({ id: 'guard-test', enabled: true }), { status: 200 })) as typeof fetch,
    })
    expect(reloads).toHaveLength(1)
    await session.close()

    // Reserved _custom theme ID and invalid event names fail lint
    const themeDir = makeTempDir('sl-cli-theme-')
    writeFileSync(path.join(themeDir, 'theme.css'), ':root {}\n')
    writeFileSync(
      path.join(themeDir, 'plugin.json'),
      JSON.stringify({
        id: '_custom',
        kind: 'theme',
        version: '1.0.0',
        name: 'Bad Theme',
        apiVersion: 1,
        theme: { css: 'theme.css', preview: [] },
        hooks: { event: ['plugin:.'] },
      }),
    )
    const badThemeLint = lintPlugin(themeDir)
    expect(badThemeLint.ok).toBe(false)
    expect(badThemeLint.errors.some((e) => e.message.includes('_custom') || e.message.includes('id'))).toBe(true)
  })

  it('lints, cross-compiles, split-packs, fat-packs, and dev-reloads Tier 2 native Go plugins', { timeout: 30000 }, async () => {
    // 1. JS plugin declaring native-only permissions fails lint.
    const badJsDir = makeTempDir('sl-cli-native-perm-')
    writeFileSync(
      path.join(badJsDir, 'plugin.json'),
      JSON.stringify({
        id: 'js-with-sql',
        kind: 'plugin',
        runtime: 'js',
        version: '1.0.0',
        name: 'Bad JS',
        apiVersion: 1,
        permissions: ['storage.sql'],
      }),
    )
    const badJsLint = lintPlugin(badJsDir)
    expect(badJsLint.ok).toBe(false)
    expect(badJsLint.errors.some((e) => e.code === 'manifest.permission.nativeOnly')).toBe(true)

    // 2. Native plugin with httpProxy/database missing permissions fails lint.
    const badNativeDir = makeTempDir('sl-cli-native-bad-')
    writeFileSync(
      path.join(badNativeDir, 'plugin.json'),
      JSON.stringify({
        id: 'native-bad',
        kind: 'plugin',
        runtime: 'native',
        version: '1.0.0',
        name: 'Bad Native',
        apiVersion: 1,
        native: {
          protocolVersion: 1,
          executable: 'bin/native-bad',
          platforms: ['linux/amd64'],
          httpProxy: true,
          database: { required: true },
        },
        permissions: [],
      }),
    )
    const badNativeLint = lintPlugin(badNativeDir)
    expect(badNativeLint.ok).toBe(false)
    const badCodes = new Set(badNativeLint.errors.map((e) => e.code))
    expect(badCodes.has('manifest.native.httpProxy')).toBe(true)
    expect(badCodes.has('manifest.native.database')).toBe(true)

    // 3. Valid native Go plugin + React web entry cross-builds, split-packs, fat-packs, signs & verifies.
    const nativeDir = makeTempDir('sl-cli-native-ok-')
    mkdirSync(path.join(nativeDir, 'src', 'web'), { recursive: true })
    mkdirSync(path.join(nativeDir, 'locales'), { recursive: true })
    writeFileSync(
      path.join(nativeDir, 'plugin.json'),
      JSON.stringify(
        {
          id: 'native-cli-demo',
          kind: 'plugin',
          runtime: 'native',
          version: '1.0.0',
          name: { en: 'Native CLI Demo' },
          apiVersion: 1,
          engines: { skinloom: '>=1.0.0', go: '>=1.26' },
          web: 'web/index.js',
          native: {
            protocolVersion: 1,
            executable: 'bin/native-cli-demo',
            platforms: ['linux/amd64', 'windows/amd64'],
            httpProxy: true,
            database: { required: true },
          },
          permissions: ['storage.sql', 'http.stream'],
        },
        null,
        2,
      ),
    )
    writeFileSync(path.join(nativeDir, 'go.mod'), 'module native-cli-demo\n\ngo 1.26\n')
    writeFileSync(path.join(nativeDir, 'main.go'), 'package main\n\nfunc main() {}\n')
    writeFileSync(
      path.join(nativeDir, 'src', 'web', 'index.tsx'),
      `export default function NativeWidget() { return null }\n`,
    )
    writeFileSync(path.join(nativeDir, 'locales', 'en.json'), JSON.stringify({ title: 'Native Demo' }))

    const lintOk = lintPlugin(nativeDir)
    expect(lintOk.errors).toEqual([])
    expect(lintOk.ok).toBe(true)

    const origGoTmp = process.env.GOTMPDIR
    const origGoCache = process.env.GOCACHE
    process.env.GOTMPDIR = os.tmpdir()
    process.env.GOCACHE = path.join(os.tmpdir(), 'skinloom-gocache')
    try {
      const built = await buildPlugin({
        dir: nativeDir,
        native: true,
        targets: ['linux/amd64', 'windows/amd64'],
      })
      expect(built.nativeBinaries).toHaveLength(2)
      expect(built.nativeBinaries?.map((b) => b.relPath)).toEqual([
        'bin/native-cli-demo_linux_amd64',
        'bin/native-cli-demo_windows_amd64.exe',
      ])
      expect(built.webBytes).toBeGreaterThan(0)

      const keys = generateKeyPair(makeTempDir('sl-cli-native-keys-'), 'release')

      // --split-platforms emits one .slp per target + artifacts.json
      const splitOutDir = path.join(nativeDir, 'release-split')
      const splitRes = await packPlugin({
        dir: nativeDir,
        skipBuild: true,
        buildDir: 'dist',
        splitPlatforms: true,
        outDir: splitOutDir,
        privateKeyFile: keys.privateKeyPath,
      })
      expect(splitRes.signed).toBe(true)
      expect(Object.keys(splitRes.artifacts ?? {}).sort()).toEqual(['linux/amd64', 'windows/amd64'])
      const manifestDoc = JSON.parse(readFileSync(splitRes.artifactsManifestPath!, 'utf8')) as {
        runtime: string
        artifacts: Record<string, { file: string; sha256: string; size: number; signature?: string }>
      }
      expect(manifestDoc.runtime).toBe('native')
      expect(manifestDoc.artifacts['linux/amd64']?.signature).toBeTruthy()

      const linuxEntries = readZip(readFileSync(splitRes.artifacts!['linux/amd64']!.archivePath))
      const linuxNames = linuxEntries.map((e) => e.name)
      expect(linuxNames).toContain('bin/native-cli-demo_linux_amd64')
      expect(linuxNames).not.toContain('bin/native-cli-demo_windows_amd64.exe')
      const linuxMf = JSON.parse(
        Buffer.from(linuxEntries.find((e) => e.name === 'plugin.json')!.data).toString('utf8'),
      ) as { native?: { platforms?: string[] } }
      expect(linuxMf.native?.platforms).toEqual(['linux/amd64'])
      expect(
        verifyPackage({ archivePath: splitRes.artifacts!['linux/amd64']!.archivePath, publicKeyFile: keys.publicKeyPath })
          .ok,
      ).toBe(true)

      // --fat bundles all targets into one offline archive
      const fatRes = await packPlugin({
        dir: nativeDir,
        skipBuild: true,
        buildDir: 'dist',
        fat: true,
        outFile: path.join(nativeDir, 'native-cli-demo-fat.slp'),
        privateKeyFile: keys.privateKeyPath,
      })
      const fatNames = readZip(readFileSync(fatRes.archivePath)).map((e) => e.name)
      expect(fatNames).toContain('bin/native-cli-demo_linux_amd64')
      expect(fatNames).toContain('bin/native-cli-demo_windows_amd64.exe')
      expect(verifyPackage({ archivePath: fatRes.archivePath, publicKeyFile: keys.publicKeyPath }).ok).toBe(true)

      // pushDevReload sends native manifest + base64 binary in files map
      let devBody: { manifest?: { runtime?: string }; files?: Record<string, string> } = {}
      await pushDevReload({
        url: 'http://127.0.0.1:8080',
        token: 'dev-token',
        distDir: built.outDir,
        fetchImpl: (async (_url, init) => {
          devBody = JSON.parse(String(init?.body ?? '{}'))
          return new Response(JSON.stringify({ id: 'native-cli-demo', runtime: 'native' }), { status: 200 })
        }) as typeof fetch,
      })
      expect(devBody.manifest?.runtime).toBe('native')
      expect(Object.keys(devBody.files ?? {})).toContain('bin/native-cli-demo_linux_amd64')
    } finally {
      if (origGoTmp === undefined) delete process.env.GOTMPDIR
      else process.env.GOTMPDIR = origGoTmp
      if (origGoCache === undefined) delete process.env.GOCACHE
      else process.env.GOCACHE = origGoCache
    }
  })
})
