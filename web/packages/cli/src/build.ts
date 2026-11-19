import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, lstatSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import * as esbuild from 'esbuild'
import {
  checkSafeRelPath,
  currentHostPlatform,
  effectiveRuntime,
  loadManifest,
  MAX_ASSET_FILE_BYTES,
  MAX_NATIVE_BINARY_BYTES,
  MAX_NATIVE_UNPACKED_FAT_BYTES,
  MAX_NATIVE_UNPACKED_SINGLE_BYTES,
  MAX_PACKAGE_TOTAL_BYTES,
  MAX_SERVER_BUNDLE_BYTES,
  nativeExecutableForTarget,
  NODE_BUILTIN_MODULES,
  PLATFORM_RE,
  resolveEntrySource,
  SHARED_WEB_MODULES,
  walkFiles,
  type ManifestJson,
} from './manifest.js'

export interface NativeBinaryArtifact {
  target: string
  relPath: string
  absPath: string
  bytes: number
}

export interface BuildOptions {
  dir?: string
  outDir?: string
  minify?: boolean
  sourcemap?: boolean
  native?: boolean
  targets?: string[]
  goBin?: string
  goPackage?: string
  allowUncappedCombined?: boolean
}

export interface BuildResult {
  outDir: string
  manifest: ManifestJson
  serverFile?: string
  serverBytes?: number
  nativeBinaries?: NativeBinaryArtifact[]
  webFile?: string
  webBytes?: number
  webCssFile?: string
  copiedAssets: number
  copiedLocales: number
  warnings: string[]
}

const SERVER_ENTRY_CANDIDATES = [
  'src/server.ts',
  'src/server.mts',
  'src/server.js',
  'src/server.mjs',
  'server.ts',
]

const WEB_ENTRY_CANDIDATES = [
  'src/web/index.tsx',
  'src/web/index.ts',
  'src/web/index.jsx',
  'src/web/index.js',
  'src/web.tsx',
  'src/web.ts',
]

const FORBIDDEN_SERVER_WEB_MODULES = new Set([
  'react',
  'react/jsx-runtime',
  'react/jsx-dev-runtime',
  'react-dom',
  'react-dom/client',
  'react-dom/server',
  '@tanstack/react-query',
  '@skinloom/ui',
  '@skinloom/plugin-sdk',
  '@skinloom/plugin-sdk/web',
])

function isForbiddenServerUIModule(specifier: string): boolean {
  return (
    FORBIDDEN_SERVER_WEB_MODULES.has(specifier) ||
    specifier.startsWith('react/') ||
    specifier.startsWith('react-dom/') ||
    specifier.startsWith('@tanstack/react-query/') ||
    specifier.startsWith('@skinloom/ui/') ||
    specifier.startsWith('@skinloom/plugin-sdk/web/')
  )
}

function serverSandboxPlugin(projectDir: string): esbuild.Plugin {
  const hasLocalSdk = existsSync(path.join(projectDir, 'node_modules', '@skinloom', 'plugin-sdk'))
  return {
    name: 'skinloom-server-sandbox',
    setup(build) {
      build.onResolve({ filter: /.*/ }, (args) => {
        if (args.path.startsWith('node:') || args.path.startsWith('bun:') || NODE_BUILTIN_MODULES.has(args.path)) {
          return {
            errors: [
              {
                text: `Module "${args.path}" is not allowed in SkinLoom server bundles (QuickJS/WASM sandbox). Use host ctx.* APIs (ctx.crypto, ctx.fetch, ctx.kv, ctx.files) instead.`,
              },
            ],
          }
        }
        if (isForbiddenServerUIModule(args.path)) {
          return {
            errors: [
              {
                text: `Browser/UI module "${args.path}" cannot be imported from server.js; move UI components under src/web/.`,
              },
            ],
          }
        }
        if (
          !hasLocalSdk &&
          (args.path === '@skinloom/plugin-sdk/server' || args.path === '@skinloom/plugin-sdk/catalog')
        ) {
          try {
            return { path: fileURLToPath(import.meta.resolve(args.path)) }
          } catch {
            // Fall through to normal resolution if import.meta.resolve cannot find it.
          }
        }
        return null
      })
    },
  }
}

