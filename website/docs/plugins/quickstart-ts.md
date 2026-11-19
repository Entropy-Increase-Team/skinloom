---
title: 快速入门：开发 TS 插件
sidebar_position: 2
---

# 快速入门：15 分钟写一个 Tier 1 TS 插件

本教程以官方插件 [`invitation-codes`](https://github.com/Entropy-Increase-Team/skinloom-plugins/tree/main/invitation-codes) 的核心逻辑为例，带你从零编写、测试、构建并打包一个运行在 QuickJS/WASM 沙箱中的 **Tier 1 TypeScript 插件**。

## 1. 用脚手架初始化工程

```bash
bunx create-skinloom-plugin my-invite --template page
cd my-invite
```

一个标准的 Tier 1 插件目录如下：

```text
my-invite/
├── plugin.json          # 清单（ID、权限、Hook、集合索引、菜单、表单、设置）
├── locales/
│   ├── zh-CN.json       # 前端与服务端 i18n 文案
│   └── en.json
└── src/
    ├── server.ts        # 后端入口（编译为单文件 server.js）
    ├── server.test.ts   # 使用 createTestHost 编写的单元测试
    └── web/index.tsx    # 前端入口（编译为单文件 web/index.js，共享宿主 React 与 @skinloom/ui）
```

## 2. 声明清单 `plugin.json`

SkinLoom 遵循**最小权限 + 声明即契约**原则：只有写入 `plugin.json` 的权限、Hook、存储集合索引、表单字段和菜单才会被宿主授权加载。

```json
{
  "$schema": "https://skinloom.cn/schema/plugin.v1.json",
  "id": "my-invite",
  "kind": "plugin",
  "runtime": "js",
  "version": "1.0.0",
  "name": { "zh-CN": "邀请码注册", "en": "Invitation Codes" },
  "apiVersion": 1,
  "engines": { "skinloom": ">=0.1.0 <2.0.0" },
  "server": "server.js",
  "web": "web/index.js",
  "permissions": ["admin.ui", "storage.docs", "score.write"],
  "hooks": {
    "guard": ["auth.register"],
    "event": ["user.registered"]
  },
  "storage": {
    "collections": {
      "codes": {
        "indexes": {
          "code": "string:unique",
          "status": "string",
          "createdAt": "time"
        }
      }
    }
  },
  "forms": {
    "auth.register": [
      {
        "name": "invite_code",
        "type": "text",
        "label": { "zh-CN": "邀请码", "en": "Invitation Code" },
        "required": false,
        "maxLength": 64
      }
    ]
  },
  "menus": [
    {
      "area": "admin",
      "path": "/codes",
      "title": { "zh-CN": "邀请码管理", "en": "Invitation Codes" },
      "icon": "ticket",
      "order": 40,
      "role": "admin"
    }
  ],
  "services": ["importCodes"]
}
```

> **提示**：每个文档集合支持最多 2 个 `string:unique`、3 个 `string`、3 个 `number` 和 2 个 `time` 预置索引槽位；过滤条件（`find` / `findOne` / `count`）仅可针对已声明索引的字段与 `id` 执行，保证在百万级数据时依然走索引。

## 3. 编写后端逻辑（`src/server.ts`）

```ts
import { definePlugin, reject } from '@skinloom/plugin-sdk/server'

interface CodeDoc extends Record<string, unknown> {
  code: string
  status: 'active' | 'used'
  usedBy: number
  createdAt: string
}

export default definePlugin({
  async setup(ctx) {
    const codes = ctx.db.collection<CodeDoc>('codes')

    // 1. 注册前置拦截 Guard：返回 reject(...) 即可向前端展示 RFC 9457 拒绝提示
    ctx.hooks.guard('auth.register', async (payload) => {
      const raw = payload.form?.['my-invite:invite_code'] ?? payload.form?.['invite_code']
      const code = typeof raw === 'string' ? raw.trim().toUpperCase() : ''
      if (!code) return reject('请输入有效的注册邀请码。')

      const doc = await codes.findOne({ code })
      if (!doc || doc.status !== 'active') return reject('邀请码无效或已被使用。')
    })

    // 2. 注册成功事件 Event：核销邀请码
    ctx.events.on('user.registered', async (payload) => {
      const raw = payload.form?.['my-invite:invite_code'] ?? payload.form?.['invite_code']
      const code = typeof raw === 'string' ? raw.trim().toUpperCase() : ''
      if (!code) return
      const doc = await codes.findOne({ code })
      if (doc && doc.status === 'active') {
        await codes.updateOne({ id: doc.id }, { status: 'used', usedBy: payload.userId })
      }
    })

    // 3. 管理员 HTTP 路由（自动挂载在 /api/plugins/my-invite/admin/codes）
    ctx.http.post('/admin/codes', { auth: 'admin' }, async () => {
      const code = `SL-${(await ctx.crypto.randomString(8)).toUpperCase()}`
      const doc = await codes.insertOne({
        code,
        status: 'active',
        usedBy: 0,
        createdAt: new Date().toISOString(),
      })
      return { status: 201, body: doc }
    })

    // 4. 暴露跨插件服务供其他插件（如 bs-importer）调用
    ctx.services.expose('importCodes', async (items: Array<{ code: string }>) => {
      let imported = 0
      for (const item of items) {
        const code = item.code.trim().toUpperCase()
        if (!(await codes.findOne({ code }))) {
          await codes.insertOne({ code, status: 'active', usedBy: 0, createdAt: new Date().toISOString() })
          imported++
        }
      }
      return { imported }
    })
  },
})
```

> **沙箱限制**：`src/server.ts` 运行在 QuickJS/WASM 中，**不能**导入 `node:fs`、`node:crypto` 等 Node 内置模块，请统一使用宿主注入的 `ctx.crypto`、`ctx.fetch`、`ctx.kv`、`ctx.db`、`ctx.files`。

## 4. 编写前端页面（`src/web/index.tsx`）

前端包自动共享宿主的 React 19、TanStack Query 与 `@skinloom/ui` 组件库，打包体积通常只有几 KB；所有颜色请使用语义 Tailwind Token（如 `text-foreground`、`bg-card`、`border-border`），确保在所有主题与亮暗模式下视觉一致。

```tsx
import { Button, Card, CardContent, CardHeader, CardTitle, CopyField } from '@skinloom/ui'
import { defineWebPlugin, toast, usePluginApi, useT } from '@skinloom/plugin-sdk/web'
import { useState } from 'react'

function AdminCodesPage() {
  const t = useT()
  const api = usePluginApi()
  const [latest, setLatest] = useState('')
  const create = api.useMutation<{ code: string }, Record<string, never>>('POST /admin/codes', {
    onSuccess(data) {
      setLatest(data.code)
      toast.success(`Generated ${data.code}`)
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <Button loading={create.isPending} onClick={() => create.mutate({})}>
          Generate Invite Code
        </Button>
        {latest && <CopyField value={latest} />}
      </CardContent>
    </Card>
  )
}

export default defineWebPlugin({
  pages: {
    'admin:/codes': AdminCodesPage,
  },
})
```

## 5. 编写单元测试并打包验证

使用 `@skinloom/plugin-sdk/testing` 提供的内存宿主模拟器 `createTestHost`，无需启动数据库或浏览器即可在毫秒级完成全链路单元测试：

```ts
import { createTestHost } from '@skinloom/plugin-sdk/testing'
import { describe, expect, it } from 'vitest'
import plugin from './server.js'
import manifest from '../plugin.json'

describe('my-invite', () => {
  it('blocks registration without a valid code and consumes valid codes', async () => {
    const host = createTestHost({ manifest })
    await host.setup(plugin)

    expect(await host.guard('auth.register', { email: 'a@b.c', nickname: 'A', form: {} }))
      .toMatchObject({ ok: false, rejected: true })

    const res = await host.request('POST', '/admin/codes', {
      user: { id: 1, email: 'admin@b.c', nickname: 'Admin', role: 'admin' },
    })
    const code = (res.body as { code: string }).code

    expect(await host.guard('auth.register', { email: 'a@b.c', nickname: 'A', form: { invite_code: code } }))
      .toEqual({ ok: true })
  })
})
```

验证清单、运行测试并打包 `.slp`：

```bash
bunx skinloom-plugin lint
bun run test
bunx skinloom-plugin keygen --out ./keys
bunx skinloom-plugin pack --key ./keys/signing.key.pem
```

开发阶段可运行 `bunx skinloom-plugin dev --url http://localhost:17546 --token <admin-token>` 开启文件监听与毫秒级热重载。
