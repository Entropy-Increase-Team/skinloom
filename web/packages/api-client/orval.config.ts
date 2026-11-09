import { defineConfig } from 'orval'

const input = { target: '../../../api/openapi.json' }
const mutator = { path: 'src/fetcher.ts', name: 'apiFetch' }
// One set of model types, shared by both outputs.
const schemas = { path: 'src/gen/model', type: 'typescript' } as const

export default defineConfig({
  // Plain fetch functions (@skinloom/api-client): server components and anything outside
  // React. Must not import React or TanStack Query: server components import it.
  skinloom: {
    input,
    output: {
      target: 'src/gen/api.ts',
      schemas,
      client: 'fetch',
      mode: 'single',
      clean: true,
      override: { mutator },
    },
  },
  // TanStack Query hooks (@skinloom/api-client/query) for client components (ADR 0042),
  // through the same transport.
  skinloomQuery: {
    input,
    output: {
      target: 'src/gen/query.ts',
      schemas,
      client: 'react-query',
      httpClient: 'fetch',
      mode: 'single',
      override: { mutator },
    },
  },
})
