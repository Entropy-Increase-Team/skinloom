// TanStack Query hooks for every operation (ADR 0042), for client components only:
// importing this pulls in React and TanStack Query, which server components cannot load.
// Server components use the plain functions from '@skinloom/api-client'; the types are
// the same in both.
export * from './gen/query'
