import type { AdminReportView } from '@skinloom/api-client'
import { describe, expect, it } from 'vitest'

import { afterReview } from './reports-queue'

const texture = { id: 7, name: 'Skin' } as AdminReportView['texture']
const report = (id: number, t = texture): AdminReportView => ({ id, reason: 'x', status: 'pending', createdAt: '2026-10-29T00:00:00Z', texture: t })

describe('afterReview', () => {
  // Deleting a texture settles every pending report about it on the server; the queue
  // must not keep offering them (each click would only get a 409).
  it('drops the other pending reports of a deleted texture from the pending queue', () => {
    const list = [report(3), report(2), report(1, { ...texture!, id: 8 })]
    const updated: AdminReportView = { ...report(3, undefined), status: 'resolved', action: 'delete' }
    expect(afterReview(list, updated, 'delete', 'pending', 7).map((r) => r.id)).toEqual([1])
  })

  it('shows them settled, without the texture, in the other tabs', () => {
    const list = [report(3), report(2), { ...report(1), status: 'rejected' as const }]
    const updated: AdminReportView = { ...report(3, undefined), status: 'resolved', action: 'delete', handledAt: '2026-10-29T01:00:00Z' }
    const got = afterReview(list, updated, 'delete', '', 7)
    expect(got[0]).toBe(updated)
    expect(got[1]).toMatchObject({ id: 2, status: 'resolved', action: 'delete', texture: undefined, handledAt: updated.handledAt })
    expect(got[2]).toMatchObject({ id: 1, status: 'rejected', texture: undefined })
  })

  it('only touches the reviewed report otherwise', () => {
    const list = [report(3), report(2)]
    const updated: AdminReportView = { ...report(3), status: 'rejected', action: 'reject' }
    expect(afterReview(list, updated, 'reject', 'pending', 7).map((r) => r.id)).toEqual([2])
    expect(afterReview(list, updated, 'reject', '', 7)).toEqual([updated, list[1]])
  })
})