function webSharedExternalsPlugin(): esbuild.Plugin {
  const sharedSet = new Set<string>(SHARED_WEB_MODULES)
  return {
    name: 'skinloom-web-shared-externals',
    setup(build) {
      build.onResolve({ filter: /.*/ }, (args) => {
        if (sharedSet.has(args.path)) {
          return {
            path: args.path,
            namespace: 'skinloom-shared',
          }
        }
        if (args.path.startsWith('node:') || args.path.startsWith('bun:') || NODE_BUILTIN_MODULES.has(args.path)) {
          return {
            errors: [
              {
                text: `Node/Bun built-in "${args.path}" cannot be bundled into plugin web UI (${args.importer}).`,
              },
            ],
          }
        }
        return null
      })

      build.onLoad({ filter: /.*/, namespace: 'skinloom-shared' }, (args) => {
        const key = JSON.stringify(args.path)
        return {
          contents: `module.exports = (globalThis.__SKINLOOM_SHARED__ && globalThis.__SKINLOOM_SHARED__[${key}]) || {};`,
          loader: 'js',
        }
      })
    },
  }
}

function copyDirectoryTree(srcRoot: string, dstRoot: string, validateJson: boolean): { count: number; warnings: string[] } {
  const files = walkFiles(srcRoot)
  const warnings: string[] = []
  let count = 0
  for (const rel of files) {
    const absSrc = path.join(srcRoot, ...rel.split('/'))
    const absDst = path.join(dstRoot, ...rel.split('/'))
    const st = lstatSync(absSrc)
    if (st.isSymbolicLink() || !st.isFile()) {
      throw new Error(`Symbolic links or special files are not allowed in plugin assets ("${rel}")`)
    }
    const size = st.size
    if (size > MAX_ASSET_FILE_BYTES) {
      throw new Error(`Static file "${rel}" exceeds 10 MiB per-file limit (${size} bytes)`)
    }
    if (validateJson && rel.endsWith('.json')) {
      try {
        JSON.parse(readFileSync(absSrc, 'utf8'))
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        throw new Error(`Invalid locale JSON in locales/${rel}: ${message}`)
      }
    }
    mkdirSync(path.dirname(absDst), { recursive: true })
    copyFileSync(absSrc, absDst)
    count++
  }
  return { count, warnings }
}

/**
 * Bundles a SkinLoom plugin or theme into `outDir` (`server.js`, `web/index.js`,
 * `assets/`, `locales/`, and normalized `plugin.json`).
 */
