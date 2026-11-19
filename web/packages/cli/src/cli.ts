import { buildPlugin } from './build.js'
import { devPlugin } from './dev.js'
import { lintPlugin } from './lint.js'
import { generateKeyPair, packPlugin, signPackage, verifyPackage } from './pack.js'

interface ParsedArgs {
  positionals: string[]
  flags: Record<string, string | boolean>
}

const BOOLEAN_FLAGS = new Set([
  'help',
  'minify',
  'sourcemap',
  'json',
  'skip-build',
  'once',
  'native',
  'split-platforms',
  'fat',
  'force',
])

function parseArgs(argv: string[]): ParsedArgs {
  const positionals: string[] = []
  const flags: Record<string, string | boolean> = {}
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === '-h' || arg === '--help') {
      flags.help = true
      continue
    }
    if (arg.startsWith('--')) {
      const eq = arg.indexOf('=')
      if (eq !== -1) {
        const key = arg.slice(2, eq)
        const val = arg.slice(eq + 1)
        if (BOOLEAN_FLAGS.has(key)) {
          flags[key] = val !== 'false' && val !== '0'
        } else {
          if (!val) {
            throw new Error(`Flag --${key} requires a non-empty value`)
          }
          flags[key] = val
        }
      } else {
        const key = arg.slice(2)
        if (BOOLEAN_FLAGS.has(key)) {
          flags[key] = true
          continue
        }
        const next = argv[i + 1]
        if (next && !next.startsWith('-')) {
          flags[key] = next
          i++
        } else {
          throw new Error(`Flag --${key} requires a value`)
        }
      }
      continue
    }
    positionals.push(arg)
  }
  return { positionals, flags }
}

function flagString(flags: Record<string, string | boolean>, key: string): string | undefined {
  const val = flags[key]
  return typeof val === 'string' ? val : undefined
}

function parseTargetsFlag(raw: string | undefined): string[] | undefined {
  if (!raw) return undefined
  const list = raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return list.length > 0 ? list : undefined
}

function usage(): string {
  return [
    'Usage: skinloom-plugin <command> [options]',
    '',
    'Commands:',
    '  build [--dir .] [--out dist] [--native] [--targets os/arch,...] [--minify] [--sourcemap]',
    '                                                          Bundle JS/native binaries, web/index.js & assets',
    '  dev   --url <origin> [--token <token>] [--dir .] [--once] Watch Go/TS/TSX, rebuild & hot-reload via admin API',
    '  lint  [--dir .] [--json]                                Validate plugin.json, hooks, permissions & sizes',
    '  pack  [--dir .] [--out <file.slp>] [--out-dir <dir>] [--key <priv.pem>] [--split-platforms | --fat]',
    '                                                          Build, hash checksums.txt & pack .slp archive(s)',
    '  sign  <archive.slp> --key <priv.pem> [--out <file.slp>] Sign an existing .slp archive with Ed25519',
    '  verify <archive.slp> [--pub <pub.pem>]                  Verify checksums.txt and optional Ed25519 signature',
    '  keygen [--out .] [--name signing]                       Generate an Ed25519 key pair',
    '',
  ].join('\n')
}

