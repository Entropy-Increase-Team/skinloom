// Judges a corpus of messages with @formatjs/icu-messageformat-parser (as next-intl
// parses them) for TestCheckMessageAgreesWithFormatJS. After `bun install` in web/,
// from the repository root:
//
//   bun internal/settings/testdata/icu-corpus.mjs > internal/settings/testdata/icu-corpus.json
import { parse } from '../../../web/node_modules/@formatjs/icu-messageformat-parser/index.js'

const handwritten = [
  '', 'Hello', 'Hello {name}', '{name}', '{ name }', '{}', '{', '}', 'a } b', '{name', '{name,}', '{name, }',
  '{n, number}', '{n, number, integer}', '{n, number, }', '{n,number,percent}', '{n, number, ::currency/CNY}',
  '{n, number, ::}', '{n, number, ::currency/}', '{d, date, short}', '{d, date, ::yyyyMMdd}', '{d, date, ::}', '{t, time}',
  '{n, plural, one {# item} other {# items}}', '{n, plural, one {x}}', '{n, plural, =0 {none} other {#}}',
  '{n, plural, offset:1 one {a} other {b}}', '{n, plural, offset:x other {b}}', '{n, plural, offset 1 other {b}}',
  '{n, plural other {b}}', '{n, plural, other {a} other {b}}', '{n, plural, =1 {a} =1 {b} other {c}}', '{n, plural, =x {a} other {b}}',
  '{n, plural, other {a}', '{n, plural, other {a}} tail', '{n, plural, other a}', '{n, plural,}', '{n, plural, other {{m} #}}',
  '{n, selectordinal, one {#st} two {#nd} few {#rd} other {#th}}', '{g, select, male {he} female {she} other {they}}',
  '{g, select, male {he}}', '{g, select, =1 {x} other {y}}', '{g, select, a {{n, plural, other {# x}}} other {z}}',
  '{g, select, a {#} other {#}}', '{n, foo}', '{n, Number}', '{a-b}', '{a_b}', '{名字}', '{名字。}', '{a b}', '{0}',
  "It''s", "'{name}'", "'{'name}", "'{name}", "'#'", "{n, plural, other {'#' is #}}", "a 'b' c", "''{x}''",
  '<b>bold</b>', '<b>bold', 'bold</b>', '<b>a</i>', '<b/>', '<b />', '< b>', '<1>', 'a < b', 'a <', '<', 'a > b', '<b>x</b >',
  '<b>x</ b>', '<b>x</', '<b>{n, plural, other {<i>#</i>}}</b>', '<b>{n, plural, other {x</b>}}', '<link>Docs</link> and <strong>more</strong>',
  '<a-b.c_1>x</a-b.c_1>', '<B>x</B>', '<b>x</B>', "'<b>'", "'<b>x</b>'", '{n, number, {a}}', '{n, number, \'x}', "{n, number, 'a'}",
  '{n, plural, one {a} other {b}} {m, select, x {y} other {z}}', 'A {a} and {b, plural, one {{c}} other {#}}', '{a}{b}', '{{a}}',
  '\u00a0{a}', '{\u00a0a}', '{a\u00a0}', '{\u3000a}', '{a\u3000}', '{n,\tplural,\tother\t{x}}', '{n, plural, one{a}other{b}}',
]

// Random strings over the characters that matter, to compare more than the cases above.
const alphabet = ['{', '}', '<', '>', '/', '#', "'", ',', ' ', '=', ':', 'a', 'b', 'n', 'other', 'plural', 'select', 'one', 'number', '1', 'x']
let seed = 20261105
const rand = (n) => {
  seed = (seed * 1103515245 + 12345) % 2147483648
  return seed % n
}
const random = []
for (let i = 0; i < 3000; i++) {
  let s = ''
  for (let len = 1 + rand(12); len > 0; len--) s += alphabet[rand(alphabet.length)]
  random.push(s)
}
// Shapes that are valid more often than random noise.
const templates = ['{a, plural, X {b} other {c}}', '<b>X</b>', '{a, select, X {b} other {c}}', 'X {a} X']
for (const t of templates) for (let i = 0; i < 300; i++) {
  let s = ''
  for (let len = 1 + rand(5); len > 0; len--) s += alphabet[rand(alphabet.length)]
  random.push(t.replace('X', s))
}

const seen = new Set()
const out = []
for (const text of [...handwritten, ...random]) {
  if (seen.has(text)) continue
  seen.add(text)
  let ok = true
  try {
    parse(text)
  } catch {
    ok = false
  }
  out.push({ text, ok })
}
console.log(JSON.stringify(out, null, 1))
