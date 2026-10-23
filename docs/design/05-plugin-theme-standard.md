# 05 · SkinLoom 插件与主题开发体系（规范草案 v0.1）

> 这是 SkinLoom 插件 API 1.0 的设计草案，第二阶段（v2.0）开放，第一阶段内核按本规范预留扩展点。
> 读者：核心开发者（实现宿主）+ 未来的插件/主题作者（文档站"插件开发"栏目的蓝本）。
> 关联：[04 重构方案](04-rewrite-plan.md) §4（Hook 总线）、§6、§7（热加载）。

---

## 0. 设计原则

| 原则 | 落到实处 |
|---|---|
| **一个标准** | 一份 `plugin.json`、一门语言（TypeScript）、一个 SDK（`@skinloom/plugin-sdk`）、一个 CLI（`skinloom-plugin`）。插件和主题用同一套规则 |
| **易学** | 会写 React + TS 就能写插件；15 分钟教程能做出一个完整插件；能声明的不让写代码（菜单、设置表单、表单字段） |
| **能力给足** | 后端能挂 Hook、开接口、跑定时任务、存数据、调外部 HTTP、操作用户/角色/材质/积分、加登录方式/验证码/CDN 提供方；前端能加页面、插槽组件、首页区块、设置页；主题能改 token、CSS、布局、首页、组件 |
| **安全可控** | 后端跑在 JS 沙箱；所有能力按权限声明授予，安装时超管逐项确认；内存/超时/配额限制；签名分发 |
| **热加载** | 安装、升级、启停、卸载、改设置、切主题都不重启 |
| **长期兼容** | 插件 API 有独立大版本；弃用至少提前两个小版本告知；Hook 载荷带版本 |

---

## 1. 十五分钟看懂一个插件：邀请码

需求：开启后注册必须填写邀请码；管理员在后台生成邀请码。

```
invitation-codes/
├── plugin.json
├── src/server/index.ts
├── src/web/index.tsx
└── locales/{zh-CN,en}.json
```

```jsonc
// plugin.json
{
  "$schema": "https://skinloom.cn/schema/plugin.v1.json",
  "id": "invitation-codes",
  "kind": "plugin",
  "version": "1.0.0",
  "name": { "zh-CN": "邀请码", "en": "Invitation Codes" },
  "description": { "zh-CN": "注册时需要填写邀请码", "en": "Require an invitation code to register" },
  "authors": ["Alice <alice@example.com>"],
  "license": "MIT",
  "engines": { "skinloom": "^2.0.0" },
  "apiVersion": 1,
  "server": "server.js",
  "web": "web/index.js",
  "permissions": ["storage.docs", "admin.ui"],
  "hooks": { "guard": ["auth.register"], "event": ["user.registered"] },
  "storage": {
    "collections": {
      "codes": { "indexes": { "code": "string:unique", "usedBy": "number", "createdAt": "time" } }
    }
  },
  "forms": {
    "auth.register": [
      { "name": "invite_code", "type": "text", "label": "form.inviteCode", "required": true, "maxLength": 32 }
    ]
  },
  "menus": [{ "area": "admin", "path": "codes", "title": "menu.codes", "icon": "ticket" }],
  "settings": {
    "enabled": { "type": "boolean", "default": true, "label": "settings.enabled" }
  }
}
```

```ts
// src/server/index.ts
import { definePlugin, reject } from '@skinloom/plugin-sdk/server'

export default definePlugin({
  setup(ctx) {
    const codes = ctx.db.collection<{ code: string; usedBy?: number; createdAt: Date }>('codes')

    // 注册前校验：拒绝时返回原因，宿主负责展示
    ctx.hooks.guard('auth.register', async ({ form }) => {
      if (!(await ctx.settings.get('enabled'))) return
      const code = await codes.findOne({ code: form.invite_code, usedBy: null })
      if (!code) return reject(ctx.t('error.invalidCode'))
    })

    // 注册成功后（事务提交后异步执行）把邀请码标记为已用
    ctx.events.on('user.registered', async ({ user, form }) => {
      await codes.updateOne({ code: form.invite_code }, { usedBy: user.id })
    })

    // 给自己的后台页面用的接口：/api/plugins/invitation-codes/codes
    ctx.http.get('/codes', { auth: 'admin' }, async (req) =>
      codes.find({}, { sort: { createdAt: -1 }, limit: 50, cursor: req.query.cursor }))

    ctx.http.post('/codes', { auth: 'admin' }, async () => {
      const code = ctx.crypto.randomString(10)
      await codes.insertOne({ code, createdAt: new Date() })
      return { code }
    })
  },
})
```

