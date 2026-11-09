import { readFileSync } from 'node:fs'
import path from 'node:path'

import * as lucide from 'lucide-react'
import { describe, expect, it } from 'vitest'

import { menuIcons } from './icons'

const pascal = (kebab: string) => kebab.replace(/(^|-)([a-z0-9])/g, (_, __, c: string) => c.toUpperCase())

describe('menuIcons', () => {
  it('keys every icon by its lucide kebab-case name', () => {
    for (const [name, Icon] of Object.entries(menuIcons)) {
      expect(name).toMatch(/^[a-z0-9]+(-[a-z0-9]+)*$/)
      // The name must be lucide's own (renamed icons keep their old name as an alias), and
      // map to that very icon, not to a neighbour imported by mistake.
      expect(lucide[pascal(name) as keyof typeof lucide], name).toBe(Icon)
    }
  })

  it('has every icon the backend names in its menus', () => {
    const site = readFileSync(path.resolve(import.meta.dirname, '../../../../internal/httpapi/site.go'), 'utf8')
    const named = [...site.matchAll(/Icon:\s*"([^"]+)"/g)].map((m) => m[1])
    expect(named.length).toBeGreaterThan(0)
    for (const name of named) expect(menuIcons).toHaveProperty(name)
  })
})
