#!/usr/bin/env node
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const distEntry = path.join(root, 'dist', 'cli.js')
const srcEntry = path.join(root, 'src', 'cli.ts')
const entry = existsSync(distEntry) ? distEntry : srcEntry

const mod = await import(pathToFileURL(entry).href)
const exitCode = await mod.runCli(process.argv.slice(2))
process.exit(typeof exitCode === 'number' ? exitCode : 0)
