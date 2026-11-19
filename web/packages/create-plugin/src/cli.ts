import { PLUGIN_TEMPLATES, scaffoldPlugin, type PluginTemplate } from './index.js'

function usage(): string {
  return [
    'Usage: create-skinloom-plugin <directory> [options]',
    '',
    'Options:',
    `  --template <name>   ${PLUGIN_TEMPLATES.join(' | ')} (default: basic)`,
    '  --id <kebab-id>     Explicit plugin or theme ID',
    '  --name <label>      Display name',
    '  --force             Overwrite existing files in directory',
    '',
  ].join('\n')
}

const BOOLEAN_FLAGS = new Set(['force', 'help'])

export async function runCreateCli(argv: string[]): Promise<number> {
  const positionals: string[] = []
  const flags: Record<string, string | boolean> = {}
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]
    if (arg === '-h' || arg === '--help') {
      flags.help = true
      continue
    }
    if (arg.startsWith('--')) {
      const eq = arg.indexOf('=')
      if (eq !== -1) {
        const key = arg.slice(2, eq)
        const val = arg.slice(eq + 1)
        if (BOOLEAN_FLAGS.has(key)) {
          flags[key] = val !== 'false' && val !== '0'
        } else {
          if (!val) {
            process.stderr.write(`Error: Flag --${key} requires a non-empty value\n`)
            return 1
          }
          flags[key] = val
        }
      } else {
        const key = arg.slice(2)
        if (BOOLEAN_FLAGS.has(key)) {
          flags[key] = true
          continue
        }
        const next = argv[i + 1]
        if (next && !next.startsWith('-')) {
          flags[key] = next
          i++
        } else {
          process.stderr.write(`Error: Flag --${key} requires a value\n`)
          return 1
        }
      }
      continue
    }
    positionals.push(arg)
  }

  if (flags.help || positionals.length === 0) {
    process.stdout.write(usage())
    return flags.help ? 0 : 1
  }

  const template = (typeof flags.template === 'string' ? flags.template : 'basic') as PluginTemplate
  try {
    const res = scaffoldPlugin({
      targetDir: positionals[0],
      template,
      id: typeof flags.id === 'string' ? flags.id : undefined,
      name: typeof flags.name === 'string' ? flags.name : undefined,
      force: Boolean(flags.force),
    })
    process.stdout.write(`Created ${res.template} starter "${res.id}" in ${res.targetDir}\n`)
    for (const file of res.files) {
      process.stdout.write(`  + ${file}\n`)
    }
    return 0
  } catch (err) {
    process.stderr.write(`Error: ${err instanceof Error ? err.message : String(err)}\n`)
    return 1
  }
}