```tsx
// src/web/index.tsx
import { defineWebPlugin, usePluginApi, useT } from '@skinloom/plugin-sdk/web'
import { Button, DataTable, Card } from '@skinloom/ui'

function CodesPage() {
  const t = useT()
  const api = usePluginApi()
  const { data, refetch } = api.useQuery('GET /codes')
  const create = api.useMutation('POST /codes', { onSuccess: refetch })
  return (
    <Card title={t('menu.codes')} actions={<Button onClick={() => create.mutate()}>{t('action.generate')}</Button>}>
      <DataTable data={data?.items ?? []} columns={[{ key: 'code' }, { key: 'usedBy' }, { key: 'createdAt', format: 'datetime' }]} />
    </Card>
  )
}

export default defineWebPlugin({
  pages: { 'admin/codes': CodesPage },   // 对应 plugin.json 里 menus 的 area + path
})
```

注意这个插件**没写一行注册表单代码**：表单字段由 `forms` 声明，宿主服务端渲染进注册页；设置页由 `settings` 声明自动生成。

---

## 2. 包结构

### 2.1 源码结构（脚手架生成）
```
my-plugin/
├── plugin.json              唯一的清单（源码与发布包相同）
├── package.json             开发依赖（@skinloom/plugin-sdk、@skinloom/ui 类型等）
├── src/
│   ├── server/index.ts      后端入口（可选）
│   └── web/index.tsx        前端入口（可选）
├── locales/zh-CN.json …     i18n（后端与前端共用）
├── assets/                  图片、字体等静态资源
├── tests/                   vitest
├── README.md  CHANGELOG.md  LICENSE
└── skinloom-plugin.config.ts（可选：构建选项）
```

### 2.2 发布包 `.slp`（zip）
```
my-plugin-1.2.0.slp
├── plugin.json
├── server.js                后端构建产物（单文件 ESM，含依赖，无 node: 内置模块）
├── web/index.js  web/style.css  web/chunks/*
├── assets/  locales/
├── README.md  CHANGELOG.md  LICENSE
├── checksums.txt            所有文件的 sha256
└── signature                对 checksums.txt 的 ed25519 签名
```
限制：包 ≤ 20 MB（可配置）；`server.js` ≤ 5 MB。

主题包同格式，`kind: "theme"`，通常只有 `web/`、`theme/`、`assets/`。

---

## 3. `plugin.json` 字段参考

| 字段 | 必填 | 说明 |
|---|---|---|
| `$schema` | 建议 | 指向官方 JSON Schema，编辑器自动补全与校验 |
| `id` | ✓ | 全局唯一，kebab-case，3–40 字符；发布后不可改 |
| `kind` | ✓ | `plugin` \| `theme` |
| `version` | ✓ | SemVer |
| `name` / `description` | ✓ | 多语言对象或字符串 |
| `authors` / `license` / `homepage` / `repository` | license 必填 | 市场上架要求开源许可证 |
| `icon` | | 包内图片路径或内置图标名 |
| `engines.skinloom` | ✓ | 宿主版本范围，如 `^2.1.0` |
| `apiVersion` | ✓ | 插件 API 大版本（当前 `1`） |
| `server` / `web` | | 后端、前端入口（构建产物路径） |
| `permissions` | | 权限列表，见 §8 |
| `http.allow` | | 外呼域名白名单（需要 `http.fetch` 权限） |
| `hooks` | | `{ guard: [], filter: [], event: [] }` 声明要订阅的 Hook（未声明的订阅在运行时被拒绝） |
| `routes.public` | | 挂在站点根路径的公开路由模式（需要 `routes.public` 权限，如新的皮肤加载协议） |
| `storage.collections` | | 文档集合与索引声明，见 §4.3 |
| `storage.quota` | | 申请的存储配额（KV 条数、文档数、文件总大小） |
| `settings` | | 设置项 schema（类型、默认值、校验、标签、分组），宿主自动生成设置页 |
| `menus` | | 菜单项：`area`（`user` \| `admin` \| `explore`）、`path`、`title`、`icon`、`order`、`role` |
| `forms` | | 向宿主表单追加字段：`auth.register`、`auth.login`、`user.profile`、`texture.upload`、`player.create` |
| `blocks` | | 提供的首页/页面区块类型及其属性 schema（前端实现见 §5.4） |
| `slots` | | 声明要占用的前端插槽（便于宿主预加载与冲突提示） |
| `dependencies` | | 依赖的其它插件及版本范围；`optionalDependencies` 同理 |
| `services` | | 对其它插件公开的服务名（见 §4.6） |
| `theme` | 主题必填 | 见 §6 |

