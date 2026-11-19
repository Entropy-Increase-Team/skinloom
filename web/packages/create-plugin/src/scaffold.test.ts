import { execFileSync, type ExecFileSyncOptions } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { buildPlugin, currentHostPlatform, lintPlugin } from '@skinloom/cli'
import { runCreateCli } from './cli.js'
import { PLUGIN_TEMPLATES, scaffoldPlugin } from './index.js'

const repoRoot = path.resolve(import.meta.dirname, '../../../..')
const webRoot = path.resolve(import.meta.dirname, '../../..')
const webNodeModules = path.join(webRoot, 'node_modules')
const tscBin = path.join(webNodeModules, 'typescript', 'bin', 'tsc')
const vitestBin = path.join(webNodeModules, 'vitest', 'vitest.mjs')

const tempDirs: string[] = []
const junctions: string[] = []

function runChecked(cmd: string, args: string[], opts: ExecFileSyncOptions) {
  try {
    execFileSync(cmd, args, opts)
  } catch (err: unknown) {
    const e = err as { stdout?: Buffer | string; stderr?: Buffer | string; message?: string }
    const out = typeof e.stdout === 'string' ? e.stdout : e.stdout?.toString('utf8') ?? ''
    const errOut = typeof e.stderr === 'string' ? e.stderr : e.stderr?.toString('utf8') ?? ''
    throw new Error(`${e.message ?? 'Command failed'}\nSTDOUT:\n${out}\nSTDERR:\n${errOut}`)
  }
}

beforeAll(() => {
  // Ensure @skinloom/* workspace links exist in root web/node_modules/@skinloom
  // across all package managers (including pnpm with node-linker=hoisted).
  const scopeDir = path.join(webNodeModules, '@skinloom')
  mkdirSync(scopeDir, { recursive: true })
  for (const pkgName of ['api-client', 'cli', 'plugin-sdk', 'ui']) {
    const linkPath = path.join(scopeDir, pkgName)
    const targetPath = path.join(webRoot, 'packages', pkgName)
    if (!existsSync(linkPath) && existsSync(targetPath)) {
      symlinkSync(targetPath, linkPath, 'junction')
    }
  }
})

afterEach(() => {
  while (junctions.length > 0) {
    try {
      rmSync(junctions.pop()!, { recursive: false, force: true })
    } catch {
      // ignore if already removed
    }
  }
  while (tempDirs.length > 0) {
    rmSync(tempDirs.pop()!, { recursive: true, force: true })
  }
})

describe('create-skinloom-plugin', () => {
  it.each(PLUGIN_TEMPLATES)(
    'scaffolds, lints, builds, typechecks, and tests the "%s" template cleanly',
    { timeout: 180000 },
    async (template) => {
      const root = mkdtempSync(path.join(os.tmpdir(), `sl-tpl-${template}-`))
      tempDirs.push(root)

      const targetDir = path.join(root, `demo-${template}`)
      const res = scaffoldPlugin({ targetDir, template })
      expect(res.files).toContain('plugin.json')

      const nmLink = path.join(targetDir, 'node_modules')
      symlinkSync(webNodeModules, nmLink, 'junction')
      junctions.push(nmLink)

      if (template === 'native-go') {
        // Point generated go.mod at the local repository checkout so offline CI/tests
        // compile and test the real Go plugin against sdk/go without network access.
        const goModPath = path.join(targetDir, 'go.mod')
        const repoGoMod = readFileSync(path.join(repoRoot, 'go.mod'), 'utf8')
        const goVerLine = /^go\s+.+$/m.exec(repoGoMod)?.[0] ?? 'go 1.26.10'
        const toolchainLine = /^toolchain\s+.+$/m.exec(repoGoMod)?.[0] ?? ''
        const reqBlocks = repoGoMod.slice(repoGoMod.indexOf('require ('))
        writeFileSync(
          goModPath,
          [
            `module ${res.id}`,
            '',
            goVerLine,
            ...(toolchainLine ? [toolchainLine] : []),
            '',
            'require github.com/Entropy-Increase-Team/skinloom v0.0.0',
            '',
            `replace github.com/Entropy-Increase-Team/skinloom => ${repoRoot.replaceAll('\\', '/')}`,
            '',
            reqBlocks,
          ].join('\n'),
          'utf8',
        )
        copyFileSync(path.join(repoRoot, 'go.sum'), path.join(targetDir, 'go.sum'))

        const origGoTmp = process.env.GOTMPDIR
        const origGoCache = process.env.GOCACHE
        const goCacheDir = origGoCache || path.join(os.tmpdir(), 'skinloom-gocache')
        process.env.GOTMPDIR = os.tmpdir()
        process.env.GOCACHE = goCacheDir
        try {
          const built = await buildPlugin({ dir: targetDir, native: true, targets: [currentHostPlatform()] })
          expect(built.manifest.id).toBe(res.id)
          expect(built.nativeBinaries).toHaveLength(1)
          expect(built.webBytes).toBeGreaterThan(0)

          // Pass -trimpath so go test reuses the GOCACHE action IDs already compiled by buildPlugin.
          runChecked('go', ['test', '-trimpath', '-v', './...'], {
            cwd: targetDir,
            env: { ...process.env, GOTMPDIR: os.tmpdir(), GOCACHE: goCacheDir },
            stdio: 'pipe',
          })
        } finally {
          if (origGoTmp === undefined) delete process.env.GOTMPDIR
          else process.env.GOTMPDIR = origGoTmp
          if (origGoCache === undefined) delete process.env.GOCACHE
          else process.env.GOCACHE = origGoCache
        }
      } else {
        const built = await buildPlugin({ dir: targetDir })
        expect(built.manifest.id).toBe(res.id)
      }

      const lint = lintPlugin(targetDir)
      expect(lint.errors).toEqual([])
      expect(lint.ok).toBe(true)

      runChecked(process.execPath, [tscBin, '-p', targetDir], { cwd: targetDir, stdio: 'pipe' })
      runChecked(process.execPath, [vitestBin, 'run'], { cwd: targetDir, stdio: 'pipe' })
    },
  )

  it('parses boolean --force flag without swallowing the target directory positional argument', async () => {
    const root = mkdtempSync(path.join(os.tmpdir(), 'sl-cli-force-'))
    tempDirs.push(root)
    const targetDir = path.join(root, 'my-plugin')

    expect(await runCreateCli([targetDir, '--template', 'basic'])).toBe(0)
    expect(await runCreateCli(['--force', targetDir, '--template', 'basic'])).toBe(0)
  })
})
