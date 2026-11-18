import type {
  DBConfigInput,
  MailConfigInput,
  SetupState,
  StorageConfigInput,
} from '@skinloom/api-client'

import { problemOf } from '@/lib/problem'

export type SetupMode = 'fresh' | 'bs' | 'restore'
export type WizardStep = 'gate' | 'mode' | 'infra' | 'branch'

export function setupErrorMessage(err: unknown, fallback: string): string {
  const p = problemOf(err)
  const fieldMsgs = Object.values(p.fields).filter(Boolean)
  if (fieldMsgs.length > 0) {
    return fieldMsgs.join('; ')
  }
  if (p.detail && p.detail !== 'invalid setup input') {
    return p.detail
  }
  return fallback
}

export function initialDBConfig(state: SetupState): DBConfigInput {
  if (state.envLocked.database) {
    return { useEnv: true }
  }
  const driver =
    state.database.driver === 'mysql' || state.database.driver === 'postgres'
      ? state.database.driver
      : 'sqlite'
  if (driver === 'mysql') {
    return {
      driver: 'mysql',
      host: state.database.host || '127.0.0.1',
      port: state.database.port || 3306,
      database: state.database.database || 'skinloom',
      username: 'skinloom',
      password: '',
    }
  }
  if (driver === 'postgres') {
    return {
      driver: 'postgres',
      host: state.database.host || '127.0.0.1',
      port: state.database.port || 5432,
      database: state.database.database || 'skinloom',
      username: 'skinloom',
      password: '',
      sslMode: 'disable',
    }
  }
  return {
    driver: 'sqlite',
    sqlitePath: state.database.sqlitePath || 'skinloom.db',
  }
}

export function initialStorageConfig(state: SetupState): StorageConfigInput {
  if (state.envLocked.storage) {
    return { useEnv: true }
  }
  if (state.storage.driver === 's3') {
    return {
      driver: 's3',
      endpoint: state.storage.endpoint || '',
      bucket: state.storage.bucket || '',
      region: state.storage.region || 'us-east-1',
      prefix: state.storage.prefix || '',
      publicUrl: state.storage.publicUrl || '',
      pathStyle: state.storage.pathStyle ?? false,
      insecure: state.storage.insecure ?? false,
    }
  }
  return {
    driver: 'local',
    path: state.storage.path || 'textures',
  }
}

export function initialMailConfig(): MailConfigInput {
  return {
    driver: 'none',
    port: 587,
    encryption: 'starttls',
  }
}

export function formatSetupBytes(n: number, locale: string): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = Math.max(0, n)
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: i === 0 ? 0 : 1 }).format(v)} ${units[i]}`
}