---

## 4. 后端开发

### 4.1 入口与生命周期

```ts
export default definePlugin({
  setup(ctx) { /* 每次加载（启动、启用、升级后）调用：在这里注册 Hook、路由、定时任务 */ },
  async onInstall(ctx) {},                 // 首次安装
  async onEnable(ctx) {},                  // 每次启用
  async onDisable(ctx) {},                 // 每次禁用（卸载前也会调用）
  async onUpgrade(ctx, { from }) {},       // 升级时，新版本执行
  async onUninstall(ctx, { purge }) {},    // 卸载；purge=true 时宿主会删除插件全部数据
})
```

规则：
- `setup` 里的注册由宿主收集后**一次性原子替换**进注册表——要么全部生效，要么（抛错时）全部不生效，旧版本继续服务。
- 宿主为每个插件维护一个**运行时池**（默认 2 个实例），模块顶层变量不在实例间共享，也不会在重载后保留。**状态请放进存储。**
- 所有 `ctx` API 都是异步的（返回 Promise）。

### 4.2 `ctx` 能力总表

| 命名空间 | 主要方法 | 所需权限 |
|---|---|---|
| `ctx.hooks` | `guard(name, fn)`、`filter(name, fn)` | 在 `hooks` 中声明 |
| `ctx.events` | `on(name, fn)`、`emit(localName, payload)`（发出 `plugin:<id>.<name>` 事件） | 订阅需声明 |
| `ctx.http` | `get/post/put/patch/delete(path, opts, handler)` → `/api/plugins/<id>/...`；`opts.auth`：`public` \| `user` \| `admin` \| `superadmin`；`opts.rateLimit` | 默认授予；根路径公开路由需 `routes.public` |
| `ctx.cron` | `schedule(name, cronExpr, fn)`（集群内只在一个 worker 上执行） | `cron` |
| `ctx.kv` | `get/set/delete/list/incr`，支持 TTL | `storage.kv` |
| `ctx.db` | `collection(name)` → `insertOne/findOne/find/updateOne/updateMany/deleteOne/count`，条件只能用于声明过的索引字段 | `storage.docs` |
| `ctx.files` | 插件私有文件空间：`put/get/delete/url` | `storage.files` |
| `ctx.fetch` | 标准 `fetch` 语义，只允许 `http.allow` 中的域名，禁止内网地址，带超时与响应大小上限 | `http.fetch` |
| `ctx.settings` | `get(key)`、`all()`、`onChange(fn)` | 默认授予（仅本插件设置） |
| `ctx.site` | 站点名、URL、语言、公开设置的只读视图 | 默认授予 |
| `ctx.users` | `get/find/list`；`update`（昵称、偏好、自定义徽章等受限字段）；`ban` | `users.read` / `users.write` / `users.ban` |
| `ctx.players` | `get/findByName/list/create/rename/setTextures/delete` | `players.read` / `players.write` |
| `ctx.textures` | `get/list/search/create(fromBytes)/update/delete/readBytes` | `textures.read` / `textures.write` |
| `ctx.closet` | `list/add/remove` | `closet.write` |
| `ctx.score` | `balance(userId)`、`apply(userId, delta, reason)`（走统一积分流水） | `score.write` |
| `ctx.notify` | `send(userIds, title, markdown)`、`announce(...)` | `notify.send` |
| `ctx.mail` | `send({ to, subject, template, data })`（经宿主队列与模板） | `mail.send` |
| `ctx.auth` | `registerProvider({...})` 新增登录方式（如论坛账号登录）；`registerPasswordVerifier(algo, fn)` 识别外部密码格式 | `auth.provider` / `auth.password` |
| `ctx.captcha` | `registerProvider({...})` 新增验证码服务 | `captcha.provider` |
| `ctx.cdn` | `registerPurger({...})` 新增 CDN 刷新提供方 | `cdn.provider` |
| `ctx.yggdrasil` | 扩展 profile 属性、外置登录元数据 | `yggdrasil.extend` |
| `ctx.services` | `expose(name, fn)`、`use(pluginId, name)` 插件间调用 | 依赖需在 `dependencies` 声明 |
| `ctx.audit` | `record(action, target, meta)` | 默认授予 |
| `ctx.crypto` | `randomString/randomBytes/sha256/hmac/timingSafeEqual/uuid` | 默认授予 |
| `ctx.t` / `ctx.i18n` | 按当前请求语言翻译本插件文本 | 默认授予 |
| `ctx.log` | `debug/info/warn/error`（后台可看插件日志） | 默认授予 |
| `ctx.cache` | 进程/集群缓存（命名空间隔离） | 默认授予 |

