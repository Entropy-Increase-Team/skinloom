import assert from 'node:assert/strict'
import { test } from 'node:test'

import { findViolations } from './check-styles.mjs'

test('palette and arbitrary colours are refused', () => {
  const hits = findViolations(
    [
      '<p className="text-white bg-slate-900 border-emerald-500/40">',
      '<i className="hover:bg-white/10 dark:text-amber-500 ring-offset-black">',
      '<b className="bg-[#fff] text-[rgb(0,0,0)] border-x-red-600">',
    ].join('\n'),
  ).map((v) => `${v.line}:${v.match}`)
  assert.deepEqual(hits, [
    '1:text-white',
    '1:bg-slate-900',
    '1:border-emerald-500',
    '2:bg-white',
    '2:text-amber-500',
    '2:ring-offset-black',
    '3:border-x-red-600',
    '3:bg-[#',
    '3:text-[rgb',
  ])
})

test('tokens and look-alikes pass', () => {
  const ok = 'text-primary-foreground bg-muted/40 border-border text-success dark:text-warning bg-(--pixel-a) text-whitespace sl-white-box shadow-(--shadow-popover)'
  assert.deepEqual(findViolations(ok), [])
})
