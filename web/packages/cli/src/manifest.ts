import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'

export const MAX_SERVER_BUNDLE_BYTES = 5 * 1024 * 1024 // 5 MiB (server.js cap)
export const MAX_ASSET_FILE_BYTES = 10 * 1024 * 1024 // 10 MiB per file
export const MAX_PACKAGE_TOTAL_BYTES = 20 * 1024 * 1024 // 20 MiB total JS/theme package cap
export const MAX_NATIVE_SINGLE_PACKAGE_BYTES = 50 * 1024 * 1024 // 50 MiB single-platform native .slp cap
export const MAX_NATIVE_FAT_PACKAGE_BYTES = 150 * 1024 * 1024 // 150 MiB multi-platform fat native .slp cap
export const MAX_NATIVE_UNPACKED_SINGLE_BYTES = 100 * 1024 * 1024 // 100 MiB single-platform native unpacked cap
export const MAX_NATIVE_UNPACKED_FAT_BYTES = 256 * 1024 * 1024 // 256 MiB multi-platform fat native unpacked cap
export const MAX_NATIVE_BINARY_BYTES = 64 * 1024 * 1024 // 64 MiB individual Go binary cap

export const DEFAULT_NATIVE_TARGETS = [
  'linux/amd64',
  'linux/arm64',
  'windows/amd64',
  'darwin/arm64',
  'darwin/amd64',
] as const

export const PLUGIN_ID_RE = /^[a-z0-9][a-z0-9-]{2,39}$/
export const THEME_ID_RE = /^[a-z0-9][a-z0-9-]{0,31}$/
export const COLL_NAME_RE = /^[a-zA-Z_][a-zA-Z0-9_]{0,63}$/
export const INDEX_FIELD_RE = /^[a-zA-Z_][a-zA-Z0-9_]{0,63}$/
export const PLATFORM_RE = /^[a-z0-9]+\/[a-z0-9]+$/

export const SHARED_WEB_MODULES = [
  'react',
  'react/jsx-runtime',
  'react/jsx-dev-runtime',
  'react-dom',
  'react-dom/client',
  '@tanstack/react-query',
  '@skinloom/ui',
  '@skinloom/plugin-sdk/web',
] as const

export const NODE_BUILTIN_MODULES = new Set([
  'assert',
  'assert/strict',
  'async_hooks',
  'buffer',
  'child_process',
  'cluster',
  'console',
  'constants',
  'crypto',
  'dgram',
  'diagnostics_channel',
  'dns',
  'dns/promises',
  'domain',
  'events',
  'fs',
  'fs/promises',
  'http',
  'http2',
  'https',
  'inspector',
  'inspector/promises',
  'module',
  'net',
  'os',
  'path',
  'path/posix',
  'path/win32',
  'perf_hooks',
  'process',
  'punycode',
  'querystring',
  'readline',
  'readline/promises',
  'repl',
  'stream',
  'stream/consumers',
  'stream/promises',
  'stream/web',
  'string_decoder',
  'sys',
  'timers',
  'timers/promises',
  'tls',
  'trace_events',
  'tty',
  'url',
  'util',
  'util/types',
  'v8',
  'vm',
  'wasi',
  'worker_threads',
  'zlib',
  'bun',
  'bun:ffi',
  'bun:sqlite',
  'bun:test',
])

export type LocalizedTextJson = string | Record<string, string>

export interface ManifestFormField {
  name: string
  type: string
  label: LocalizedTextJson
  description?: LocalizedTextJson
  placeholder?: LocalizedTextJson
  required?: boolean
  maxLength?: number
  options?: string[]
}

export interface ManifestMenuDef {
  area: 'user' | 'admin' | 'explore' | string
  path: string
  title: LocalizedTextJson
  icon?: string
  order?: number
  role?: 'admin' | 'superadmin' | string
}

export interface ManifestNativeDatabase {
  required?: boolean
}

export interface ManifestNativeSpec {
  protocolVersion?: number
  executable: string
  platforms?: string[]
  httpProxy?: boolean
  database?: ManifestNativeDatabase
}

