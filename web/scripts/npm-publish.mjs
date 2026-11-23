#!/usr/bin/env node
// Build, stage, validate, pack, or publish SkinLoom's public npm packages in
// topological dependency order without mutating workspace source package.json files.
//
// Usage:
//   node scripts/npm-publish.mjs --dry-run [--version 1.1.3] [--only <pkg>]
//   node scripts/npm-publish.mjs --pack    [--version 1.1.3] [--only <pkg>]
//   node scripts/npm-publish.mjs --publish [--version 1.1.3] [--tag latest] [--provenance] [--skip-existing] [--otp 123456] [--only <pkg>]
import { spawnSync } from 'node:child_process'
import { copyFileSync, cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { detectPackageManager, pmCommand, runArgs } from './ws.mjs'

const wsRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repoRoot = path.resolve(wsRoot, '..')
const stageRoot = path.join(wsRoot, '.npm-publish-stage')

export const SEMVER_RE = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/
export const CANONICAL_REPO_URL = 'git+https://github.com/Entropy-Increase-Team/skinloom.git'

/** Publishable packages in strict topological order (dependencies before dependents). */
export const PUBLISH_PACKAGES = [
  {
    dir: 'ui',
    name: '@skinloom/ui',
    exports: {
      '.': {
        types: './dist/index.d.ts',
        import: './dist/index.js',
      },
      './globals.css': './dist/styles/globals.css',
      './components/ui/chart': {
        types: './dist/components/ui/chart.d.ts',
        import: './dist/components/ui/chart.js',
      },
    },
    imports: {
      '#components/*': {
        types: './dist/components/*.d.ts',
        default: './dist/components/*.js',
      },
      '#lib/*': {
        types: './dist/lib/*.d.ts',
        default: './dist/lib/*.js',
      },
      '#hooks/*': {
        types: './dist/hooks/*.d.ts',
        default: './dist/hooks/*.js',
      },
    },
  },
  {
    dir: 'plugin-sdk',
    name: '@skinloom/plugin-sdk',
    exports: {
      '.': {
        types: './dist/index.d.ts',
        import: './dist/index.js',
      },
      './server': {
        types: './dist/server/index.d.ts',
        import: './dist/server/index.js',
      },
      './web': {
        types: './dist/web/index.d.ts',
        import: './dist/web/index.js',
      },
      './testing': {
        types: './dist/testing/index.d.ts',
        import: './dist/testing/index.js',
      },
      './catalog': {
        types: './dist/gen/catalog.d.ts',
        import: './dist/gen/catalog.js',
      },
    },
  },
  {
    dir: 'cli',
    name: '@skinloom/cli',
    exports: {
      '.': {
        types: './dist/index.d.ts',
        import: './dist/index.js',
      },
    },
  },
  {
    dir: 'create-plugin',
    name: 'create-skinloom-plugin',
    exports: {
      '.': {
        types: './dist/index.d.ts',
        import: './dist/index.js',
      },
    },
  },
]

const INTERNAL_PACKAGE_NAMES = new Set(PUBLISH_PACKAGES.map((p) => p.name))

/** Infer default npm dist-tag from a version string ("next" for prereleases, else "latest"). */
export function defaultDistTag(version) {
  return version.includes('-') ? 'next' : 'latest'
}

/** Rewrite workspace wildcards for sibling SkinLoom packages into explicit semver ranges. */
function rewriteDependencyMap(deps, targetVersion) {
  if (!deps || typeof deps !== 'object') return undefined
  const out = {}
  for (const [k, v] of Object.entries(deps)) {
    if (INTERNAL_PACKAGE_NAMES.has(k) && (v === '*' || String(v).startsWith('workspace:'))) {
      out[k] = `^${targetVersion}`
    } else {
      out[k] = v
    }
  }
  return out
}

/** Build the production package.json written into the publish staging directory. */
export function buildPublishManifest(srcPkg, spec, targetVersion) {
  if (!SEMVER_RE.test(targetVersion)) {
    throw new Error(`Invalid semver version "${targetVersion}"`)
  }
  const files = ['dist', 'README.md', 'LICENSE']
  if (srcPkg.bin) files.unshift('bin')

  const out = {
    name: spec.name,
    version: targetVersion,
    description: srcPkg.description,
    license: srcPkg.license || 'MIT',
    homepage: srcPkg.homepage || 'https://docs.skinloom.cn/docs/plugins/overview',
    repository: {
      type: 'git',
      url: CANONICAL_REPO_URL,
      directory: `web/packages/${spec.dir}`,
    },
    bugs: srcPkg.bugs || {
      url: 'https://github.com/Entropy-Increase-Team/skinloom/issues',
    },
    publishConfig: {
      access: 'public',
    },
    type: 'module',
    files,
  }
  if (srcPkg.sideEffects !== undefined) out.sideEffects = srcPkg.sideEffects
  if (srcPkg.bin) out.bin = srcPkg.bin
  out.exports = spec.exports
  if (spec.imports) out.imports = spec.imports
  if (srcPkg.engines) out.engines = srcPkg.engines
  if (srcPkg.dependencies) out.dependencies = rewriteDependencyMap(srcPkg.dependencies, targetVersion)
  if (srcPkg.peerDependencies) out.peerDependencies = rewriteDependencyMap(srcPkg.peerDependencies, targetVersion)
  if (srcPkg.peerDependenciesMeta) out.peerDependenciesMeta = srcPkg.peerDependenciesMeta
  return out
}

/** Validate a staged npm package directory before packing or publishing. */
export function validateStagedPackage(stageDir, manifest) {
  const errors = []
  if (manifest.private) {
    errors.push(`${manifest.name}: must not set private: true`)
  }
  if (manifest.publishConfig?.access !== 'public') {
    errors.push(`${manifest.name}: publishConfig.access must be "public"`)
  }
  if (manifest.repository?.url !== CANONICAL_REPO_URL) {
    errors.push(`${manifest.name}: repository.url must be "${CANONICAL_REPO_URL}" for npm provenance`)
  }
  for (const field of ['dependencies', 'peerDependencies']) {
    const map = manifest[field]
    if (!map) continue
    for (const [dep, range] of Object.entries(map)) {
      if (range === '*' || String(range).startsWith('workspace:')) {
        errors.push(`${manifest.name}: ${field}.${dep} has unresolved workspace specifier "${range}"`)
      }
    }
  }
  const checkPath = (rel, label, allowDts) => {
    if (typeof rel !== 'string' || !rel.startsWith('./')) {
      errors.push(`${manifest.name}: ${label} must be a relative "./" path, got "${rel}"`)
      return
    }
    if (!allowDts && rel.endsWith('.ts') && !rel.endsWith('.d.ts')) {
      errors.push(`${manifest.name}: ${label} leaks raw TypeScript source "${rel}"`)
    }
    const full = path.join(stageDir, rel)
    if (!existsSync(full)) {
      errors.push(`${manifest.name}: ${label} target "${rel}" does not exist in staged package`)
    }
  }
  if (manifest.bin) {
    for (const [binName, rel] of Object.entries(manifest.bin)) {
      checkPath(rel, `bin.${binName}`, false)
    }
  }
  for (const [subpath, target] of Object.entries(manifest.exports || {})) {
    if (typeof target === 'string') {
      checkPath(target, `exports["${subpath}"]`, false)
    } else if (target && typeof target === 'object') {
      if (target.types) checkPath(target.types, `exports["${subpath}"].types`, true)
      if (target.import) checkPath(target.import, `exports["${subpath}"].import`, false)
    }
  }
  for (const reqFile of ['README.md', 'LICENSE', 'package.json']) {
    if (!existsSync(path.join(stageDir, reqFile))) {
      errors.push(`${manifest.name}: missing required file "${reqFile}"`)
    }
  }
  return errors
}

function npmCommand() {
  if (process.platform === 'win32') {
    const cli = path.join(path.dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js')
    if (existsSync(cli)) return [process.execPath, [cli]]
  }
  return ['npm', []]
}

function runCmd(cmd, args, cwd, capture = false) {
  const res = spawnSync(cmd, args, {
    cwd,
    stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
    encoding: capture ? 'utf8' : undefined,
  })
  if (res.error) throw res.error
  return res
}

function isAlreadyPublished(pkgName, version) {
  const [cmd, lead] = npmCommand()
  const res = runCmd(cmd, [...lead, 'view', `${pkgName}@${version}`, 'version'], wsRoot, true)
  return res.status === 0 && String(res.stdout || '').trim() === version
}

function parseArgs(argv) {
  const opts = {
    mode: 'dry-run',
    version: '',
    tag: '',
    provenance: false,
    skipExisting: false,
    otp: '',
    only: '',
  }
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (a === '--dry-run') opts.mode = 'dry-run'
    else if (a === '--pack') opts.mode = 'pack'
    else if (a === '--publish') opts.mode = 'publish'
    else if (a === '--provenance') opts.provenance = true
    else if (a === '--skip-existing') opts.skipExisting = true
    else if (a === '--version' && argv[i + 1]) opts.version = argv[++i].replace(/^v/, '')
    else if (a === '--tag' && argv[i + 1]) opts.tag = argv[++i]
    else if (a === '--otp' && argv[i + 1]) opts.otp = argv[++i]
    else if (a === '--only' && argv[i + 1]) opts.only = argv[++i]
    else if (a === '-h' || a === '--help') {
      console.log(
        'Usage: node scripts/npm-publish.mjs [--dry-run|--pack|--publish] [--version X.Y.Z] [--tag latest|next] [--provenance] [--skip-existing] [--otp CODE] [--only PKG]',
      )
      process.exit(0)
    } else {
      throw new Error(`Unknown argument: ${a}`)
    }
  }
  return opts
}

function main() {
  const opts = parseArgs(process.argv.slice(2))
  const pm = detectPackageManager()
  const [pmBin, pmLead] = pmCommand(pm)
  const [npmBin, npmLead] = npmCommand()

  const selected = opts.only
    ? PUBLISH_PACKAGES.filter((p) => p.name === opts.only || p.dir === opts.only)
    : PUBLISH_PACKAGES
  if (selected.length === 0) {
    throw new Error(`Unknown package selector "${opts.only}"`)
  }

  rmSync(stageRoot, { recursive: true, force: true })
  mkdirSync(stageRoot, { recursive: true })
  const tarballDir = path.join(stageRoot, 'tarballs')
  if (opts.mode === 'pack') mkdirSync(tarballDir, { recursive: true })

  const rootLicense = path.join(repoRoot, 'LICENSE')
  if (!existsSync(rootLicense)) {
    throw new Error(`Missing repository LICENSE at ${rootLicense}`)
  }

  for (const spec of selected) {
    const pkgDir = path.join(wsRoot, 'packages', spec.dir)
    const srcPkg = JSON.parse(readFileSync(path.join(pkgDir, 'package.json'), 'utf8'))
    const version = opts.version || srcPkg.version
    const tag = opts.tag || defaultDistTag(version)

    console.log(`\n=== [${spec.name}@${version}] Building ===`)
    const buildRes = runCmd(pmBin, [...pmLead, ...runArgs(pm, 'build')], pkgDir)
    if (buildRes.status !== 0) process.exit(buildRes.status ?? 1)

    const stageDir = path.join(stageRoot, spec.dir)
    mkdirSync(stageDir, { recursive: true })
    cpSync(path.join(pkgDir, 'dist'), path.join(stageDir, 'dist'), { recursive: true })
    if (existsSync(path.join(pkgDir, 'bin'))) {
      cpSync(path.join(pkgDir, 'bin'), path.join(stageDir, 'bin'), { recursive: true })
    }
    if (existsSync(path.join(pkgDir, 'README.md'))) {
      copyFileSync(path.join(pkgDir, 'README.md'), path.join(stageDir, 'README.md'))
    }
    copyFileSync(rootLicense, path.join(stageDir, 'LICENSE'))

    const manifest = buildPublishManifest(srcPkg, spec, version)
    writeFileSync(path.join(stageDir, 'package.json'), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8')

    const errors = validateStagedPackage(stageDir, manifest)
    if (errors.length > 0) {
      console.error(`Staged package validation failed for ${spec.name}:`)
      for (const e of errors) console.error(`  - ${e}`)
      process.exit(1)
    }

    if (opts.mode === 'dry-run') {
      console.log(`=== [${spec.name}@${version}] Dry-run pack ===`)
      const r = runCmd(npmBin, [...npmLead, 'pack', '--dry-run'], stageDir)
      if (r.status !== 0) process.exit(r.status ?? 1)
    } else if (opts.mode === 'pack') {
      console.log(`=== [${spec.name}@${version}] Packing tarball ===`)
      const r = runCmd(npmBin, [...npmLead, 'pack', '--pack-destination', tarballDir], stageDir)
      if (r.status !== 0) process.exit(r.status ?? 1)
    } else if (opts.mode === 'publish') {
      if (opts.skipExisting && isAlreadyPublished(spec.name, version)) {
        console.log(`=== [${spec.name}@${version}] Already published on npm; skipping ===`)
        continue
      }
      const pubArgs = [...npmLead, 'publish', '--access', 'public', '--tag', tag]
      if (opts.provenance) pubArgs.push('--provenance')
      if (opts.otp) pubArgs.push('--otp', opts.otp)
      console.log(`=== [${spec.name}@${version}] Publishing (tag=${tag}, provenance=${opts.provenance}) ===`)
      const r = runCmd(npmBin, pubArgs, stageDir)
      if (r.status !== 0) process.exit(r.status ?? 1)
    }
  }

  console.log(`\nCompleted "${opts.mode}" for ${selected.length} package(s) cleanly.`)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main()
}