### 4.3 存储：KV、文档集合、文件

插件**拿不到裸 SQL**，而是用三种不依赖数据库方言的存储（同一份代码在 SQLite / MySQL / PostgreSQL 上行为一致）：

| 存储 | 适合 | 实现 |
|---|---|---|
| KV | 开关、计数器、小型缓存、令牌 | `plugin_kv` 表 |
| 文档集合 | 业务数据（邀请码、评论、日志……） | `plugin_docs` 表：数据存 JSON；清单里声明的索引字段映射到槽位列 `s1..s3`（字符串）、`n1..n3`（数字）、`t1`（时间），宿主建复合索引；`:unique` 建唯一索引 |
| 文件 | 插件生成的图片、导出文件 | blob 存储下的插件私有前缀，带配额 |

索引声明示例：`"indexes": { "code": "string:unique", "usedBy": "number", "createdAt": "time" }`。查询条件、排序只能落在声明过的索引字段上（宿主据此保证性能）；每个集合最多 3 个字符串、3 个数字、1 个时间索引字段。需要更多时拆集合或用 KV 做反向索引。

> 为什么不给 SQL：三种方言的迁移脚本对插件作者是巨大负担；裸 SQL 也让沙箱与配额失去意义。若将来确有需求，再以"方言中立的 schema DSL"形式开放。

### 4.4 Hook 目录（初版）与 BS 对照

**Guard（同步，可拒绝）**

| Hook | 载荷要点 | 对应 BS |
|---|---|---|
| `auth.login` | identifier, user, ip, form | `can_login` |
| `auth.register` | email, nickname/playerName, ip, form（含插件声明的字段） | `can_register` |
| `player.create` / `player.rename` / `player.delete` | user, player/name | `can_add_player` / `can_rename_player` / `can_delete_player` |
| `player.textures.set` / `player.textures.clear` | player, type, texture | `can_set_texture` / `can_clear_texture` |
| `texture.upload` | user, name, type, visibility, width, height, sizeBytes, sha256, form | `can_upload_texture` |
| `texture.rename` / `texture.visibility` / `texture.type` / `texture.delete` | texture, 新值 | `can_update_texture_*` / `can_delete_texture` |
| `closet.add` / `closet.rename` / `closet.remove` | user, texture, name | `can_*_closet_item` |
| `score.checkin` | user | `can_sign` |
| `user.profile.update` / `user.avatar.update` | user, field, value | `user_can_edit_profile` / `user_can_update_avatar` |
| `report.submit` | reporter, texture, reason | `user_can_report` |
| `yggdrasil.authenticate` / `yggdrasil.join` | user, player, clientToken / serverId | 新增 |

**Filter（同步，改值）**

| Hook | 值 | 对应 BS |
|---|---|---|
| `player.name` | 新角色名 | `new_player_name` |
| `texture.name` | 上传/改名的材质名 | `uploaded_texture_name` |
| `closet.itemName` | 收藏名 | `add_closet_item_name` / `rename_closet_item_name` |
| `score.checkinAmount` | 签到得分 | `sign_score` |
| `user.badges` | 徽章列表 | `user_badges` |
| `profile.textures` | CSL/USM/Yggdrasil 输出的材质信息 | 新增 |
| `yggdrasil.profileProperties` | 角色属性 | 新增 |

**Event（异步，提交后，至少一次）**：`user.registered`、`user.loggedIn`、`user.updated`、`user.banned`、`user.deleted`、`player.created`、`player.renamed`、`player.updated`、`player.deleted`、`texture.uploaded`、`texture.updated`、`texture.deleted`、`closet.added`、`closet.removed`、`score.changed`、`report.submitted`、`report.resolved`、`yggdrasil.joined`、`settings.changed`。

BS 中被取代、不再作为 Hook 提供的：`client_ip`（改为可信代理配置）、`user_password` / `verify_password`（改为 `ctx.auth.registerPasswordVerifier`）、`head_links` / `scripts` / `grid:*` / `auth_page_rows:*` / 菜单事件（改为前端扩展与清单声明）。

Hook 目录在 Go 代码中定义，文档站的 Hook 参考页与 SDK 类型都由它生成，不会出现 BS 那种"同名事件两处参数不一样"的问题。