export interface ManifestJson {
  $schema?: string
  id: string
  kind: 'plugin' | 'theme' | string
  runtime?: 'js' | 'native' | string
  version: string
  name: LocalizedTextJson
  description?: LocalizedTextJson
  authors?: string[]
  license?: string
  homepage?: string
  repository?: string
  icon?: string
  screenshot?: string
  extends?: string
  engines?: { skinloom?: string; go?: string }
  apiVersion: number
  server?: string
  web?: string
  native?: ManifestNativeSpec
  permissions?: string[]
  http?: { allow?: string[]; maxBodyBytes?: number }
  hooks?: { guard?: string[]; filter?: string[]; event?: string[] }
  routes?: { public?: string[] }
  storage?: {
    collections?: Record<string, { indexes?: Record<string, string> }>
    quota?: { kvKeys?: number; docs?: number; filesBytes?: number }
  }
  settings?: Record<string, Record<string, unknown>>
  menus?: ManifestMenuDef[]
  forms?: Record<string, ManifestFormField[]>
  blocks?: Record<string, unknown>
  slots?: string[]
  dependencies?: Record<string, string>
  optionalDependencies?: Record<string, string>
  services?: string[]
  theme?: {
    colorScheme?: string
    css?: string
    tokens?: Record<string, unknown>
    layout?: Record<string, unknown>
    overrides?: string[]
    home?: unknown[]
    settings?: Record<string, Record<string, unknown>>
  }
}

export interface LoadedManifest {
  path: string
  raw: string
  manifest: ManifestJson
}

/** Loads and parses `<dir>/plugin.json`, throwing a descriptive error on failure. */
export function loadManifest(dir: string): LoadedManifest {
  const manifestPath = path.join(dir, 'plugin.json')
  if (!existsSync(manifestPath)) {
    throw new Error(`Missing plugin.json in ${dir}`)
  }
  const raw = readFileSync(manifestPath, 'utf8')
  const parsed = JSON.parse(raw) as unknown
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('plugin.json must contain a JSON object')
  }
  return {
    path: manifestPath,
    raw,
    manifest: parsed as ManifestJson,
  }
}

/** Resolves a safe relative path and returns null if safe, or an error description. */
export function checkSafeRelPath(relPath: string | undefined, field: string): string | null {
  if (!relPath) return null
  if (/[\x00-\x1f\x7f]/.test(relPath) || relPath.includes('\\') || relPath.startsWith('/') || relPath.endsWith('/')) {
    return `${field} path "${relPath}" must be a relative POSIX path`
  }
  if (relPath.split('/').some((seg) => seg === '' || seg === '.' || seg === '..')) {
    return `${field} path "${relPath}" must not escape the plugin root`
  }
  const clean = path.posix.normalize(relPath)
  if (clean === '.' || clean === '..' || clean.startsWith('../')) {
    return `${field} path "${relPath}" must not escape the plugin root`
  }
  return null
}

