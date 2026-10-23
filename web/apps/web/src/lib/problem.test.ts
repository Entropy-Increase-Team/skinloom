import { ApiError } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { problemOf } from './problem'

describe('problemOf', () => {
  it('maps validation details to form fields', () => {
    const err = new ApiError(422, {
      detail: 'invalid input',
      errors: [
        { location: 'body.email', message: 'not a valid email address' },
        { location: 'body.password', message: 'must be 8 to 128 characters' },
        { location: 'body.email', message: 'second message is ignored' },
        { location: 'query.x', message: 'not a body field' },
      ],
    })
    expect(problemOf(err)).toEqual({
      status: 422,
      detail: 'invalid input',
      fields: { email: 'not a valid email address', password: 'must be 8 to 128 characters' },
    })
  })

  it('treats network failures as status 0', () => {
    expect(problemOf(new TypeError('fetch failed'))).toEqual({ status: 0, fields: {} })
  })
})
