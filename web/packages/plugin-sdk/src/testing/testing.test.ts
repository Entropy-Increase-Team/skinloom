import { describe, expect, it } from 'vitest'

import { definePlugin, reject } from '../server/index.js'
import { createTestHost } from './index.js'

const invitePlugin = definePlugin({
  setup(ctx) {
    const codes = ctx.db.collection<{ code: string; usedBy?: number | null }>('codes')

    ctx.hooks.guard('auth.register', async ({ form }) => {
      if (!(await ctx.settings.get<boolean>('enabled'))) return
      const code = String(form?.invite_code ?? '').trim()
      if (!code) return reject('invite_required')
      const hit = await codes.findOne({ code, usedBy: null })
      if (!hit) return reject('invite_invalid')
    })

    ctx.events.on('user.registered', async ({ userId, form }) => {
      const code = String(form?.invite_code ?? '').trim()
      if (!code) return
      await codes.updateOne({ code }, { usedBy: userId })
      await ctx.score.apply(userId, 50, 'invite.bonus')
      await ctx.notify.send(userId, 'Welcome', 'Redeemed code ' + code)
    })

    ctx.hooks.filter('user.badges', (badges, { userId }) => {
      if (userId === 7) return [...badges, { text: 'Early', color: 'highlight' }]
      return badges
    })

    ctx.http.post('/codes', { auth: 'admin' }, async (req) => {
      const code = String((req.body as { code?: string } | null)?.code ?? (await ctx.crypto.randomString(8)))
      const doc = await codes.insertOne({ code, usedBy: null })
      return { status: 201, body: doc }
    })

    ctx.http.get('/external', { auth: 'public' }, async () => {
      const res = await ctx.fetch('https://api.example.com/status')
      return res.json()
    })
  },
})

