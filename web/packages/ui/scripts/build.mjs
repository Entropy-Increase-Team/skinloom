#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
import { copyFileSync, mkdirSync, rmSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const pkgRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const wsRoot = path.resolve(pkgRoot, '../..')
const distDir = path.join(pkgRoot, 'dist')

const require = createRequire(import.meta.url)
const esbuild = require(require.resolve('esbuild', { paths: [pkgRoot, wsRoot] }))
const tscBin = require.resolve('typescript/bin/tsc', { paths: [pkgRoot, wsRoot] })

rmSync(distDir, { recursive: true, force: true })

await esbuild.build({
  absWorkingDir: pkgRoot,
  entryPoints: ['src/index.ts', 'src/components/ui/chart.tsx'],
  outbase: 'src',
  outdir: 'dist',
  bundle: true,
  format: 'esm',
  jsx: 'automatic',
  packages: 'external',
  sourcemap: true,
  target: ['es2022'],
  banner: {
    js: "'use client';",
  },
})

execFileSync(process.execPath, [tscBin, '-p', path.join(pkgRoot, 'tsconfig.build.json')], {
  cwd: pkgRoot,
  stdio: 'inherit',
})

mkdirSync(path.join(distDir, 'styles'), { recursive: true })
copyFileSync(path.join(pkgRoot, 'src', 'styles', 'globals.css'), path.join(distDir, 'styles', 'globals.css'))