export async function runCli(argv: string[]): Promise<number> {
  let positionals: string[]
  let flags: Record<string, string | boolean>
  try {
    ;({ positionals, flags } = parseArgs(argv))
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    process.stderr.write(`Error: ${msg}\n`)
    return 1
  }
  const [command, target] = positionals

  if (!command || flags.help) {
    process.stdout.write(usage())
    return flags.help ? 0 : 1
  }

  try {
    switch (command) {
      case 'build': {
        const res = await buildPlugin({
          dir: flagString(flags, 'dir') ?? target ?? '.',
          outDir: flagString(flags, 'out') ?? 'dist',
          minify: Boolean(flags.minify),
          sourcemap: Boolean(flags.sourcemap),
          native: Boolean(flags.native),
          targets: parseTargetsFlag(flagString(flags, 'targets')),
        })
        if (flags.json) {
          process.stdout.write(`${JSON.stringify(res, null, 2)}\n`)
        } else {
          process.stdout.write(`Built ${res.manifest.id}@${res.manifest.version} -> ${res.outDir}\n`)
          if (res.serverFile) process.stdout.write(`  server: server.js (${res.serverBytes} bytes)\n`)
          for (const bin of res.nativeBinaries ?? []) {
            process.stdout.write(`  native [${bin.target}]: ${bin.relPath} (${bin.bytes} bytes)\n`)
          }
          if (res.webFile) process.stdout.write(`  web: web/index.js (${res.webBytes} bytes)\n`)
        }
        return 0
      }

      case 'lint': {
        const res = lintPlugin(flagString(flags, 'dir') ?? target ?? '.')
        if (flags.json) {
          process.stdout.write(`${JSON.stringify(res, null, 2)}\n`)
        } else {
          for (const issue of [...res.errors, ...res.warnings]) {
            const loc = issue.file ? ` (${issue.file})` : ''
            process.stderr.write(`${issue.severity.toUpperCase()} [${issue.code}]${loc}: ${issue.message}\n`)
          }
          if (res.ok) {
            process.stdout.write(`Lint passed (${res.warnings.length} warnings)\n`)
          }
        }
        return res.ok ? 0 : 1
      }

      case 'pack': {
        const res = await packPlugin({
          dir: flagString(flags, 'dir') ?? target ?? '.',
          outFile: flagString(flags, 'out'),
          outDir: flagString(flags, 'out-dir'),
          buildDir: flagString(flags, 'build-dir'),
          privateKeyFile: flagString(flags, 'key'),
          skipBuild: Boolean(flags['skip-build']),
          native: Boolean(flags.native),
          targets: parseTargetsFlag(flagString(flags, 'targets')),
          splitPlatforms: Boolean(flags['split-platforms']),
          fat: Boolean(flags.fat),
        })
        if (flags.json) {
          process.stdout.write(`${JSON.stringify(res, null, 2)}\n`)
        } else if (res.artifacts) {
          for (const [plat, item] of Object.entries(res.artifacts)) {
            process.stdout.write(
              `Packed [${plat}] ${item.archivePath} (${item.size} bytes${res.signed ? ', signed' : ''})\n`,
            )
          }
          if (res.artifactsManifestPath) {
            process.stdout.write(`Wrote artifacts manifest: ${res.artifactsManifestPath}\n`)
          }
        } else {
          process.stdout.write(
            `Packed ${res.archivePath} (${res.archiveBytes} bytes, ${res.fileCount} entries${res.signed ? ', signed' : ''})\n`,
          )
        }
        return 0
      }

      case 'sign': {
        const archivePath = target ?? flagString(flags, 'in')
        if (!archivePath) throw new Error('sign requires <archive.slp>')
        const res = signPackage({
          archivePath,
          privateKeyFile: flagString(flags, 'key'),
          outPath: flagString(flags, 'out'),
        })
        process.stdout.write(`Signed ${res.archivePath}\n`)
        return 0
      }

      case 'verify': {
        const archivePath = target ?? flagString(flags, 'in')
        if (!archivePath) throw new Error('verify requires <archive.slp>')
        const res = verifyPackage({
          archivePath,
          publicKeyFile: flagString(flags, 'pub'),
        })
        if (flags.json) {
          process.stdout.write(`${JSON.stringify(res, null, 2)}\n`)
        } else if (!res.ok) {
          for (const err of res.errors) {
            process.stderr.write(`ERROR: ${err}\n`)
          }
        } else {
          const sigNote = res.signed
            ? flagString(flags, 'pub')
              ? ', signature valid'
              : ', signed (unverified; pass --pub to verify signature)'
            : ''
          process.stdout.write(`Verified ${archivePath} (${res.fileCount} entries${sigNote})\n`)
        }
        return res.ok ? 0 : 1
      }

      case 'keygen': {
        const res = generateKeyPair(
          flagString(flags, 'out') ?? '.',
          flagString(flags, 'name') ?? 'signing',
          Boolean(flags.force),
        )
        if (flags.json) {
          process.stdout.write(`${JSON.stringify(res, null, 2)}\n`)
        } else {
          process.stdout.write(`Generated Ed25519 keys:\n  public:  ${res.publicKeyPath}\n  private: ${res.privateKeyPath}\n`)
        }
        return 0
      }

      case 'dev': {
        const url = flagString(flags, 'url')
        if (!url) throw new Error('dev requires --url <skinloom-origin>')
        const once = Boolean(flags.once)
        const session = await devPlugin({
          dir: flagString(flags, 'dir') ?? target ?? '.',
          outDir: flagString(flags, 'out') ?? 'dist',
          url,
          token: flagString(flags, 'token') ?? process.env.SKINLOOM_ADMIN_TOKEN,
          watch: !once,
          onReload(info) {
            const label = typeof info === 'object' && info && 'id' in info ? String((info as { id: unknown }).id) : 'plugin'
            process.stdout.write(`Reloaded ${label} -> ${url}\n`)
          },
          onError(err) {
            process.stderr.write(`Reload error: ${err.message}\n`)
          },
        })
        if (once) {
          await session.close()
          return 0
        }
        await new Promise<void>((resolve) => {
          const shutdown = (): void => {
            void session.close().finally(resolve)
          }
          process.once('SIGINT', shutdown)
          process.once('SIGTERM', shutdown)
        })
        return 0
      }

      default:
        process.stderr.write(`Unknown command: ${command}\n\n${usage()}`)
        return 1
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    process.stderr.write(`Error: ${msg}\n`)
    return 1
  }
}
