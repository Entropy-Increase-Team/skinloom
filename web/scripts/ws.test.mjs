import assert from 'node:assert/strict'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { detectPackageManager, pmCommand, runArgs, workspaceDirs } from './ws.mjs'

test('detects the invoking package manager', () => {
  assert.equal(detectPackageManager('bun/1.3.10 npm/? node/v24.22.1 win32 x64'), 'bun')
  assert.equal(detectPackageManager('pnpm/10.20.0 npm/? node/v24.22.1 linux x64'), 'pnpm')
  assert.equal(detectPackageManager('npm/11.6.0 node/v24.22.1 darwin arm64'), 'npm')
  assert.equal(detectPackageManager('yarn/4.0.0 npm/? node/v24'), 'npm')
  assert.equal(detectPackageManager(''), 'npm')
})

test('passes extra args the way each package manager expects', () => {
  assert.deepEqual(runArgs('bun', 'dev', ['--port', '3000']), ['run', 'dev', '--port', '3000'])
  assert.deepEqual(runArgs('pnpm', 'dev', ['--port', '3000']), ['run', 'dev', '--port', '3000'])
  assert.deepEqual(runArgs('npm', 'dev', ['--port', '3000']), ['run', 'dev', '--', '--port', '3000'])
  assert.deepEqual(runArgs('npm', 'test'), ['run', 'test'])
})

test('starts the package manager without a shell', () => {
  const node = '/opt/node/bin/node'
  // the JS entries pnpm and npm export are run with node; bun exports its own binary
  assert.deepEqual(pmCommand('pnpm', { npm_execpath: '/opt/pnpm/bin/pnpm.cjs' }, 'linux', node), [node, ['/opt/pnpm/bin/pnpm.cjs']])
  assert.deepEqual(pmCommand('npm', { npm_execpath: '/opt/npm/bin/npm-cli.js' }, 'linux', node), [node, ['/opt/npm/bin/npm-cli.js']])
  assert.deepEqual(pmCommand('bun', { npm_execpath: '/home/u/.bun/bin/bun' }, 'linux', node), ['/home/u/.bun/bin/bun', []])
  assert.deepEqual(pmCommand('bun', { npm_execpath: 'C:\\bun\\bun.exe' }, 'win32', node), ['C:\\bun\\bun.exe', []])
  // an inherited npm_execpath from another package manager is ignored
  assert.deepEqual(pmCommand('pnpm', { npm_execpath: '/opt/npm/bin/npm-cli.js' }, 'linux', node), ['pnpm', []])
  // run directly with node: npm on Windows is npm.cmd, which needs a shell, so use npm-cli.js
  const winNode = path.join('C:', 'node', 'node.exe')
  assert.deepEqual(pmCommand('npm', {}, 'win32', winNode), [winNode, [path.join('C:', 'node', 'node_modules', 'npm', 'bin', 'npm-cli.js')]])
  assert.deepEqual(pmCommand('npm', {}, 'linux', node), ['npm', []])
})

test('lists libraries before apps', () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const names = workspaceDirs(root).map((d) => path.relative(root, d).split(path.sep).join('/'))
  assert.ok(names.includes('apps/web') && names.includes('packages/ui') && names.includes('packages/api-client'))
  assert.ok(names.indexOf('packages/ui') < names.indexOf('apps/web'))
})
