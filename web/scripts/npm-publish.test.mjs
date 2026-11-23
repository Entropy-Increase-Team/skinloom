import assert from 'node:assert/strict'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'

import {
  buildPublishManifest,
  CANONICAL_REPO_URL,
  defaultDistTag,
  PUBLISH_PACKAGES,
  validateStagedPackage,
} from './npm-publish.mjs'

test('defaultDistTag routes prereleases to next and stable semver to latest', () => {
  assert.equal(defaultDistTag('1.1.3'), 'latest')
  assert.equal(defaultDistTag('1.2.0-beta.1'), 'next')
  assert.equal(defaultDistTag('2.0.0-rc.1'), 'next')
})

test('buildPublishManifest strips private/devDependencies, pins workspace deps, and rewrites exports', () => {
  const cliSpec = PUBLISH_PACKAGES.find((p) => p.name === '@skinloom/cli')
  const manifest = buildPublishManifest(
    {
      name: '@skinloom/cli',
      version: '1.0.0',
      private: true,
      description: 'CLI',
      license: 'MIT',
      bin: { 'skinloom-plugin': './bin/skinloom-plugin.mjs' },
      dependencies: { '@skinloom/plugin-sdk': '*', esbuild: '^0.28.0' },
      devDependencies: { vitest: '^5.0.0' },
      scripts: { test: 'vitest run' },
    },
    cliSpec,
    '1.1.3',
  )

  assert.equal(manifest.version, '1.1.3')
  assert.equal(manifest.private, undefined)
  assert.equal(manifest.devDependencies, undefined)
  assert.equal(manifest.scripts, undefined)
  assert.equal(manifest.publishConfig.access, 'public')
  assert.equal(manifest.repository.url, CANONICAL_REPO_URL)
  assert.equal(manifest.repository.directory, 'web/packages/cli')
  assert.deepEqual(manifest.dependencies, {
    '@skinloom/plugin-sdk': '^1.1.3',
    esbuild: '^0.28.0',
  })
  assert.deepEqual(manifest.exports, {
    '.': { types: './dist/index.d.ts', import: './dist/index.js' },
  })
  assert.deepEqual(manifest.files, ['bin', 'dist', 'README.md', 'LICENSE'])
})

test('validateStagedPackage passes on complete staged dir and catches leaks or missing targets', () => {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'sl-npm-stage-test-'))
  try {
    mkdirSync(path.join(dir, 'dist', 'server'), { recursive: true })
    mkdirSync(path.join(dir, 'dist', 'web'), { recursive: true })
    mkdirSync(path.join(dir, 'dist', 'testing'), { recursive: true })
    mkdirSync(path.join(dir, 'dist', 'gen'), { recursive: true })
    for (const rel of [
      'README.md',
      'LICENSE',
      'package.json',
      'dist/index.js',
      'dist/index.d.ts',
      'dist/server/index.js',
      'dist/server/index.d.ts',
      'dist/web/index.js',
      'dist/web/index.d.ts',
      'dist/testing/index.js',
      'dist/testing/index.d.ts',
      'dist/gen/catalog.js',
      'dist/gen/catalog.d.ts',
    ]) {
      writeFileSync(path.join(dir, rel), '')
    }

    const sdkSpec = PUBLISH_PACKAGES.find((p) => p.name === '@skinloom/plugin-sdk')
    const valid = buildPublishManifest(
      {
        name: '@skinloom/plugin-sdk',
        version: '1.1.3',
        description: 'SDK',
        license: 'MIT',
        peerDependencies: { '@skinloom/ui': '*', react: '^19' },
      },
      sdkSpec,
      '1.1.3',
    )
    assert.deepEqual(validateStagedPackage(dir, valid), [])
    assert.equal(valid.peerDependencies['@skinloom/ui'], '^1.1.3')

    // Mutate to verify validator catches raw .ts leak, wildcard dep, and private flag
    const broken = {
      ...valid,
      private: true,
      dependencies: { '@skinloom/ui': '*' },
      exports: { '.': { types: './dist/index.d.ts', import: './src/index.ts' } },
    }
    const errs = validateStagedPackage(dir, broken)
    assert.ok(errs.some((e) => e.includes('private')))
    assert.ok(errs.some((e) => e.includes('unresolved workspace specifier')))
    assert.ok(errs.some((e) => e.includes('raw TypeScript source')))
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})
