import assert from 'node:assert/strict'
import test from 'node:test'

import { parseArgs, routeSlug } from './smoke.mjs'

test('parseArgs parses widths, themes, modes and flags', () => {
  const opts = parseArgs(['--url', 'http://127.0.0.1:9000/', '--widths', '375,1280', '--themes', 'default,brocade', '--not-found', '/404-check', '--measure'])
  assert.equal(opts.url, 'http://127.0.0.1:9000')
  assert.deepEqual(opts.widths, [375, 1280])
  assert.deepEqual(opts.themes, ['default', 'brocade'])
  assert.equal(opts.notFoundRoute, '/404-check')
  assert.equal(opts.measure, true)
})

test('routeSlug normalizes route paths into filenames', () => {
  assert.equal(routeSlug('/'), 'home')
  assert.equal(routeSlug('/skinlib/show/1'), 'skinlib_show_1')
})

test('parseArgs rejects invalid URLs, widths, modes and missing values', () => {
  assert.throws(() => parseArgs(['--url', 'ftp://example.com']), /url/i)
  assert.throws(() => parseArgs(['--widths', '375,abc']), /width/i)
  assert.throws(() => parseArgs(['--widths', '0']), /width/i)
  assert.throws(() => parseArgs(['--modes', 'sepia']), /mode/i)
  assert.throws(() => parseArgs(['--routes']), /missing value/i)
})
