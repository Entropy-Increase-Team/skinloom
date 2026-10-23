#!/usr/bin/env node
// Run a package.json script in every workspace package that defines it, with whichever
// package manager started us (bun, pnpm or npm). Root scripts call this instead of
// bun/pnpm-only flags such as --filter, so `bun run test`, `pnpm test` and `npm test`
// all work.
//
//   node scripts/ws.mjs <script> [--only <package-name>] [-- extra args]
import { spawnSync } from 'node:child_process'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

/** The package manager that invoked us; bun first, then pnpm, then npm. */
export function detectPackageManager(userAgent = process.env.npm_config_user_agent ?? '') {
  const name = userAgent.split('/')[0]
  return ['bun', 'pnpm', 'npm'].includes(name) ? name : 'npm'
}

/**
 * The executable and leading args that start package manager pm without a shell.
 * Package managers export npm_execpath for scripts they run (bun: its binary; pnpm and
 * npm: a JS entry run with node), so we reuse exactly the one that started us. A shell
 * would join the args unquoted, and Node 24 deprecates passing args with shell: true.
 */
export function pmCommand(pm, env = process.env, platform = process.platform, node = process.execPath) {
  const exec = env.npm_execpath
  if (exec && path.win32.basename(exec).toLowerCase().startsWith(pm)) { // win32: splits on / and \
    return /\.[cm]?js$/i.test(exec) ? [node, [exec]] : [exec, []]
  }
  if (pm === 'npm' && platform === 'win32') {
    // run directly with node: npm is npm.cmd here, which cannot start without a shell
    return [node, [path.join(path.dirname(node), 'node_modules', 'npm', 'bin', 'npm-cli.js')]]
  }
  return [pm, []]
}

/** argv to run `script` with extra args under package manager pm. */
export function runArgs(pm, script, extra = []) {
  // npm needs "--" before arguments meant for the script; bun and pnpm pass them through.
  return pm === 'npm' && extra.length > 0 ? ['run', script, '--', ...extra] : ['run', script, ...extra]
}

/** Workspace package directories in dependency order: libraries before apps. */
export function workspaceDirs(base = root) {
  const dirs = []
  for (const group of ['packages', 'apps']) {
    const groupDir = path.join(base, group)
    if (!existsSync(groupDir)) continue
    for (const entry of readdirSync(groupDir, { withFileTypes: true })) {
      if (entry.isDirectory() && existsSync(path.join(groupDir, entry.name, 'package.json'))) {
        dirs.push(path.join(groupDir, entry.name))
      }
    }
  }
  return dirs
}

function main(argv) {
  const sep = argv.indexOf('--')
  const own = sep >= 0 ? argv.slice(0, sep) : argv
  const extra = sep >= 0 ? argv.slice(sep + 1) : []
  const [script, ...flags] = own
  if (!script) {
    console.error('usage: node scripts/ws.mjs <script> [--only <package>] [-- args]')
    return 2
  }
  const only = flags[0] === '--only' ? flags[1] : undefined
  const pm = detectPackageManager()
  const [file, lead] = pmCommand(pm)

  let ran = 0
  for (const dir of workspaceDirs()) {
    const pkg = JSON.parse(readFileSync(path.join(dir, 'package.json'), 'utf8'))
    if (only && pkg.name !== only) continue
    if (!pkg.scripts?.[script]) continue
    console.log(`\n> ${pkg.name} › ${pm} run ${script}`)
    const res = spawnSync(file, [...lead, ...runArgs(pm, script, extra)], { cwd: dir, stdio: 'inherit' })
    if (res.error) {
      console.error(`cannot start ${pm}: ${res.error.message}`)
      return 1
    }
    if (res.status !== 0) return res.status ?? 1
    ran++
  }
  if (ran === 0) {
    console.error(`no workspace package${only ? ` named ${only}` : ''} defines "${script}"`)
    return 1
  }
  return 0
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(main(process.argv.slice(2)))
}