describe('@skinloom/plugin-sdk createTestHost', () => {
  it('runs guards, events, filters, http routes, score, notify, and unique index enforcement', async () => {
    const host = createTestHost({
      manifest: {
        id: 'invitation-codes',
        permissions: ['storage.docs', 'score.write', 'notify.send', 'http.fetch'],
        http: { allow: ['api.example.com'] },
        hooks: {
          guard: ['auth.register'],
          filter: ['user.badges'],
          event: ['user.registered'],
        },
        storage: {
          collections: {
            codes: {
              indexes: { code: 'string:unique', usedBy: 'number' },
            },
          },
        },
      },
      settings: { enabled: true },
      fixtures: {
        users: [{ id: 7, email: 'alice@example.com', score: 100 }],
      },
    })

    await host.setup(invitePlugin)
    host.fetchMock.mockJson('https://api.example.com/status', { healthy: true })

    // Unauth POST blocked with 401
    const unauth = await host.request('POST', '/codes', { body: { code: 'SKIN-1' } })
    expect(unauth.status).toBe(401)

    // Admin POST creates code
    const created = await host.request('POST', '/codes', {
      body: { code: 'SKIN-1' },
      user: { id: 1, email: 'admin@example.com', nickname: 'Admin', role: 'admin' },
    })
    expect(created.status).toBe(201)

    // Duplicate unique key rejected
    await expect(
      host.request('POST', '/codes', {
        body: { code: 'SKIN-1' },
        user: { id: 1, email: 'admin@example.com', nickname: 'Admin', role: 'admin' },
      }),
    ).rejects.toThrow(/unique constraint/)

    // Missing / bad code rejected by guard
    expect(await host.guard('auth.register', { email: 'a@b.c', nickname: 'A', form: {} })).toEqual({
      ok: false,
      rejected: true,
      reason: 'invite_required',
    })

    // Valid code passes guard & redeems via user.registered event
    expect(
      await host.guard('auth.register', { email: 'a@b.c', nickname: 'A', form: { invite_code: 'SKIN-1' } }),
    ).toEqual({ ok: true })
    await host.emit('user.registered', { userId: 7, method: 'password', form: { invite_code: 'SKIN-1' } })
    expect(await host.ctx.score.balance(7)).toBe(150)
    expect(host.notifications).toHaveLength(1)

    // Reusing redeemed code fails guard
    expect(
      await host.guard('auth.register', { email: 'd@e.f', nickname: 'D', form: { invite_code: 'SKIN-1' } }),
    ).toEqual({
      ok: false,
      rejected: true,
      reason: 'invite_invalid',
    })

    // Filter hook works
    expect(await host.filter('user.badges', [], { userId: 7, role: 'user' })).toEqual([
      { text: 'Early', color: 'highlight' },
    ])

    // fetchMock works and enforces allowlist
    const ext = await host.request('GET', '/external')
    expect(ext.status).toBe(200)
    expect(ext.body).toEqual({ healthy: true })
    await expect(host.ctx.fetch('https://blocked.example/x')).rejects.toThrow(/not in http\.allow/)

    // Disabling setting bypasses guard
    await host.updateSettings({ enabled: false })
    expect(await host.guard('auth.register', { email: 'x@y.z', nickname: 'X', form: {} })).toEqual({ ok: true })
  })

  it('aligns docs, files, textures, closet, yggdrasil, services, and HTTP routes with Go runtime semantics', async () => {
    const host = createTestHost({
      manifest: {
        id: 'parity-plugin',
        permissions: ['storage.docs', 'storage.files', 'textures.read', 'closet.write', 'yggdrasil.extend'],
        storage: {
          collections: {
            items: {
              indexes: {
                code: 'string:unique',
                score: 'number',
                createdAt: 'time',
              },
            },
          },
        },
        services: ['declaredSvc'],
      },
      fixtures: {
        players: [{ id: 1, userId: 1, name: 'Steve', uuid: 'uuid-steve' }],
        textures: [{ id: 1, name: 'Classic', type: 'steve' }],
      },
    })

    await host.setup(
      definePlugin({
        setup(ctx) {
          ctx.services.expose('declaredSvc', () => 'ok')
          ctx.http.get('/items/:id', (req) => ({
            id: req.params.id,
            body: 'article content',
            hdrs: req.headers,
          }))
          ctx.http.post('/webhook', (req) => ({
            status: 201,
            headers: {
              'X-Custom': 'yes',
              'Set-Cookie': 'evil=1',
              'Content-Security-Policy': "default-src 'none'",
            },
            body: { rawBody: req.rawBody, parsed: req.body },
          }))
        },
      }),
    )

    // files.get returns null when absent
    expect(await host.ctx.files.get('missing.txt')).toBeNull()
    await host.ctx.files.put('hello.txt', 'abc')
    expect(new TextDecoder().decode((await host.ctx.files.get('hello.txt'))!)).toBe('abc')

    // docs filter supports id, $eq, string/ISO time comparisons, and atomic updateMany
    const col = host.ctx.db.collection<{ code: string; score?: number; createdAt: string }>('items')
    const d1 = await col.insertOne({ code: 'A', score: 10, createdAt: '2026-01-01T00:00:00.000Z' })
    const d2 = await col.insertOne({ code: 'B', createdAt: '2026-06-01T00:00:00.000Z' })
    expect((await col.findOne({ id: d1.id }))?.code).toBe('A')
    expect((await col.find({ code: { $eq: 'B' } })).items).toHaveLength(1)
    expect((await col.find({ createdAt: { $gte: '2026-03-01T00:00:00.000Z' } })).items.map((x) => x.id)).toEqual([d2.id])
    // null field does not match numeric $gte: 0
    expect((await col.find({ score: { $gte: 0 } })).items.map((x) => x.code)).toEqual(['A'])
    // atomic updateMany fails without mutating any matched row when unique constraint collides
    await expect(col.updateMany({}, { code: 'COLLIDE' })).rejects.toThrow(/unique constraint/)
    expect((await col.findOne({ id: d1.id }))?.code).toBe('A')
    expect((await col.findOne({ id: d2.id }))?.code).toBe('B')

    // textures.readBytes throws on missing bytes; closet uses monotonic IDs
    await expect(host.ctx.textures.readBytes(999)).rejects.toThrow(/not found/)
    const c1 = await host.ctx.closet.add(1, 1, 'First')
    const c2 = await host.ctx.closet.add(1, 1, 'Second')
    expect(c2.item.id).toBeGreaterThan(c1.item.id)

    // yggdrasil.lookupProfile returns profile DTO
    expect(await host.ctx.yggdrasil.lookupProfile('uuid-steve')).toEqual({
      id: 'uuid-steve',
      name: 'Steve',
      properties: [],
    })

    // services.expose rejects undeclared services
    expect(() => host.ctx.services.expose('undeclared', () => null)).toThrow(/not declared/)

    // HTTP route parity: 405 on wrong method, non-envelope object with body returned verbatim, header filtering & cookie/CSP stripping
    expect((await host.request('POST', '/items/42')).status).toBe(405)
    const getRes = await host.request('GET', '/items/42', {
      headers: { Cookie: 'secret=1', 'X-Forwarded-For': '1.2.3.4', Accept: 'application/json' },
    })
    expect(getRes.status).toBe(200)
    expect(getRes.body).toEqual({
      id: '42',
      body: 'article content',
      hdrs: { accept: 'application/json' },
    })

    const postRes = await host.request('POST', '/webhook', {
      headers: { 'Content-Type': 'application/json' },
      body: '{"sig":"ok"}',
    })
    expect(postRes.status).toBe(201)
    expect(postRes.headers).toEqual({ 'X-Custom': 'yes' })
    expect(postRes.body).toEqual({ rawBody: '{"sig":"ok"}', parsed: { sig: 'ok' } })

    // Duplicate explicit document ID rejected and $set/$unset operators supported
    await expect(
      col.insertOne({ id: d1.id, code: 'UNIQUE_NEW', createdAt: '2026-07-01T00:00:00.000Z' } as unknown as {
        code: string
        score?: number
        createdAt: string
      }),
    ).rejects.toThrow(/duplicate document id/)
    await col.updateOne({ id: d1.id }, { $set: { score: 99 } } as unknown as Partial<{
      code: string
      score?: number
      createdAt: string
    }>)
    expect((await col.findOne({ id: d1.id }))?.score).toBe(99)
  })

  it('enforces admin user protection, kv.incr TTL preservation, and IPv6 loopback SSRF block in createTestHost', async () => {
    const host = createTestHost({
      manifest: {
        id: 'sec-test',
        permissions: ['storage.kv', 'users.write', 'users.ban', 'http.fetch'],
        http: { allow: ['[::1]', 'api.example.com'] },
      },
      fixtures: {
        users: [
          { id: 1, email: 'admin@example.com', role: 'admin', status: 'active' },
          { id: 2, email: 'deleted@example.com', role: 'user', status: 'deleted' },
        ],
      },
    })

    // Cannot update or ban an admin user, or unban a deleted user
    await expect(host.ctx.users.update(1, { nickname: 'Hacked' })).rejects.toThrow(/admin/)
    await expect(host.ctx.users.ban(1, true)).rejects.toThrow(/admin/)
    await expect(host.ctx.users.ban(2, false)).rejects.toThrow(/deleted/)

    // IPv6 loopback [::1] is blocked even if present in http.allow
    await expect(host.ctx.fetch('http://[::1]:8080/secret')).rejects.toThrow(/private host/)

    // kv.incr preserves existing value and TTL
    await host.ctx.kv.set('counter', 5, 60)
    expect(await host.ctx.kv.incr('counter', 3)).toBe(8)
    expect(await host.ctx.kv.get('counter')).toBe(8)
  })
})
