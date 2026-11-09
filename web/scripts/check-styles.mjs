#!/usr/bin/env node
// Components may only use the design tokens for colour (web/design.md §2.3, §13): a
// theme changes every colour on the site through them. This refuses Tailwind palette
// colours (text-slate-900, bg-white, border-emerald-500/40 …) and arbitrary colour
// values (bg-[#fff], text-[rgb(…)]) in the app and the shared UI package.
//
//   node scripts/check-styles.mjs            exit 1 and list offending lines
import { readdirSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const DIRS = ['apps/web/src', 'packages/ui/src']
const EXT = /\.(tsx?|css)$/

const PREFIX = '(?:bg|text|border(?:-[trblxy])?|ring|ring-offset|fill|stroke|from|via|to|outline|shadow|decoration|divide|placeholder|caret|accent)'
const PALETTE = 'slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose'

/** Colour classes that bypass the tokens. */
export const FORBIDDEN = [
  new RegExp(`(?<![\\w-])${PREFIX}-(?:(?:${PALETTE})-\\d{2,3}|white|black)(?![\\w-])`, 'g'),
  new RegExp(`(?<![\\w-])${PREFIX}-\\[(?:#|rgb|hsl|oklch|oklab|lab|lch|color)`, 'g'),
]

/** The offending class names in one file's text, with 1-based line numbers. */
export function findViolations(text) {
  const out = []
  text.split('\n').forEach((line, i) => {
    for (const re of FORBIDDEN) for (const m of line.matchAll(re)) out.push({ line: i + 1, match: m[0] })
  })
  return out
}

function* files(dir) {
  for (const entry of readdirSync(dir)) {
    const p = path.join(dir, entry)
    if (statSync(p).isDirectory()) yield* files(p)
    else if (EXT.test(entry) && !/\.test\.tsx?$/.test(entry)) yield p
  }
}

function main() {
  let bad = 0
  for (const dir of DIRS) {
    for (const file of files(path.join(root, dir))) {
      for (const v of findViolations(readFileSync(file, 'utf8'))) {
        console.error(`${path.relative(root, file)}:${v.line}: ${v.match} — use a design token (web/design.md §2)`)
        bad++
      }
    }
  }
  if (bad > 0) {
    console.error(`\n${bad} colour class(es) outside the design tokens`)
    return 1
  }
  console.log('styles: only design tokens used')
  return 0
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exit(main())
