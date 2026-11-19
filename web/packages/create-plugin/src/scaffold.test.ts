import { execFileSync } from 'node:child_process'
import { copyFileSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { afterEach, describe, expect, it } from 'vitest'
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
  it.each(PLUGIN_TEMPLATES)('scaffolds, lints, builds, typechecks, and tests the "%s" template cleanly', { timeout: 60000 }, async (template) => {
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
      process.env.GOTMPDIR = os.tmpdir()
      process.env.GOCACHE = path.join(os.tmpdir(), 'skinloom-gocache')
      try {
        const built = await buildPlugin({ dir: targetDir, native: true, targets: [currentHostPlatform()] })
        expect(built.manifest.id).toBe(res.id)
        expect(built.nativeBinaries).toHaveLength(1)
        expect(built.webBytes).toBeGreaterThan(0)

        execFileSync('go', ['test', '-v', './...'], {
          cwd: targetDir,
          env: { ...process.env, GOTMPDIR: os.tmpdir(), GOCACHE: path.join(os.tmpdir(), 'skinloom-gocache') },
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

    execFileSync(process.execPath, [tscBin, '-p', targetDir], { cwd: targetDir, stdio: 'pipe' })
    execFileSync(process.execPath, [vitestBin, 'run'], { cwd: targetDir, stdio: 'pipe' })
  })

  it('parses boolean --force flag without swallowing the target directory positional argument', async () => {
    const root = mkdtempSync(path.join(os.tmpdir(), 'sl-cli-force-'))
    tempDirs.push(root)
    const targetDir = path.join(root, 'my-plugin')

    expect(await runCreateCli([targetDir, '--template', 'basic'])).toBe(0)
    expect(await runCreateCli(['--force', targetDir, '--template', 'basic'])).toBe(0)
  })
})
