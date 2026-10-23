# 04 · SkinLoom 重构方案（v2）

> 输入：[01 架构拆解](01-bs-architecture.md)、[02 功能清单与兼容契约](02-feature-contract.md)、[03 缺陷清单](03-bs-defects.md)。
> 配套：[05 插件与主题开发体系](05-plugin-theme-standard.md)、[06 文档与日志体系](06-docs-and-logs.md)。
> 修订：v1（2026-10-22）→ **v2（2026-10-23）**，按评审意见改为前后端分离 + Next.js、启动器一键运行、插件/主题以 TypeScript 为统一标准、全链路热加载、确定项目名、补文档体系。修订明细见 [devlog](/devlog/plan-v2)。

---

## 0. 结论速览

| 维度 | 决策 |
|---|---|
| 项目名 | **SkinLoom**（中文名「云织」）。皮肤 + 织机：把像素织成皮肤，也呼应"衣柜"。命令行 `skinloom`，npm scope `@skinloom`。仓库 [Entropy-Increase-Team/skinloom](https://github.com/Entropy-Increase-Team/skinloom)，官网 **skinloom.cn**。鸣谢中保留 Blessing Skin。详见 §11 |
| 重构方式 | 重写，不移植。外部协议（CSL/USM/Legacy/Yggdrasil/OAuth）兼容，内部重新设计 |
| 前后端 | **分离**：Go 后端（API + 游戏协议 + 后台任务）与 Next.js 前端是两个独立程序，可独立部署与扩容 |
| 一键运行 | 后端二进制自带 **启动器 `skinloom up`**：拉起并监管 Next.js 子进程、对外只暴露一个端口；Windows/macOS/Linux 发行包附带便携 Node 运行时 + `start.bat` / `start.command` / `start.sh` |
| 后端 | Go（跟随最新两个稳定版），模块化单体：chi + Huma（OpenAPI 3.1）、ent + Atlas、SQLite / MySQL / MariaDB / PostgreSQL、可选 Redis(Valkey) 与 S3 |
| 前端 | **Next.js（App Router）+ React 19 + TypeScript**，shadcn/ui（Radix + Tailwind CSS v4）、next-intl、TanStack Query、orval 生成 API 客户端、skinview3d |
| 插件 | **SkinLoom 插件标准**：一份 `plugin.json` + 一门语言（TypeScript）+ 一个 SDK + 一个 CLI。后端代码跑在内嵌的 JS 沙箱（QuickJS-NG/WASM），前端代码运行时挂载到扩展点；主题是同一标准下的 `kind: "theme"` |
| 热加载 | 插件安装/卸载/启停/升级、主题切换、站点设置、大部分文件配置 **全部无需重启**；后端用原子替换的注册表，前端靠配置版本号 + SSE 刷新 |
| 文档 | Docusaurus 文档站：用户/站长指南、开发者指南、插件开发（按插件 API 版本化）、API 参考（OpenAPI/TypeDoc 生成）、更新日志、开发日志（devlog）、ADR |
| 部署 | Win/macOS/Linux 发行包、Docker（server / web / all-in-one 三个镜像）、Compose、Helm（k3s/k8s）、系统服务 |
| 迁移 | `skinloom import bs` 只读导入 BS，玩家 UUID、密码、皮肤链接不变 |
| 许可 | MIT |

---

## 1. 目标与非目标

**第一阶段 v1.0 目标**
1. 功能对齐 BS 核心 + 刚需插件（Yggdrasil、USM、Legacy、204、S3 存储、CDN 刷新、社交登录、邀请码/禁止注册/邮箱域名限制），清单见 02 §1。
2. 外部契约兼容（02 §2），可从 BS 一键导入。
3. 修掉 03 中全部 🔴、🟠 问题，并用数据库约束、事务、限流等结构性手段保证不复发。
4. 部署：发行包解压即用（SQLite + 本地存储 + 内存缓存），同时能扩展到 k8s 多副本。
5. **插件平台的地基在 v1 就位**：Hook 总线、可热替换的注册表、类型化设置、主题 token、前端插槽、配置版本号 + SSE。默认主题本身就按主题标准实现。

**第二阶段 v2.0**：开放第三方插件（SDK、CLI、市场），见 05。

**非目标**：不兼容 BS 的 PHP 插件；不做在线覆盖式自更新；不做微服务；不做多租户。

---

## 2. 关键决策与论证

### 2.1 前后端分离 + 启动器

| 方案 | 结论 | 原因 |
|---|---|---|
| 前端嵌进 Go 二进制（v1 稿方案） | ✗ | 选了 Next.js 后无法静态嵌入；SSR 需要 Node |
| **Go 后端 + Next.js 前端，两个进程；Go 启动器统一拉起** | **✓** | 前端获得 SSR/SEO 与成熟生态；部署仍然"一键" |
| Next.js 在前、Go 在后（Next 反代 API） | ✗ | 游戏协议这条最热的路径会多经过一次 Node |

**入口与路由归属**：默认由 Go 做唯一入口（一个端口），按路径分流——

```
                    :8080（唯一对外端口）
用户/游戏/启动器 ──▶ skinloom（Go）
                     ├─ 后端保留路径 → Go 直接处理
                     │   /api/*  /oauth/*  /.well-known/*
                     │   /{name}.json  /csl/*  /usm/*  /skin/*  /cape/*  /textures/*  /raw/*
                     │   /avatar/*  /preview/*  /plugins-assets/*  /themes-assets/*  /events (SSE)
                     ├─ /_next/static/*、/public 静态文件 → Go 直接从前端产物目录读文件（不经过 Node）
                     └─ 其它所有页面 → 反向代理到 Next.js（127.0.0.1:3000，内部端口）
```

- 小站和桌面发行包：`skinloom up` 一个命令，Go 作为监管者拉起 Node 子进程（崩溃自动重启、日志合并、优雅退出）。
- 大站/k8s：web 与 server 各自独立部署；Ingress 可以把所有流量交给 server（`gateway` 模式），也可以按上面的路径表直接分流（`split` 模式），让页面请求不经过 Go。
- 后端保留路径是一份**写进代码的固定清单**，Next.js 的路由不允许与之冲突（CI 检查）。

### 2.2 为什么 Next.js 可行，以及要避开什么

可行：React 不变；SSR 解决皮肤库/详情页/首页的 SEO 与首屏；`output: "standalone"` 产出不依赖 `node_modules` 全量的独立服务包，适合打包分发。

要避开的坑（已做设计取舍）：
| 坑 | 取舍 |
|---|---|
| 插件 UI 运行时加载：Next.js 是构建期编译的，Module Federation 的 `nextjs-mf` 已在弃用、对 App Router 支持有限 | **不用 Module Federation**。插件前端是独立构建的 ES Module，宿主在浏览器里 `import()` 加载，React 等共享依赖由宿主通过共享模块表提供（05 §5） |
| 多副本下 ISR/Data Cache 一致性需要自定义 cacheHandler | **不依赖 ISR**。页面动态渲染，数据缓存交给 Go（快且已有失效机制）；Next 进程内只做几秒的微缓存，以"配置版本号"为键 |
| 插件组件不能参与服务端渲染（RSC 无法在运行时引入外部组件） | 插件页面与插槽在客户端渲染；需要 SEO 的首页用**声明式区块（Blocks）**：内置区块服务端渲染，插件区块客户端渲染（05 §6.4） |
| Node 版本漂移 | 发行包内置固定版本的 Node LTS；Docker 镜像固定版本 |

### 2.3 后端选型（与 v1 相同）

| 关注点 | 选择 | 备选 |
|---|---|---|
| 路由 / API | chi v5 + Huma v2（代码即 OpenAPI 3.1） | Gin + swaggo |
| 数据访问 / 迁移 | ent + Atlas 版本化迁移（三方言各一套 SQL） | GORM + goose |
| SQLite | modernc.org/sqlite（纯 Go，`CGO_ENABLED=0`） | |
| 配置 | koanf（文件 + 环境变量 + **fsnotify 热加载**） | |
| 缓存 / 发布订阅 | 内存（otter）/ Redis·Valkey（go-redis v9） | |
| 后台任务 | 内置 DB 任务队列（零依赖），可选 Redis | asynq |
| OAuth2/OIDC 提供方 | zitadel/oidc v3 | ory/fosite |
| **插件 JS 沙箱** | **QuickJS-NG 编译为 WASM，跑在 wazero 上**（参考实现 fastschema/qjs：纯 Go、ES2023、async/await、内存上限、执行超时、运行时池） | goja（成熟但只到 ES5.1 + 部分 ES6，无内存上限） |
| 其它 | slog、Prometheus、golang-jwt、go-mail、x/image、cobra、kardianos/service | |

> 关于框架：你们 entropy-platform 用 Kratos，本项目建议**不用**（社区站长部署、贡献门槛）。这一点仍请确认（§12 D1）。
> 关于 JS 沙箱：fastschema/qjs 较年轻（约 600 star）。我们在它外面包一层自己的 `jsrt` 接口，插件只依赖 SDK，不依赖引擎；万一上游停更，可以自己维护 QuickJS-NG 的 WASM 构建或换引擎，插件无感知。

### 2.4 前端选型

| 关注点 | 选择 |
|---|---|
| 框架 | Next.js App Router + React 19 + TypeScript（开工时锁定当时的稳定大版本） |
| 渲染策略 | 公开页（首页、皮肤库、材质详情、用户公开页）= Server Components + SSR；用户中心/后台 = SSR 外壳 + 客户端组件 |
| 数据 | 服务端组件直接调 Go 内部地址（转发 Cookie）；客户端用 TanStack Query；两边共用 orval 从 OpenAPI 生成的客户端与 zod 校验 |
| UI | shadcn/ui + Tailwind CSS v4；所有颜色/圆角/字体都是 CSS 变量 token（主题系统的基础） |
| i18n | next-intl；不在 URL 加语言前缀（保持 BS 的 URL）；语言来自 Cookie / Accept-Language / 用户偏好；导入 BS 13 种语言作为起点；Crowdin |
| 暗色模式 | next-themes |
| 3D 预览 | skinview3d（最新 3.x），封装为 `<SkinViewer>` 并通过 `@skinloom/ui` 开放给插件 |
| 测试 | Vitest + Testing Library、Playwright |

---

## 3. 总体架构

### 3.1 组件与进程

```
┌──────────────────────────── skinloom（Go，单二进制）────────────────────────────┐
│ 启动器 / 监管    up · 子进程管理 · 健康检查 · 日志 · 优雅退出 · 热重载信号            │
│ 网关            唯一端口 · 路径分流 · 静态文件 · 反代 Next.js                       │
│ API 面          /api/v1（Huma/OpenAPI）· /oauth/* · /api/*（BS 兼容）· /events（SSE） │
│ Game 面         CSL · USM · Legacy · /textures · /avatar · /preview · /api/yggdrasil │
│ Worker 面       邮件 · 通知 · 渲染预热 · CDN 刷新 · Webhook · 插件事件与定时任务      │
│ ─────────────────────────────────────────────────────────────────────────────── │
│ 领域服务        auth · user · player · texture · closet · skinlib · score · report  │
│                 notification · oauth · yggdrasil · render · settings · theme · plugin │
│ Hook 总线       Guard · Filter · Event   ← 内置模块 / TS 插件（JS 沙箱）/ Webhook      │
│ 热替换注册表    路由表 · Hook 表 · 定时任务表 · 设置快照 · 主题快照（atomic 指针替换）   │
│ 平台层          db · cache · blob · queue · lock · pubsub · mail · captcha · jsrt     │
└───────────────────────────────────────────────────────────────────────────────┘
          ▲ 内部 HTTP（SSR 取数）                                   │
┌─────────┴────────── skinloom-web（Next.js standalone，Node）──────▼─────────────┐
│ App Router 页面 · 布局/主题注入 · 插件页面容器 · 插槽 · 区块渲染 · SSE 客户端        │
└───────────────────────────────────────────────────────────────────────────────┘
```

`--role=all|api|game|worker|gateway` 决定 Go 进程挂载哪些面，k8s 上可按面拆分扩容；`skinloom up` = `--role=all` + 拉起 web。

### 3.2 部署拓扑

```
桌面/小站（发行包）            中站（Compose）                       大站（k8s / Helm）
┌───────────────────┐    ┌──────────┐ ┌─────┐ ┌──────┐        Ingress / CDN
│ skinloom up       │    │ skinloom │ │MySQL│ │Valkey│          ├─ game 路径 → server(role=game) ×N HPA
│  ├ Go :8080       │    │  (all)   │ └─────┘ └──────┘          ├─ api 路径  → server(role=api)  ×2
│  └ Node(内部:3000)│    │  + web   │                            ├─ 页面      → web(Next.js)      ×2..N
│ SQLite + 本地盘    │    └──────────┘                            └─ worker    → server(role=worker)×1..2
└───────────────────┘                                            外部：MySQL/PG · Valkey · S3/OSS
```

### 3.3 仓库结构（monorepo）

```
skinloom/
├── cmd/skinloom/              up · serve · migrate · import · admin · backup · plugin · theme · doctor · service
├── internal/
│   ├── app/                   按 role 装配、生命周期、热重载协调
│   ├── launcher/              子进程监管（Node）、端口/健康检查、浏览器打开
│   ├── gateway/               路径分流、静态文件、反代
│   ├── config/                静态配置 + 热加载（区分 hot / cold 键）
│   ├── platform/              db · cache · blob · queue · lock · pubsub · mail · captcha · crypto · ratelimit
│   ├── hook/                  Hook 总线与 Hook 目录（catalog，生成 SDK 类型的单一来源）
│   ├── registry/              可原子替换的路由/Hook/Cron/菜单注册表
│   ├── jsrt/                  JS 沙箱抽象 + QuickJS-NG(WASM) 实现
│   ├── modules/               auth user player texture closet skinlib score report notification
│   │                          settings theme oauth yggdrasil gameapi render storage_s3 cdn_* sociallogin importer
│   └── plugin/                插件包管理、签名校验、权限、能力实现（ctx.*）、registry 客户端
├── ent/schema/  migrations/{sqlite,mysql,postgres}/  api/openapi.yaml
├── web/                       pnpm workspace
│   ├── apps/web/              Next.js 应用
│   ├── packages/ui/           @skinloom/ui（设计系统，开放给插件）
│   ├── packages/api-client/   orval 生成
│   ├── packages/plugin-sdk/   @skinloom/plugin-sdk（server / web / testing 三个入口）
│   ├── packages/create-plugin/ create-skinloom-plugin（脚手架）
│   ├── packages/cli/          @skinloom/cli（skinloom-plugin dev/build/pack/sign/publish）
│   └── themes/default/        默认主题（按主题标准实现，吃自己的狗粮）
├── plugins/examples/          官方示例插件（邀请码、自定义登录、首页区块、后台页面……）
├── website/                   Docusaurus 文档站（见 06）
├── deploy/                    packaging/（发行包脚本）· docker/ · compose/ · helm/ · systemd/ · windows/ · macos/
├── docs/adr/                  架构决策记录
└── test/                      契约测试（golden）· e2e · k6
```

### 3.4 模块边界

| 模块 | 职责 | 拥有的表 |
|---|---|---|
| auth | 注册、登录、会话、验证码、邮箱验证、找回密码、社交登录、登录方式扩展点、限流 | sessions, auth_tokens, social_accounts |
| user / score | 资料、偏好、注销 / 积分余额、流水、签到 | users / score_ledger, sign_ins |
| player | 角色、换肤、UUID | players |
| texture / closet / skinlib | 上传管线、可见性、删除、blob 引用计数 / 收藏 / 浏览搜索 | textures, blobs / closet_items / — |
| report / notification | 举报 / 个人通知 + 广播公告 | reports / notifications, announcements, announcement_reads |
| gameapi / render / yggdrasil | 游戏协议 / 头像与预览 / 外置登录 | — / — / ygg_tokens |
| oauth | OAuth2/OIDC 提供方、PAT、BS 兼容 API | oauth_clients, oauth_grants, oauth_tokens |
| settings / theme | 类型化设置、翻译覆盖 / 主题注册、激活、主题设置 | settings, translation_overrides / themes |
| plugin | 安装、启停、权限、存储、调度 | plugins, plugin_kv, plugin_docs |
| audit | 审计 | audit_logs |

跨模块一致性在发起方的**同一事务**内完成；事件只用于对外通知（插件、Webhook、CDN、缓存失效）。

---

## 4. Hook 总线（v1 内核的一部分）

| 类型 | 语义 | 执行时机 | 失败处理 |
|---|---|---|---|
| Guard | 同步，可拒绝并给出原因 | 校验之后、写库之前（事务外） | 每个 hook 声明 fail-open / fail-closed |
| Filter | 同步，改写值 | 同上 | 出错用原值 |
| Event | 异步，至少一次，带幂等键 | 事务提交后，经 outbox 投递 | 重试 + 死信 |

- Hook 目录（catalog）是**单一事实来源**：Go 里定义名称、载荷结构、版本、失败策略 → 生成 JSON Schema → 生成 SDK 的 TS 类型和文档里的 Hook 参考页。
- 命名：`<领域>.<动作>`，例如 `auth.register`（guard）、`player.name`（filter）、`texture.uploaded`（event），载荷带版本 `@1`。完整目录与 BS 对照表见 05 §4。

---

## 5. 数据模型、API 与性能

### 5.1 表设计（落地时以 ent schema 为准）

> 全部表：自增 bigint 主键（players/textures 对外仍用整数 ID，兼容 tid/pid）、`created_at/updated_at`、外键、明确的删除策略。

```
users
  id, email, email_normalized UNIQUE, nickname, password_hash, password_algo,
  role ENUM(user,admin,superadmin), status ENUM(active,banned,deleted),
  score BIGINT CHECK(score >= 0), avatar_texture_id FK→textures NULL ON DELETE SET NULL,
  locale, theme_pref, email_verified_at, register_ip, last_login_at, deleted_at
  INDEX(register_ip, created_at)

players
  id, user_id FK→users ON DELETE CASCADE, name, name_lower UNIQUE,
  uuid CHAR(32) UNIQUE,               -- Yggdrasil UUID 直接落在角色上
  skin_id FK→textures NULL ON DELETE SET NULL, cape_id FK→textures NULL ON DELETE SET NULL,
  renamed_at, updated_at
  INDEX(user_id)

blobs               hash CHAR(64) PK, size, width, height, ref_count INT, created_at
textures
  id, name, type ENUM(steve,alex,cape), hash FK→blobs, size_kb,
  uploader_id FK→users NULL ON DELETE SET NULL, visibility ENUM(public,private),
  likes INT, description, status ENUM(ok,missing,removed), created_at
  INDEX(visibility, type, created_at DESC, id)  -- 皮肤库按时间
  INDEX(visibility, type, likes DESC, id)       -- 皮肤库按热度
  INDEX(uploader_id), INDEX(hash)
  （可选）全文索引：SQLite FTS5 / MySQL FULLTEXT ngram / PG pg_trgm

closet_items        PK(user_id, texture_id), name, created_at, INDEX(texture_id)   -- 两个 FK 均 CASCADE
score_ledger        id, user_id, delta, balance_after, reason, ref_type, ref_id, actor_id, created_at
                    INDEX(user_id, created_at)
sign_ins            PK(user_id, period_key)          -- 同一签到周期只能一行，天然防并发重复签到
reports             id, texture_id NULL, uploader_id, reporter_id, reason, status, handled_by, handled_at
                    UNIQUE(reporter_id, texture_id)
notifications       id, user_id, title, content_md, read_at, created_at, INDEX(user_id, read_at)
announcements       id, title, content_md, audience, created_at
announcement_reads  PK(announcement_id, user_id)
sessions            id(随机), user_id, ip, ua, created_at, last_seen_at, expires_at, revoked_at
auth_tokens         id, user_id, purpose ENUM(verify_email,reset_password,setup), token_hash UNIQUE, expires_at, used_at
social_accounts     PK(provider, subject), user_id
oauth_clients / oauth_grants / oauth_tokens       （令牌存哈希）
ygg_tokens          access_token_hash PK, client_token, user_id, player_id NULL, issued_at, expires_at, refresh_until, revoked_at
                    INDEX(user_id, issued_at)
settings            key PK, value JSON, updated_by, updated_at
translation_overrides  PK(locale, namespace, key), text
themes              id PK, version, source(builtin|package), settings JSON, active BOOL
plugins             id PK, version, status(enabled|disabled|failed), granted_permissions JSON, settings JSON, installed_at
plugin_kv           PK(plugin, key), value, expires_at
plugin_docs         PK(plugin, collection, id), data JSON, s1, s2, s3, n1, n2, n3, t1, created_at, updated_at
                    INDEX(plugin, collection, s1) …（按插件清单声明的索引槽位建复合索引，05 §4.3）
audit_logs          id, actor_id, action, target_type, target_id, ip, meta JSON, created_at
jobs / outbox       任务队列与事件外发
```

### 5.2 一致性规则（对应 03 §7）
- **积分**：所有变动经 `score.Apply(tx, userID, delta, reason, ref)`：`UPDATE users SET score = score + ? WHERE id = ? AND score + ? >= 0`，受影响行数为 0 即"积分不足"；同事务写流水。退款 = 冲正原流水，不按"当前配置"重算（修 C05、C12）。
- **签到**：插入 `sign_ins`，唯一冲突 = 已签到。
- **计数**：`likes = likes ± 1` 原子更新 + 每日校准任务。
- **文件删除**：`blobs.ref_count` 归零后由 worker 删除物理文件（修 C06）。
- **用户注销**：软删除（30 天宽限可恢复）→ 到期后角色、衣柜、通知、会话、令牌级联删除；公开材质保留，上传者显示"已注销用户"（修 C09）。
- **名字**：角色名、邮箱的唯一性只靠数据库约束判定，应用层只做友好提示（修 C01、C02）。

### 5.3 API 设计
- `/api/v1`：REST + JSON；错误统一 RFC 9457 `application/problem+json`（`type`、`title`、`status`、`detail`、`code`、`errors[]`、`request_id`）；列表统一游标分页（`?cursor=&limit=`，limit ≤ 100）；时间 RFC 3339 UTC。
- 认证：浏览器用会话 Cookie（`HttpOnly; Secure; SameSite=Lax`）+ 非安全方法校验 `Origin` 与自定义头（防 CSRF）；Next.js 服务端取数时转发 Cookie 到内部地址；第三方用 OAuth2 Bearer 或个人访问令牌，scope 沿用 BS 命名并补全。
- OpenAPI 3.1 从代码生成，CI 校验 `api/openapi.yaml` 未漂移；前端客户端、文档站 API 参考均由它生成。
- 兼容层：`/api/*`（BS OAuth 资源 API）、游戏协议、`/oauth/*` 由独立 handler 实现，内部调用同一套 Service，契约测试保证不漂移。
- 插件 API：插件自己的接口挂在 `/api/plugins/<id>/...`，同样出现在运行时生成的 OpenAPI 里（标注来源插件）。

### 5.4 性能设计与目标（M5 压测验证）

| 路径 | 设计 | 目标 |
|---|---|---|
| `/{name}.json`、USM、Legacy | `name_lower` 唯一索引单查；进程内 LRU 缓存序列化好的 JSON，角色/材质变更事件失效（多实例经 pub/sub 广播）；不读会话、不写 Cookie；变更时 CDN 主动刷新 | 服务端 p99 < 5 ms |
| `/textures/{hash}` | `Cache-Control: public, max-age=31536000, immutable`，`ETag = hash`；本地盘 `http.ServeContent`（Range/零拷贝）；S3 模式可 302 到 CDN | 静态文件级 |
| `/avatar/*`、`/preview/*` | 参数白名单 → 规范化缓存键 → blob 缓存 `renders/{hash}/{kind}-{size}.{fmt}`；singleflight；有界渲染池 | 命中 p99 < 10 ms，未命中 < 50 ms |
| Yggdrasil `hasJoined` | join 会话进缓存（TTL 30 s）；RSA 私钥启动时解析；签名结果按 (player, updated_at) 缓存 | p99 < 20 ms |
| 皮肤库列表 | 复合索引 + 游标分页 + 总数缓存 60 s；关键字走搜索 provider；Next.js SSR 首屏 | 100 万材质下 API p99 < 50 ms |
| 插件 Hook | JS 运行时池预热；载荷用结构化克隆而非 JSON 往返（视引擎能力） | 额外开销 p99 < 2 ms |
| 进程 | Go 空闲内存 < 100 MB，冷启动 < 1 s；Next.js 进程按官方 standalone 常规占用 | |

---

## 6. 插件与主题（摘要，完整规范见 05）

- **一个标准**：`plugin.json`（JSON Schema 校验、编辑器自动补全）+ TypeScript + `@skinloom/plugin-sdk` + `skinloom-plugin` CLI。`npm create skinloom-plugin` 一分钟起步。
- **后端**：`definePlugin({ setup(ctx) {...} })`，在 JS 沙箱里运行，只能通过 `ctx` 访问宿主能力（Hook、路由、定时任务、KV/文档存储、外呼 HTTP、用户/角色/材质/积分/通知/邮件、登录方式、验证码、CDN、插件间服务……），每项能力需要在清单里声明权限。
- **前端**：`defineWebPlugin({ pages, slots, blocks, settingsPage })`，页面挂到用户中心/后台/探索区，组件塞进命名插槽，首页可用区块；UI 统一用 `@skinloom/ui`。菜单、路由、设置表单尽量**声明式**写在清单里，不写前端代码也能有完整的设置页。
- **主题**：`kind: "theme"`，提供 token（亮/暗）、CSS、布局变体、首页区块编排、组件覆盖；站长可在后台调主题参数，热切换。
- **热加载**：安装/卸载/启停/升级/切换主题全部在线完成，见 §7。
- **市场**：签名的静态 registry + 国内镜像 + 私有源，后台一键安装、权限确认、更新提示。

---

## 7. 热加载设计

| 对象 | 机制 | 生效时间 |
|---|---|---|
| 站点设置（DB） | 保存 → schema 校验 → 写库 → 递增 `config_version` → 本机 + pub/sub 广播 → 订阅模块原子替换快照（邮件客户端重建、验证码 provider 切换、存储/CDN 客户端重建、限流表更新……） | 即时 |
| 文件配置 `config.toml` | fsnotify 监听；**热键**（日志级别、限流、可信代理、CORS、缓存参数、Node 子进程参数）即时生效；**冷键**（监听地址、数据库 DSN、数据目录）在后台标注"需重启"，`skinloom restart` 优雅重启（Linux/macOS 支持监听 socket 交接，零中断；Windows 为快速重启） | 即时 / 重启 |
| 内置模块启停 | 模块注册/注销自己的路由、Hook、定时任务 → 注册表整体替换（copy-on-write + `atomic.Pointer`） | 即时 |
| 插件安装 | 下载 → 验签 → 解压到 `data/plugins/<id>/<ver>/` → 权限确认 → 建 JS 运行时池 → 执行存储声明（建索引）→ `onInstall/onEnable` → 注册表替换 → 版本号递增 → 广播 | 即时 |
| 插件升级 | 新旧版本并存加载 → 新版 `onUpgrade(from)` → 注册表切换到新版 → 旧版**排空**在途调用（≤ 30 s）后卸载 | 即时、不中断请求 |
| 插件卸载 | 停止接收新调用 → 排空 → `onDisable/onUninstall` → 释放运行时 → 删除文件；数据默认保留，`--purge` 才删 | 即时 |
| 主题切换 / 主题参数 | 主题快照替换 → 版本号递增；Next.js 根布局按版本号重新取 token 与 CSS | 下一次请求 |
| 前端感知 | Next.js 服务端：bootstrap（站点信息、主题、菜单、插件前端清单）按 `config_version` 微缓存；浏览器：订阅 `/events` SSE，收到 `config.changed` → `router.refresh()`，插件模块 URL 带版本号自动换新 | 秒级 |
| 多副本 | 所有变更经 Redis pub/sub（无 Redis 时 DB 轮询 `config_version`）通知其它副本；插件包从 blob 存储拉取 | 秒级 |
| Next.js 自身升级 | 属于版本升级，不在热加载范围：替换前端产物后 `skinloom up` 滚动重启 Node 子进程 | 重启子进程 |

实现约束：所有"可被热替换"的东西都不允许在请求处理中被长期持有引用——请求开始时取一次快照，请求内使用同一份，保证一致性。

---

## 8. 安全基线（同 v1，补插件部分）

argon2id + 旧算法透明升级；会话表与一键吊销；一次性令牌存哈希；只信任配置的代理 CIDR；账号+IP 双维度限流；PNG 先验 IHDR；CSP nonce；RBAC（插件安装、自定义 JS、OAuth 全局配置仅超管）；密钥首启生成；发行物 SBOM + cosign；审计日志。

插件相关：权限声明 + 安装/升级时权限增量确认；JS 沙箱内存上限（默认 64 MB/插件）与调用超时（Guard/Filter 2 s、路由 30 s、定时任务 5 min）；外呼 HTTP 白名单 + 内网地址拦截；连续失败熔断并通知超管；插件包与 registry 签名；前端插件同源运行，因此同样要求签名与超管安装。

---

## 9. 构建、发行与部署

### 9.1 发行物

| 发行物 | 内容 | 启动方式 |
|---|---|---|
| `skinloom-<ver>-windows-amd64.zip`（及 arm64） | `skinloom.exe`、`web/`（Next.js standalone）、`runtime/node/`（便携 Node LTS）、`start.bat`、`stop.bat`、`config.example.toml`、`LICENSE`、`NOTICE` | 双击 `start.bat`（= `skinloom up`），首次自动打开浏览器进入安装向导；`skinloom service install` 注册为 Windows 服务 |
| `skinloom-<ver>-darwin-{arm64,amd64}.tar.gz` | 同上，`start.command` | 双击 `start.command`；`skinloom service install` 注册 launchd；可选 Homebrew tap |
| `skinloom-<ver>-linux-{amd64,arm64}.tar.gz` | 同上，`start.sh` | `./start.sh`；`skinloom service install` 注册 systemd；另出 deb/rpm |
| `skinloom-server-<ver>-<os>-<arch>` | 仅 Go 后端 | 前端单独部署时使用 |
| Docker `skinloom/server`、`skinloom/web`、`skinloom/skinloom`（all-in-one） | 多架构 amd64/arm64，非 root，`/data` 卷 | `docker run -p 8080:8080 -v ./data:/data skinloom/skinloom` |
| Compose 模板 | `minimal`（all-in-one + SQLite）、`standard`（server + web + MySQL/MariaDB + Valkey）、`postgres`、`full`（+ MinIO + Caddy 自动 HTTPS） | `docker compose up -d` |
| Helm Chart | server（按 role 拆多个 Deployment）、web、HPA、PDB、Ingress（gateway / split 两种模式）、ServiceMonitor、迁移 Job | `helm install` |

### 9.2 启动器 `skinloom up` 的行为
1. 读取配置（无配置则生成默认 `data/config.toml` 与密钥）。
2. 带锁执行数据库迁移（SQLite 迁移前自动快照）。
3. 启动 Go 各个面，健康检查通过。
4. 选择 Node：优先发行包内 `runtime/node`，否则系统 Node（校验版本）；以内部端口启动 `web/server.js`，注入内部 API 地址等环境变量。
5. 监管：Node 崩溃指数退避重启；日志加前缀合并输出到控制台与 `data/logs/`；Ctrl+C / 服务停止时先停接收、再排空、再退出子进程。
6. 首次运行打印一次性安装链接（带 setup token）并尝试打开浏览器。
7. 其它子命令：`skinloom up --no-web`（只起后端）、`skinloom restart`、`skinloom reload`（触发热键重载）、`skinloom doctor`。

### 9.3 平台注意事项
- **macOS**：未签名的可执行文件会被 Gatekeeper 拦截。正式发行需要 Apple 开发者账号做签名与公证（GoReleaser 支持）；在此之前文档说明"右键打开"或 `xattr -dr com.apple.quarantine`。
- **Windows**：未签名会触发 SmartScreen 提示；可后续申请代码签名证书（开源项目可考虑 SignPath 免费计划）。
- **国内网络**：发行包与镜像同步到国内镜像源；npm 包同步到 npmmirror；插件 registry 有国内镜像；文档提供宝塔/1Panel 指南。
- 便携 Node 的许可证（MIT）随包附带。

---

## 10. 从 BS 迁移

```
1. BS 站点开启维护模式（或停写）
2. skinloom import bs --dsn ... --prefix ... --textures ./storage/textures --env ./.env --dry-run
   → 报告：重名角色、大小写重复邮箱、悬空引用、缺失文件、无法识别的密码算法
3. 人工处理报告中的冲突（或接受默认策略）
4. 正式导入（只读源库；材质目录可直接挂载/复制，hash 文件名不变）
5. 按 02 §6 契约测试清单抽查；用 authlib-injector 实际登录进服一次
6. 切换反向代理/DNS 到 SkinLoom；URL 全部兼容，游戏端无需改配置
7. 回滚：BS 库从未被写入，切回即可
```
用户侧影响：需要重新登录网页与启动器（会话、Yggdrasil 令牌不迁移）；第三方 OAuth 应用需重新授权；密码、角色、UUID、皮肤链接、衣柜、积分不变。表与字段映射见 02 §5。

---

## 11. 项目命名与鸣谢

**推荐：SkinLoom（中文名：云织）**

| 候选 | 含义 | npm 包名 | GitHub 账号 | 同名仓库 | 域名（DNS 粗查） |
|---|---|---|---|---|---|
| **SkinLoom · 云织** | Skin + Loom（织机）：把像素织成皮肤；"云织"取自托管与"云想衣裳"的意象 | 空闲 | 空闲 | 0 | .com 已被注册（停放页）；.dev / .org 未解析，可能可注册 |
| Pixwear · 像素衣橱 | Pixel + Wear | 空闲 | 空闲 | 2 | .com 已注册 |
| Texelia · 纹素 | 源自 texel（纹理像素，中文译名"纹素"） | 空闲 | 空闲 | 2 | .com 已注册 |

（查询时间 2026-10-22，域名只做了 DNS 粗查，注册前以注册商为准；商标需另行检索。）

命名约束已满足：不以 blessing 开头；不含 Minecraft / Mojang 商标。

**已确定（2026-10-23）**：SkinLoom · 云织；源码仓库 <https://github.com/Entropy-Increase-Team/skinloom>（熵增项目组维护，Go 模块 `github.com/Entropy-Increase-Team/skinloom`）；官方域名 **skinloom.cn**；镜像 `ghcr.io/entropy-increase-team/*`。上表的候选与域名粗查结果保留作为当时的决策依据。

**鸣谢落实方式**
- 仓库 `README` 与文档站都设"鸣谢"章节，第一条：**Blessing Skin Server（bs-community）**——本项目的设计参考与致敬对象。
- `NOTICE` 文件：保留 Blessing Skin 的 MIT 版权声明，适用于借用的翻译文本与渲染算法思路。
- 程序内"关于"页面列出鸣谢：Blessing Skin、skinview3d、authlib-injector、CustomSkinLoader、以及所有依赖的开源项目（自动生成许可证清单）。

---

## 12. 待确认事项

| # | 事项 | 当前结论 / 推荐 | 状态 |
|---|---|---|---|
| D0 | 项目名 | SkinLoom · 云织 | **待你确认** |
| D1 | 后端框架 | chi + Huma 模块化单体（不用 Kratos） | 待确认 |
| D2 | 数据访问 | ent + Atlas | 待确认 |
| D3 | 前端 | Next.js + React | ✅ 已定（评审意见 2） |
| D4 | OAuth2 提供方进 v1 | 进 | 待确认 |
| D5 | 插件标准 | TypeScript 单语言 + JS 沙箱；多语言（WASM 原生模块）作为远期可选项 | ✅ 方向已定（评审意见 3），细节见 05 |
| D6 | beta 迁移站点 | 需要一个真实 BS 站 | 待你提供 |
| D7 | 与熵增体系的关系 | 独立开源，仅通过通用 OIDC 登录对接 | 待确认 |
| D8 | 部署形态 | 前后端分离 + 启动器一键运行 | ✅ 已定（评审意见 1） |
| D9 | 热加载范围 | 见 §7，仅 DSN/监听地址/数据目录需重启 | ✅ 已定（评审意见 4），请复核冷键清单 |

---

## 13. 路线图

> 人力假设：1 后端 + 1 前端，兼职 PM/运维。每个里程碑都发 devlog（见 06）。

### 第一阶段 v1.0「能替换 BS」

| 里程碑 | 内容 | 验收 | 估算 |
|---|---|---|---|
| M0 地基 | monorepo、CI（三库矩阵）、配置与热加载、ent/Atlas、Hook 总线 + 可替换注册表、Huma/OpenAPI、**启动器 `up` + 网关**、Next.js 壳（布局、token 主题、i18n、插槽、SSE、bootstrap）、GoReleaser 发行包（含便携 Node）、Docker、**Docusaurus 文档站与 devlog 起步** | 三平台发行包双击可运行；改设置/主题 token 页面秒级生效 | 3–4 周 |
| M1 账号 | 注册/登录/会话/验证码/邮件队列/验证/找回/资料/注销、旧密码算法、限流、可信代理、审计、用户管理 | 03 的 S01–S08、S15 用例通过 | 3 周 |
| M2 材质与角色 | 上传管线、blob、皮肤库（SSR + 游标分页）、详情页（SEO）、衣柜、角色、积分与签到、举报、通知/公告、渲染器、CSL/USM/Legacy | 02 §2.1–2.3、2.5 契约测试通过 | 4–5 周 |
| M3 外置登录与开放平台 | Yggdrasil 全套、OAuth2 提供方 + BS 兼容 API + PAT、社交登录 | authlib-injector 端到端；BS OAuth 客户端可直接用 | 3 周 |
| M4 后台、主题与运维 | 设置中心、**默认主题按主题标准重写 + 首页区块**、翻译覆盖、系统状态、CDN 刷新、备份、Compose、Helm、系统服务 | 四种部署形态按文档从零部署成功；后台切换主题无需重启 | 3 周 |
| M5 迁移与发布 | BS 导入器、契约测试全量、k6 压测、安全自查、真实站点 beta | 真实站点稳定运行 2 周 → **v1.0 GA** | 3 周 |

合计约 **19–21 周**（两人并行）。

### 第二阶段 v2.0「插件平台」（详细拆分见 05 §10）
P2-1 JS 沙箱与 `ctx` 能力 → P2-2 SDK/CLI/脚手架/测试工具 → P2-3 前端扩展与主题开放 → P2-4 包格式/签名/权限确认/热安装 → P2-5 市场与国内镜像 → P2-6 用 SDK 重写 10 个示例插件 + 插件开发文档定稿 → 插件 API 1.0 冻结。

### 第三阶段
Passkey/TOTP、内置 OIDC 提供方开放给 Yggdrasil Connect、Microsoft 正版验证、全文搜索、感知哈希去重与审核队列、合集/标签/评论、OpenTelemetry、多区域部署指南、可选的 WASM 原生插件模块（性能敏感场景）。

---

## 14. 头脑风暴存档（被否决的路线）

1. 逐文件翻译 PHP → Go：解决不了 03 的五个根源。
2. 在 BS 上修：可作为回馈上游的 PR，不作为本项目路线。
3. 微服务（Kratos / go-zero）：社区部署不起来。
4. 前端嵌进 Go 二进制（v1 稿）：被 Next.js 方案取代；保留 `skinloom-server` 单独发行物给只要后端的人。
5. Module Federation 加载插件 UI：`nextjs-mf` 弃用中，App Router 支持有限。
6. 多语言插件（Extism：Rust/Go/JS…）作为主路径：生态会碎片化、文档成本翻倍；按评审意见改为 TypeScript 单一标准，运行时仍是 WASM 沙箱，将来需要时再开放原生模块。
7. goja 作为插件引擎：成熟，但语法只到 ES5.1 + 部分 ES6，且没有内存上限。
8. 兼容运行 BS 的 PHP 插件：复杂度与安全风险都不可接受。

## 15. 开源治理
MIT；`NOTICE` 保留 BS 版权；DCO；Conventional Commits；SemVer（核心、插件 API、SDK、Helm Chart 各自独立版本）；release-please 自动生成 CHANGELOG；GoReleaser + cosign；`SECURITY.md`；govulncheck/CodeQL；Crowdin；GitHub Discussions + QQ 群/Discord；公开路线图。文档与日志的长期维护机制见 [06](06-docs-and-logs.md)。不打包 Mojang 默认材质；BS 的字体与背景图核实许可后再决定是否沿用。