export async function buildPlugin(options: BuildOptions = {}): Promise<BuildResult> {
  const projectDir = path.resolve(options.dir ?? '.')
  const outDirRel = options.outDir ?? 'dist'
  const outDir = path.resolve(projectDir, outDirRel)
  const { manifest } = loadManifest(projectDir)
  const outManifest: ManifestJson = structuredClone(manifest)
  const warnings: string[] = []

  const isNative = Boolean(options.native) || effectiveRuntime(manifest) === 'native'
  if (isNative && (!manifest.native || !manifest.native.executable)) {
    throw new Error('Native build requires runtime: "native" and native.executable in plugin.json')
  }
  if (isNative && manifest.native?.executable) {
    const execErr = checkSafeRelPath(manifest.native.executable, 'native.executable')
    if (execErr) throw new Error(execErr)
  }
  for (const [field, rel] of [
    ['server', manifest.server],
    ['web', manifest.web],
    ['theme.css', manifest.theme?.css],
    ['screenshot', manifest.screenshot],
    ['icon', manifest.icon],
  ] as const) {
    if (!rel) continue
    const pathErr = checkSafeRelPath(rel.replace(/^\.\//, ''), field)
    if (pathErr) throw new Error(pathErr)
  }

  const serverSource = isNative
    ? null
    : resolveEntrySource(projectDir, manifest.server, 'server.js', SERVER_ENTRY_CANDIDATES, outDirRel)
  const webSource = resolveEntrySource(projectDir, manifest.web, 'web/index.js', WEB_ENTRY_CANDIDATES, outDirRel)

  const isCaseInsensitive = process.platform === 'win32' || process.platform === 'darwin'
  const normOut = isCaseInsensitive ? outDir.toLowerCase() : outDir
  const normProj = isCaseInsensitive ? projectDir.toLowerCase() : projectDir
  const relOut = path.relative(normOut, normProj)
  if (
    relOut === '' ||
    (relOut !== '..' && !relOut.startsWith(`..${path.sep}`) && !path.isAbsolute(relOut))
  ) {
    throw new Error(`Refusing to clean outDir "${outDir}" because it contains the project root "${projectDir}"`)
  }
  rmSync(outDir, { recursive: true, force: true })
  mkdirSync(outDir, { recursive: true })

  let nativeBinaries: NativeBinaryArtifact[] | undefined
  if (isNative && manifest.native) {
    delete outManifest.server
    const rawTargets =
      options.targets && options.targets.length > 0
        ? options.targets
        : manifest.native.platforms && manifest.native.platforms.length > 0
          ? manifest.native.platforms
          : [currentHostPlatform()]
    const targets = Array.from(new Set(rawTargets.map((t) => t.trim()).filter(Boolean)))
    if (targets.length === 0) {
      throw new Error('Native build requires at least one os/arch target')
    }
    for (const target of targets) {
      if (!PLATFORM_RE.test(target)) {
        throw new Error(`Invalid native target "${target}" (expected os/arch like linux/amd64)`)
      }
    }

    const goBin = options.goBin ?? 'go'
    const goPackage = options.goPackage ?? '.'
    nativeBinaries = []
    for (const target of targets) {
      const [goos, goarch] = target.split('/')
      const relPath = nativeExecutableForTarget(manifest.native.executable, target)
      const absPath = path.join(outDir, ...relPath.split('/'))
      mkdirSync(path.dirname(absPath), { recursive: true })
      execFileSync(goBin, ['build', '-trimpath', '-ldflags=-s -w', '-o', absPath, goPackage], {
        cwd: projectDir,
        env: {
          ...process.env,
          CGO_ENABLED: '0',
          GOOS: goos,
          GOARCH: goarch,
        },
        stdio: 'pipe',
      })
      const bytes = statSync(absPath).size
      if (bytes > MAX_NATIVE_BINARY_BYTES) {
        throw new Error(
          `Built native binary "${relPath}" is ${bytes} bytes, exceeding the 64 MiB limit (${MAX_NATIVE_BINARY_BYTES} bytes)`,
        )
      }
      nativeBinaries.push({ target, relPath, absPath, bytes })
    }
    outManifest.runtime = 'native'
    outManifest.native = {
      ...manifest.native,
      protocolVersion: manifest.native.protocolVersion ?? 1,
      platforms: targets,
    }
  }

  let serverFile: string | undefined
  let serverBytes: number | undefined
  if (serverSource) {
    const outServerRel = 'server.js'
    const outServerAbs = path.join(outDir, outServerRel)
    await esbuild.build({
      entryPoints: [serverSource],
      outfile: outServerAbs,
      bundle: true,
      format: 'esm',
      platform: 'neutral',
      target: 'es2023',
      mainFields: ['module', 'main'],
      conditions: ['import', 'default'],
      minify: Boolean(options.minify),
      sourcemap: options.sourcemap ? 'inline' : false,
      plugins: [serverSandboxPlugin(projectDir)],
      logLevel: 'silent',
    })
    serverBytes = statSync(outServerAbs).size
    if (serverBytes > MAX_SERVER_BUNDLE_BYTES) {
      throw new Error(
        `Built server.js is ${serverBytes} bytes, exceeding the 5 MiB QuickJS sandbox limit (${MAX_SERVER_BUNDLE_BYTES} bytes)`,
      )
    }
    outManifest.server = outServerRel
    serverFile = outServerAbs
  } else {
    delete outManifest.server
  }

  let webFile: string | undefined
  let webBytes: number | undefined
  let webCssFile: string | undefined
  if (webSource) {
    const outWebRel = 'web/index.js'
    const outWebAbs = path.join(outDir, 'web', 'index.js')
    mkdirSync(path.dirname(outWebAbs), { recursive: true })
    await esbuild.build({
      entryPoints: [webSource],
      outfile: outWebAbs,
      bundle: true,
      format: 'esm',
      platform: 'browser',
      target: 'es2022',
      jsx: 'automatic',
      minify: Boolean(options.minify),
      sourcemap: options.sourcemap ? 'inline' : false,
      plugins: [webSharedExternalsPlugin()],
      logLevel: 'silent',
    })
    webBytes = statSync(outWebAbs).size
    outManifest.web = outWebRel
    webFile = outWebAbs
    const candidateCss = path.join(outDir, 'web', 'index.css')
    if (existsSync(candidateCss)) {
      webCssFile = candidateCss
    }
  } else {
    delete outManifest.web
  }

  const assetsResult = existsSync(path.join(projectDir, 'assets'))
    ? copyDirectoryTree(path.join(projectDir, 'assets'), path.join(outDir, 'assets'), false)
    : { count: 0, warnings: [] }
  const localesResult = existsSync(path.join(projectDir, 'locales'))
    ? copyDirectoryTree(path.join(projectDir, 'locales'), path.join(outDir, 'locales'), true)
    : { count: 0, warnings: [] }

  // Copy declared static files (e.g. theme.css, screenshot, icon) if present.
  for (const rel of [manifest.theme?.css, manifest.screenshot, manifest.icon]) {
    if (!rel) continue
    const cleanRel = path.posix.normalize(rel.replace(/^\.\//, ''))
    if (checkSafeRelPath(cleanRel, 'static') !== null) continue
    const absSrc = path.join(projectDir, ...cleanRel.split('/'))
    const absDst = path.join(outDir, ...cleanRel.split('/'))
    if (existsSync(absSrc) && !existsSync(absDst)) {
      const st = lstatSync(absSrc)
      if (st.isSymbolicLink() || !st.isFile()) {
        throw new Error(`Symbolic links or special files are not allowed in plugin assets ("${cleanRel}")`)
      }
      if (st.size > MAX_ASSET_FILE_BYTES) {
        throw new Error(`Static file "${cleanRel}" exceeds 10 MiB per-file limit (${st.size} bytes)`)
      }
      mkdirSync(path.dirname(absDst), { recursive: true })
      copyFileSync(absSrc, absDst)
    }
  }

  warnings.push(...assetsResult.warnings, ...localesResult.warnings)

  writeFileSync(path.join(outDir, 'plugin.json'), `${JSON.stringify(outManifest, null, 2)}\n`, 'utf8')

  if (!options.allowUncappedCombined) {
    let totalBuiltBytes = 0
    for (const rel of walkFiles(outDir)) {
      totalBuiltBytes += statSync(path.join(outDir, ...rel.split('/'))).size
    }
    const builtCap = isNative
      ? (nativeBinaries?.length ?? 0) > 1
        ? MAX_NATIVE_UNPACKED_FAT_BYTES
        : MAX_NATIVE_UNPACKED_SINGLE_BYTES
      : MAX_PACKAGE_TOTAL_BYTES
    if (totalBuiltBytes > builtCap) {
      throw new Error(
        `Total built output (${totalBuiltBytes} bytes) exceeds ${Math.round(builtCap / (1024 * 1024))} MiB package cap`,
      )
    }
  }

  return {
    outDir,
    manifest: outManifest,
    serverFile,
    serverBytes,
    nativeBinaries,
    webFile,
    webBytes,
    webCssFile,
    copiedAssets: assetsResult.count,
    copiedLocales: localesResult.count,
    warnings,
  }
}