### 4.5 路由与接口
- 插件接口统一在 `/api/plugins/<id>/`，宿主负责认证、CSRF、限流、错误格式（RFC 9457），handler 只关心业务。
- 请求体/响应体有大小上限（默认 1 MB，可在清单申请提高到 10 MB）。
- 需要挂在站点根路径的（例如实现一个新的皮肤加载协议 `/mcskin/{name}.json`），在 `routes.public` 声明模式并申请 `routes.public` 权限；不得与宿主保留路径冲突，安装时检查。
- 插件接口会出现在运行时 OpenAPI 文档中，前端 `usePluginApi()` 有类型提示（构建时从 `ctx.http` 注册推导）。

### 4.6 插件间协作
- 事件：任何插件都可以订阅 `plugin:<id>.<name>` 事件（需在 `hooks.event` 声明）。
- 服务：A 插件 `ctx.services.expose('issueCode', fn)`，B 插件在 `dependencies` 声明 A 后 `await ctx.services.use('a', 'issueCode')(args)`；跨插件调用同样受超时与权限约束。

### 4.7 运行环境（不是 Node.js）
- 语言：ES2023（async/await、class、模块……），SDK 用 esbuild 打包成单文件。
- **没有** `fs`、`process`、`net`、`child_process`、`Buffer`（SDK 提供 `Uint8Array` 工具）——一切外部交互走 `ctx`。只依赖纯 JS 的 npm 包都能用（如 zod、date-fns、lodash-es）。CLI 构建时检测 `node:` 导入并报错。
- 提供：`fetch`（即 `ctx.fetch`）、`URL`、`TextEncoder/Decoder`、`crypto.getRandomValues`、`setTimeout`（受调用超时约束）、`console`（映射到 `ctx.log`）。
- 资源限制（默认，可由超管调整）：内存 64 MB/插件；Guard/Filter 单次 2 s、接口 30 s、定时任务 5 min；外呼 HTTP 单次 10 s、响应 5 MB。超时的 Guard 按该 Hook 的 fail 策略处理；连续失败超过阈值自动熔断并通知超管。

---

## 5. 前端开发

### 5.1 加载机制
- 插件前端是 SDK 构建出的 **ES Module**。`react`、`react-dom`、`react/jsx-runtime`、`@skinloom/ui`、`@skinloom/plugin-sdk/web`、`@tanstack/react-query` 是**共享依赖**：插件构建时被替换为宿主提供的共享模块表，插件包里不带 React，保证只有一个 React 实例。
- 宿主根据 bootstrap 里的插件前端清单，在需要时 `import()` 插件模块（URL 带版本号，升级后自动换新）；样式文件同时注入。
- 插件页面与插槽组件在**客户端渲染**；菜单、表单字段、设置页因为是声明式的，可以服务端渲染。
- 每个插件组件外层有错误边界：插件崩溃只影响自己的区域，并上报到后台插件日志。

### 5.2 `defineWebPlugin`

```ts
export default defineWebPlugin({
  pages: {                                  // key = `${area}/${path}`，与 plugin.json 的 menus/路由对应
    'user/invites': InvitesPage,            // → /user/p/invitation-codes/invites
    'admin/codes': CodesPage,               // → /admin/p/invitation-codes/codes
    'explore/gallery': GalleryPage,         // → /p/invitation-codes/gallery（公开页）
  },
  slots: {
    'skinlib.detail.sidebar': DownloadStatsCard,
    'user.dashboard.widgets': { component: InviteWidget, order: 20 },
  },
  blocks: { 'invite-cta': InviteCtaBlock }, // 首页区块，props 的 schema 在 plugin.json 的 blocks 中声明
  settingsPage: CustomSettings,             // 可选；不提供则按 settings schema 自动生成
})
```

### 5.3 插槽目录（初版）

| 区域 | 插槽 |
|---|---|
| 全站 | `site.header.actions`、`site.footer`、`site.banner` |
| 认证 | `auth.login.below`、`auth.login.providers`、`auth.register.below` |
| 用户中心 | `user.dashboard.widgets`、`user.profile.sections`、`user.player.actions`、`user.closet.item.actions` |
| 皮肤库 | `skinlib.card.badges`、`skinlib.detail.sidebar`、`skinlib.detail.below`、`skinlib.upload.below` |
| 后台 | `admin.dashboard.widgets`、`admin.user.actions`、`admin.user.tabs`、`admin.texture.actions`、`admin.status.cards` |

插槽目录与 Hook 目录一样，是版本化的公共 API；新增插槽是小版本，删除/改参数是大版本。

