import { ApiError } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { formatSetupBytes, initialDBConfig, initialStorageConfig, setupErrorMessage } from './types'

describe('setup wizard helpers', () => {
  it('extracts field-level validation messages on 422 instead of generic invalid setup input', () => {
    const err422 = new ApiError(422, {
      status: 422,
      title: 'Unprocessable Entity',
      detail: 'invalid setup input',
      errors: [
        { location: 'body.bridgeUrl', message: 'bridge URL must use https://' },
        { location: 'body.bridgeToken', message: 'bridge Bearer token is required' },
      ],
    })
    expect(setupErrorMessage(err422, 'fallback')).toBe(
      'bridge URL must use https://; bridge Bearer token is required',
    )

    const errGeneric422 = new ApiError(422, {
      status: 422,
      title: 'Unprocessable Entity',
      detail: 'invalid setup input',
    })
    expect(setupErrorMessage(errGeneric422, 'custom fallback')).toBe('custom fallback')

    const err500 = new ApiError(500, {
      status: 500,
      title: 'Internal Server Error',
      detail: 'upstream database timed out',
    })
    expect(setupErrorMessage(err500, 'fallback')).toBe('upstream database timed out')
  })

  it('builds initial DB and storage configs respecting env locks', () => {
    const baseState = {
      needsSetup: true,
      inContainer: false,
      envLocked: {
        database: true,
        storage: false,
        trustedProxies: false,
      },
      database: { driver: 'postgres', host: 'db.internal', port: 5432, database: 'sl' },
      storage: { driver: 's3', endpoint: 's3.example.com', bucket: 'skins', region: 'auto', pathStyle: true },
      trustedProxies: [],
      suggestedSiteUrl: 'https://skins.example.com',
      defaultTimezone: 'Asia/Shanghai',
      maxUploadBytes: 1073741824,
    }
    expect(initialDBConfig(baseState)).toEqual({ useEnv: true })
    expect(initialStorageConfig(baseState)).toEqual({
      driver: 's3',
      endpoint: 's3.example.com',
      bucket: 'skins',
      region: 'auto',
      prefix: '',
      publicUrl: '',
      pathStyle: true,
      insecure: false,
    })
  })

  it('formats byte counts cleanly with locale numbers', () => {
    expect(formatSetupBytes(0, 'en')).toBe('0 B')
    expect(formatSetupBytes(1024 * 1024 * 1024, 'en')).toBe('1 GB')
  })
})
