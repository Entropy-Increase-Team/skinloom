import { existsSync, readFileSync, watch, type FSWatcher } from 'node:fs'
import path from 'node:path'
import { buildPlugin } from './build.js'
import { lintPlugin } from './lint.js'
import { currentHostPlatform, effectiveRuntime, loadManifest, walkFiles } from './manifest.js'

export interface PushDevReloadOptions {
  url: string
  token?: string
  distDir: string
  fetchImpl?: typeof fetch
}

export interface DevOptions {
  dir?: string
  outDir?: string
  url: string
  token?: string
  watch?: boolean
  debounceMs?: number
  fetchImpl?: typeof fetch
  onReload?: (info: unknown) => void
  onError?: (err: Error) => void
}

export interface DevSession {
  initial: unknown
  close(): Promise<void>
}

/** Uploads an unpacked built directory (`dist/`) to `POST /api/v1/admin/plugins/dev-reload`. */
export async function pushDevReload(options: PushDevReloadOptions): Promise<unknown> {
  const distDir = path.resolve(options.distDir)
  const manifestPath = path.join(distDir, 'plugin.json')
  if (!existsSync(manifestPath)) {
    throw new Error(`Missing built plugin.json in ${distDir}`)
  }
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8')) as Record<string, unknown>

  const serverPath = path.join(distDir, 'server.js')
  const serverJs = existsSync(serverPath) ? readFileSync(serverPath, 'utf8') : ''

  const files: Record<string, string> = {}
  for (const rel of walkFiles(distDir)) {
    if (rel === 'plugin.json' || rel === 'server.js') continue
    const abs = path.join(distDir, ...rel.split('/'))
    files[rel] = readFileSync(abs).toString('base64')
  }

  const endpoint = `${options.url.replace(/\/+$/, '')}/api/v1/admin/plugins/dev-reload`
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-SkinLoom-Request': '1',
  }
  if (options.token) {
    headers['Authorization'] = `Bearer ${options.token}`
  }

  const isNative = manifest.runtime === 'native'
  const maxPayloadBytes = isNative ? 96 * 1024 * 1024 : 20 * 1024 * 1024
  const payload = JSON.stringify({ manifest, serverJs, files })
  if (Buffer.byteLength(payload, 'utf8') > maxPayloadBytes) {
    throw new Error(`dev-reload payload exceeds ${Math.round(maxPayloadBytes / (1024 * 1024))} MiB limit`)
  }

  const fetchFn = options.fetchImpl ?? globalThis.fetch
  const resp = await fetchFn(endpoint, {
    method: 'POST',
    headers,
    body: payload,
  })
  const text = await resp.text()
  let parsed: unknown = text
  try {
    parsed = text ? JSON.parse(text) : {}
  } catch {
    // keep raw text fallback on non-JSON server errors
  }
  if (!resp.ok) {
    throw new Error(`dev-reload failed (${resp.status}): ${typeof parsed === 'string' ? parsed : JSON.stringify(parsed)}`)
  }
  return parsed
}

/** Runs lint + build + hot-reload push, optionally watching source files for rebuilds. */
export async function devPlugin(options: DevOptions): Promise<DevSession> {
  const projectDir = path.resolve(options.dir ?? '.')
  const outDirRel = options.outDir ?? 'dist'
  const outDirAbs = path.resolve(projectDir, outDirRel)

  const reloadOnce = async (): Promise<unknown> => {
    const lint = lintPlugin(projectDir, outDirRel)
    if (!lint.ok) {
      const msg = lint.errors.map((e) => `[${e.code}] ${e.message}`).join('; ')
      throw new Error(`Plugin lint failed: ${msg}`)
    }
    const { manifest } = loadManifest(projectDir)
    const isNative = effectiveRuntime(manifest) === 'native'
    await buildPlugin({
      dir: projectDir,
      outDir: outDirRel,
      sourcemap: true,
      native: isNative,
      targets: isNative ? [currentHostPlatform()] : undefined,
    })
    const info = await pushDevReload({
      url: options.url,
      token: options.token,
      distDir: outDirAbs,
      fetchImpl: options.fetchImpl,
    })
    options.onReload?.(info)
    return info
  }

  const initial = await reloadOnce()
  if (options.watch === false) {
    return {
      initial,
      async close() {},
    }
  }

  const debounceMs = options.debounceMs ?? 200
  let timer: ReturnType<typeof setTimeout> | null = null
  let running = false
  let pending = false
  let closed = false
  let activeRunPromise: Promise<void> | null = null

  const trigger = (): void => {
    if (closed) return
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      void runQueued()
    }, debounceMs)
  }

  const runQueued = async (): Promise<void> => {
    if (closed) return
    if (running) {
      pending = true
      return
    }
    running = true
    const p = (async () => {
      try {
        await reloadOnce()
      } catch (err) {
        options.onError?.(err instanceof Error ? err : new Error(String(err)))
      } finally {
        running = false
        activeRunPromise = null
        if (pending && !closed) {
          pending = false
          trigger()
        }
      }
    })()
    activeRunPromise = p
    await p
  }

  const isCaseInsensitive = process.platform === 'win32' || process.platform === 'darwin'
  const normCase = (s: string) => (isCaseInsensitive ? s.toLowerCase() : s)
  const outRel = normCase(path.relative(projectDir, outDirAbs).replaceAll('\\', '/').replace(/\/+$/, ''))
  const watcher: FSWatcher = watch(projectDir, { recursive: true }, (_event, filename) => {
    if (!filename || closed) return
    const norm = normCase(filename.replaceAll('\\', '/'))
    if (
      (outRel && (norm === outRel || norm.startsWith(`${outRel}/`))) ||
      norm === 'node_modules' ||
      norm.startsWith('node_modules/') ||
      norm === '.git' ||
      norm.startsWith('.git/') ||
      norm.endsWith('.slp')
    ) {
      return
    }
    trigger()
  })
  watcher.on('error', (err) => {
    options.onError?.(err instanceof Error ? err : new Error(String(err)))
  })

  return {
    initial,
    async close() {
      closed = true
      pending = false
      if (timer) clearTimeout(timer)
      watcher.close()
      if (activeRunPromise) {
        await activeRunPromise.catch(() => undefined)
      }
    },
  }
}