### 5.4 区块（Blocks）
页面中需要 SEO 的部分（主要是首页）由**区块**组成：站长在后台可视化地增删、排序、配置区块。
- 内置区块（服务端渲染）：`hero`、`announcement`、`stats`、`latest-textures`、`popular-textures`、`markdown`、`image`、`features`、`buttons`、`launcher-guide`（authlib-injector 配置引导）。
- 插件区块：在清单 `blocks` 声明类型名与属性 schema，前端实现组件；客户端渲染，服务端输出占位骨架。

### 5.5 客户端 SDK 与 UI 组件
- `@skinloom/plugin-sdk/web`：`usePluginApi()`（调用本插件接口，带类型、带认证）、`useSettings()`、`useT()`、`useSession()`、`useSite()`、`useTheme()`、`toast()`、`confirm()`、`navigate()`。
- `@skinloom/ui`：宿主设计系统的公开部分——`Button`、`Card`、`Dialog`、`Form`（含 zod 绑定）、`DataTable`、`Tabs`、`Badge`、`EmptyState`、`SkinViewer`（skinview3d 封装）、`TexturePicker`、`PlayerPicker`、`UserAvatar`、`TextureCard`……全部跟随当前主题 token。
- 样式：推荐只用 `@skinloom/ui` 组件 + 主题 CSS 变量；需要 Tailwind 时用 SDK 提供的预设（与宿主同一套 token），构建产物是插件自己的 CSS 文件。

---

## 6. 主题

### 6.1 主题能改什么（由浅到深）

| 层级 | 内容 | 渲染方式 | 难度 |
|---|---|---|---|
| ① Token | 颜色（亮/暗）、圆角、字体、阴影、3D 预览背景 | 服务端注入 CSS 变量 | 只写 JSON |
| ② CSS | 额外样式表 | 服务端注入 `<link>` | 写 CSS |
| ③ 布局变体 | 站点导航（顶栏/侧栏）、登录页（卡片/分屏/全屏背景）、用户中心（侧栏/顶栏）、皮肤库密度 | 服务端 | 选项 |
| ④ 首页编排 | 默认区块及其属性 | 服务端（内置区块）/ 客户端（插件区块） | JSON |
| ⑤ 组件覆盖 | 覆盖指定组件：`site.header`、`site.footer`、`site.logo`、`auth.background`、`home.hero`、`texture.card`、`player.card` | 客户端（服务端先输出默认实现或骨架） | 写 React |

**建议尽量停留在 ①–④**：完全服务端渲染、无闪烁、最容易跟随宿主升级。⑤ 是为深度定制准备的，代价是覆盖的组件需要跟随插槽 API 版本维护。

### 6.2 主题清单

```jsonc
{
  "id": "aurora",
  "kind": "theme",
  "version": "1.0.0",
  "name": { "zh-CN": "极光", "en": "Aurora" },
  "engines": { "skinloom": "^2.0.0" },
  "apiVersion": 1,
  "theme": {
    "tokens": {
      "light": { "primary": "oklch(0.62 0.19 260)", "background": "oklch(0.99 0 0)", "radius": "0.75rem" },
      "dark":  { "primary": "oklch(0.70 0.17 260)", "background": "oklch(0.16 0.02 260)" },
      "fonts": { "sans": "Inter, 'HarmonyOS Sans SC', sans-serif" },
      "viewer": { "background": "panorama:assets/panorama.png" }
    },
    "css": "theme/theme.css",
    "layout": { "site": "topnav", "auth": "split", "user": "sidebar", "skinlib": "comfortable" },
    "home": [
      { "type": "hero", "props": { "title": "$site.name", "background": "assets/hero.webp" } },
      { "type": "latest-textures", "props": { "limit": 12 } },
      { "type": "launcher-guide" }
    ],
    "overrides": ["site.footer"]
  },
  "web": "web/index.js",
  "settings": {
    "accent": { "type": "color", "default": "#7c3aed", "label": "settings.accent", "token": "primary" },
    "heroImage": { "type": "image", "label": "settings.hero" }
  }
}
```

Token 名称与 shadcn/ui 一致（`background`、`foreground`、`card`、`popover`、`primary`、`secondary`、`muted`、`accent`、`destructive`、`border`、`input`、`ring`、`chart-1..5`、`sidebar-*`、`radius`），另加 SkinLoom 专有的 `viewer.*`、`brand-gradient`、`font-display`。主题设置项可以直接绑定到 token（如上例 `accent → primary`），站长在后台调色即时预览、保存即生效。

### 6.3 主题切换（热加载）
后台选择主题 → 主题快照替换 → `config_version` 递增 → Next.js 根布局下一次请求注入新 token 与 CSS → 已打开的页面收到 SSE 后刷新。支持"预览模式"：仅当前管理员会话看到新主题，确认后再对全站生效。默认主题本身就是一个按此规范编写的内置主题包。

