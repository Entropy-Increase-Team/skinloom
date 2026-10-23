import { defineConfig } from 'orval'

export default defineConfig({
  skinloom: {
    input: { target: '../../../api/openapi.json' },
    output: {
      target: 'src/gen/api.ts',
      client: 'fetch',
      mode: 'single',
      clean: true,
      override: {
        mutator: { path: 'src/fetcher.ts', name: 'apiFetch' },
      },
    },
  },
})