/** Resolves a source file on disk matching a built manifest entry or standard source candidates. */
export function resolveEntrySource(
  dir: string,
  declared: string | undefined,
  outRelDefault: string,
  candidates: string[],
  outDir = 'dist',
): string | null {
  const isCaseInsensitive = process.platform === 'win32' || process.platform === 'darwin'
  const normPath = (p: string) => (isCaseInsensitive ? p.toLowerCase() : p)
  const rootPrefix = normPath(path.resolve(dir) + path.sep)
  const outPrefix = normPath(path.resolve(dir, outDir) + path.sep)

  if (declared) {
    const safeErr = checkSafeRelPath(declared.replace(/^\.\//, ''), 'entry')
    if (safeErr) {
      throw new Error(safeErr)
    }
    const cleanDeclared = path.posix.normalize(declared)
    const absDeclared = path.resolve(dir, cleanDeclared)
    if (!normPath(absDeclared).startsWith(rootPrefix)) {
      throw new Error(`Entry path "${declared}" must not escape the project root`)
    }
    if (existsSync(absDeclared) && statSync(absDeclared).isFile() && !normPath(absDeclared).startsWith(outPrefix)) {
      if (cleanDeclared !== outRelDefault) {
        return absDeclared
      }
      // If `declared` matches default dist output (e.g. "server.js"), prefer src/* when present.
      for (const candidate of candidates) {
        const abs = path.resolve(dir, candidate)
        if (normPath(abs) !== normPath(absDeclared) && existsSync(abs) && statSync(abs).isFile()) {
          return abs
        }
      }
      return absDeclared
    }
  }

  for (const candidate of candidates) {
    const abs = path.resolve(dir, candidate)
    if (existsSync(abs) && statSync(abs).isFile()) {
      return abs
    }
  }

  if (declared) {
    throw new Error(`Declared entry "${declared}" was not found (checked ${declared} and ${candidates.join(', ')})`)
  }
  return null
}

/** Walks a directory recursively, returning relative POSIX paths sorted deterministically. */
export function walkFiles(rootDir: string, ignoreNames = new Set<string>()): string[] {
  if (!existsSync(rootDir)) return []
  const out: string[] = []
  function visit(currentDir: string, relBase: string): void {
    const entries = readdirSync(currentDir, { withFileTypes: true }).sort((a, b) =>
      a.name < b.name ? -1 : a.name > b.name ? 1 : 0,
    )
    for (const entry of entries) {
      if (ignoreNames.has(entry.name)) continue
      const abs = path.join(currentDir, entry.name)
      const rel = relBase ? `${relBase}/${entry.name}` : entry.name
      if (entry.isDirectory()) {
        visit(abs, rel)
      } else if (entry.isFile()) {
        out.push(rel)
      }
    }
  }
  visit(rootDir, '')
  return out
}

/** Resolves the primary localized label (preferring English, then zh-CN, then any). */
export function resolveLocalizedText(value: LocalizedTextJson | undefined): string {
  if (typeof value === 'string') return value.trim()
  if (!value || typeof value !== 'object' || Array.isArray(value)) return ''
  for (const key of ['en', 'zh-CN']) {
    const candidate = value[key]
    if (typeof candidate === 'string' && candidate.trim() !== '') {
      return candidate.trim()
    }
  }
  for (const key of Object.keys(value).sort()) {
    const candidate = value[key]
    if (typeof candidate === 'string' && candidate.trim() !== '') {
      return candidate.trim()
    }
  }
  return ''
}

/** Returns normalized plugin runtime ('js' | 'native'). */
export function effectiveRuntime(manifest: ManifestJson): 'js' | 'native' {
  return manifest.runtime === 'native' ? 'native' : 'js'
}

/** Maps current Node/Bun host platform+arch to Go GOOS/GOARCH format. */
export function currentHostPlatform(): string {
  const osMap: Record<string, string> = {
    win32: 'windows',
    darwin: 'darwin',
    linux: 'linux',
    freebsd: 'freebsd',
  }
  const archMap: Record<string, string> = {
    x64: 'amd64',
    arm64: 'arm64',
    ia32: '386',
    arm: 'arm',
  }
  const goos = osMap[process.platform] ?? process.platform
  const goarch = archMap[process.arch] ?? process.arch
  return `${goos}/${goarch}`
}

/** Returns the canonical POSIX binary path inside dist/ or .slp for a GOOS/GOARCH target. */
export function nativeExecutableForTarget(executableBase: string, target: string): string {
  const [goos, goarch] = target.split('/')
  const cleanBase = path.posix.normalize(executableBase.replaceAll('\\', '/')).replace(/\.exe$/i, '')
  const ext = goos === 'windows' ? '.exe' : ''
  return `${cleanBase}_${goos}_${goarch}${ext}`
}

/** Checks whether a POSIX relative path corresponds to a native plugin executable binary. */
export function isNativeExecutablePath(manifest: ManifestJson, rel: string): boolean {
  if (effectiveRuntime(manifest) !== 'native' || !manifest.native?.executable) {
    return false
  }
  const base = path.posix.normalize(manifest.native.executable.replaceAll('\\', '/')).replace(/\.exe$/i, '')
  const clean = path.posix.normalize(rel.replaceAll('\\', '/'))
  if (path.posix.dirname(clean) !== path.posix.dirname(base)) {
    return false
  }
  if (clean === base || clean === `${base}.exe`) {
    return true
  }
  return parseTargetFromBinaryPath(manifest.native.executable, rel) !== null
}

/** Extracts GOOS/GOARCH from a platform-suffixed native binary path if recognized. */
export function parseTargetFromBinaryPath(executableBase: string, rel: string): string | null {
  const base = path.posix.normalize(executableBase.replaceAll('\\', '/')).replace(/\.exe$/i, '')
  const clean = path.posix.normalize(rel.replaceAll('\\', '/'))
  if (path.posix.dirname(clean) !== path.posix.dirname(base)) {
    return null
  }
  if (!clean.startsWith(`${base}_`) && !clean.startsWith(`${base}-`)) {
    return null
  }
  const hasExe = /\.exe$/i.test(clean)
  const suffix = clean.slice(base.length + 1).replace(/\.exe$/i, '')
  const sep = clean[base.length]
  const parts = suffix.split(sep)
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    return null
  }
  if (hasExe && parts[0] !== 'windows') {
    return null
  }
  const target = `${parts[0]}/${parts[1]}`
  return PLATFORM_RE.test(target) ? target : null
}