---

## 7. 设置与 i18n

- **设置 schema** 字段类型：`string`、`text`（多行）、`markdown`、`number`、`boolean`、`select`、`multiselect`、`color`、`image`（上传到插件文件空间）、`secret`（加密存储、前端不回显）、`json`、`list`、`group`（分组）。支持 `default`、`required`、`min/max`、`pattern`、`showIf`（条件显示）、`scope`（`site` 全站 / `user` 用户级设置——用户级会出现在用户偏好页）。
- 设置修改即时生效：后端 `ctx.settings.onChange`，前端 `useSettings()` 自动更新。
- **i18n**：`locales/<lang>.json`，后端 `ctx.t`、前端 `useT()` 共用；清单里的文本（菜单标题、设置标签、表单 label）写 key 即可。宿主语言集合（含 BS 继承的 13 种）中缺失的翻译回退到 `en`，再回退到 `zh-CN`。

---

## 8. 权限模型

| 等级 | 权限 | 安装时展示 |
|---|---|---|
| 默认授予 | 本插件设置/路由/日志/缓存/加密工具/i18n、读取站点公开信息 | 不单独展示 |
| 普通 | `storage.kv`、`storage.docs`、`storage.files`、`cron`、`admin.ui`、`users.read`、`players.read`、`textures.read`、`notify.send` | 列表展示 |
| 敏感 | `http.fetch`（附域名）、`mail.send`、`players.write`、`textures.write`、`closet.write`、`score.write`、`users.write`、`routes.public`、`yggdrasil.extend`、`cdn.provider`、`captcha.provider` | 橙色，逐项确认 |
| 高危 | `users.ban`、`auth.provider`、`auth.password` | 红色，需要输入确认 |

- 运行时强制：未声明的能力调用直接抛 `PermissionDenied`；未声明的 Hook 订阅在 `setup` 时失败。
- 升级时权限**增加**需要超管重新确认，确认前继续运行旧版本。
- 后台"插件详情"展示：实际调用统计、外呼记录、存储用量、错误日志。

---

## 9. 安装、升级、卸载（热加载语义）

| 操作 | 步骤 | 失败时 |
|---|---|---|
| 安装 | 下载 → 校验签名与 checksums → 校验清单与 `engines` → 依赖检查 → 权限确认 → 解压到 `data/plugins/<id>/<ver>/` → 建运行时池 → 建存储索引 → `onInstall` → `onEnable` → `setup` 注册 → 注册表原子替换 → 版本号递增 → 广播 | 任一步失败则回滚文件与注册表，插件状态记为 `failed`，不影响站点 |
| 启用 / 禁用 | 注册表替换；禁用时排空在途调用（≤ 30 s）后释放运行时 | |
| 升级 | 新版本并行加载 → 存储索引变更 → `onUpgrade` → 切换 → 排空并卸载旧版本 | 新版本 `setup` 失败则保持旧版本运行 |
| 卸载 | 禁用 → `onUninstall` → 删除文件；数据默认保留（重装可恢复），勾选"同时删除数据"才清空 | |
| 多副本 | 状态写库 + 包文件进 blob 存储；其它副本收到广播后各自执行同样的加载/替换 | 副本加载失败上报，后台可见 |

开发模式下 `skinloom-plugin dev` 走同一条路径（只是跳过签名、自动确认权限），保证"开发时的热替换"和"线上的热安装"是同一套机制。

---

## 10. 工具链

```bash
npm create skinloom-plugin@latest my-plugin   # 模板：basic / page / auth-provider / block / theme / full
cd my-plugin && pnpm install
skinloom up --dev                             # 另开终端：本地宿主，开发模式 + 演示数据（skinloom seed demo）
pnpm dev                                      # 监听源码，保存即构建并热替换到本地宿主（前后端同时）
pnpm test                                     # vitest + @skinloom/plugin-sdk/testing
pnpm build && pnpm pack                       # → dist/my-plugin-1.0.0.slp
pnpm sign                                     # 用发布者密钥签名
pnpm publish                                  # 向官方 registry 提交（GitHub PR 流程）
```

- **类型**：Hook 载荷、插槽属性、`ctx` API、`plugin.json` 全部有类型与 JSON Schema，编辑器内补全和报错。
- **校验**：`skinloom-plugin lint` 检查清单、权限与代码使用是否一致（比如用了 `ctx.fetch` 却没声明 `http.fetch`）、`node:` 导入、包体积。
- **测试工具**：`createTestHost({ settings, fixtures })` 在 Node 里模拟完整 `ctx`，可以直接调用 `host.guard('auth.register', payload)`、`host.request('POST', '/codes')`、断言存储与通知；外呼用 `host.fetchMock`。
- **调试**：插件日志与报错在开发宿主后台实时查看；源码映射（source map）让堆栈指向 TS 源码。

---

## 11. 版本与兼容策略

| 版本号 | 含义 |
|---|---|
| 宿主版本（`engines.skinloom`） | SkinLoom 本体的 SemVer |
| `apiVersion` | 插件 API 大版本。宿主在一个大版本周期内同时支持当前与上一个 API 大版本 |
| `@skinloom/plugin-sdk` | 主版本号 = `apiVersion`；小版本新增能力 |
| Hook 载荷版本 | 载荷不兼容变更发新版本（`texture.uploaded@2`），旧版本继续投递一个 API 大版本周期 |

弃用流程：标记 `@deprecated`（SDK 类型 + 文档 + 后台对使用该 API 的插件给出提示）→ 至少两个宿主小版本后 → 下一个 API 大版本移除。所有变化写进"插件 API 更新日志"（见 06）。

---

## 12. 插件市场

- **分发**：签名的静态 registry（`index.json` + 包文件），官方源 + 国内镜像 + 私有源，宿主可配置多个。
- **上架流程**：作者 `publish` → 向 registry 仓库提交元数据 PR → CI 自动检查（清单、签名、体积、`lint`、依赖许可证、恶意模式扫描）→ 涉及敏感/高危权限的人工审核 → 合并后官方密钥对索引签名 → 同步镜像。
- **上架要求**：开源许可证、README（含截图）、CHANGELOG、权限用途说明、可复现构建（附源码仓库链接）。
- **信任与处置**：发布者签名 + 官方索引签名；问题版本可被"撤回"（yank），严重安全问题进入索引中的阻断名单，宿主自动禁用并通知超管。
- **后台市场**：搜索、分类（功能 / 主题 / 登录与安全 / 存储与 CDN / 游戏协议 / 运营）、兼容性标识、权限预览、更新提示与更新日志、一键安装/升级。

---

## 13. 学习路径（文档站"插件开发"栏目目录）

1. 入门：15 分钟做一个邀请码插件（本文 §1 扩写）
2. 概念：清单、后端、前端、主题、权限、热加载
3. 指南：Hook 与事件 · 接口 · 存储 · 定时任务 · 外部请求 · 前端页面 · 插槽 · 区块 · 设置 · i18n · 登录/验证码/CDN 提供方 · 插件间协作 · 测试 · 发布
4. 主题开发：Token → CSS → 布局 → 首页 → 组件覆盖
5. 参考（自动生成）：`plugin.json` · `ctx` API（TypeDoc）· Hook 目录 · 插槽目录 · `@skinloom/ui` 组件（Storybook）
6. 示例与 Cookbook：邀请码、论坛账号登录、新皮肤协议、Turnstile 验证码、自定义首页区块、后台统计页、材质描述、评论
7. 从 Blessing Skin 迁移插件：概念对照、Hook 对照表（§4.4）、常见改写
8. 插件 API 更新日志与升级指南

---

## 14. 第二阶段实施拆分

| 步骤 | 内容 | 估算 |
|---|---|---|
| P2-1 | `jsrt`（QuickJS-NG/WASM）与运行时池、`ctx` 核心能力（hooks/events/http/kv/docs/settings/log）、权限执行、资源限制、熔断 | 3 周 |
| P2-2 | `@skinloom/plugin-sdk`（server/web/testing）、`skinloom-plugin` CLI、`create-skinloom-plugin` 模板、类型与 Schema 生成、dev 热替换 | 3 周 |
| P2-3 | 前端：共享模块表、页面容器、插槽、区块、声明式表单/设置页、主题开放（token/CSS/布局/首页/覆盖）、预览模式 | 3 周 |
| P2-4 | `.slp` 包格式、签名与校验、安装/升级/卸载热加载全流程、多副本同步 | 2 周 |
| P2-5 | registry 仓库与 CI、后台市场、国内镜像、撤回/阻断名单 | 2 周 |
| P2-6 | 用 SDK 实现 10 个示例插件 + 2 个主题，插件开发文档定稿，**插件 API 1.0 冻结** | 2–3 周 |

合计约 15–16 周。第一阶段需要提前就位的内核部分：Hook 目录与总线、可原子替换的注册表、类型化设置、主题 token 与布局变体、前端插槽与区块、bootstrap + `config_version` + SSE。
