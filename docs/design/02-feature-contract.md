# 02 · 功能清单与兼容契约（重构验收基线）

> 用途：第一阶段（v1.0）"功能对齐 BS"的**验收清单**。每一行都要在新系统里有对应实现或明确的"放弃/延后"决定。
> 标记：**P1** = 第一阶段必须；**P1+** = 第一阶段必须且要改进行为（见备注）；**P2** = 插件平台阶段；**P3** = 之后；**✗** = 不再提供。
> 兼容等级：**L-A 字节级兼容**（路径、方法、状态码、JSON 字段、头都不能变）；**L-B 语义兼容**（路径与语义保留，可修正错误行为）；**L-C 不兼容**（新前端改用新 API，旧路径 301 或下线）。

---

## 1. 功能清单

### 1.1 访客与认证

| # | 功能 | BS 现状 | 新系统 | 备注 |
|---|---|---|---|---|
| F01 | 首页 | 背景图、站点介绍、透明导航、固定背景、隐藏介绍 | P1 | 后台可配 |
| F02 | 注册 | 邮箱+密码+昵称，或邮箱+密码+**角色名**（`register_with_player_name`）；验证码；`regs_per_ip` | P1+ | 邮箱规范化后唯一；IP 限流基于可信代理；可关闭注册（原 disable-registration 插件）、邀请码（P2 插件化或内置开关） |
| F03 | 登录 | 邮箱或角色名 + 密码；失败 >3 次要验证码；记住我 | P1+ | 账号+IP 双维度限流；不再区分"用户不存在/密码错误" |
| F04 | 验证码 | 内置图形验证码 / reCAPTCHA（可 invisible） | P1+ | Provider 抽象：图形、reCAPTCHA、hCaptcha、**Cloudflare Turnstile**（issue #623/#633） |
| F05 | 忘记/重置密码 | 签名 URL，1 小时有效，可重复使用 | P1+ | 一次性令牌（DB 存哈希）、30 分钟、使用后作废、重置后吊销全部会话与令牌（issue #675） |
| F06 | 邮箱验证 | 永久有效签名 URL；`require_verification` 开启时未验证用户不能管理角色/上传 | P1+ | 一次性令牌；大小写不敏感（issue #581） |
| F07 | 绑定邮箱 | 无邮箱账号（第三方登录注册）强制先绑定 | P1 | 与社交登录同时交付 |
| F08 | 第三方登录 | 插件：GitHub/Google/Microsoft/LittleSkin | P1（GitHub、Microsoft）/P2（其它） | 通用 OAuth2/OIDC 客户端 provider |
| F09 | 安装向导 | `/setup` 写 `.env`；或 `bs:install` | P1+ | 一次性 setup token（控制台打印）防抢装；不写 .env，写数据目录配置 |

### 1.2 用户中心

| # | 功能 | BS 现状 | 新系统 | 备注 |
|---|---|---|---|---|
| F10 | 仪表盘 | 积分、角色数、存储用量、公告（Markdown）、积分说明 | P1 | |
| F11 | 签到 | 随机积分；间隔 N 小时或每日零点 | P1+ | 幂等（同一签到周期唯一约束），时区可配 |
| F12 | 站内通知 | 管理员群发/插件发送，Markdown，标记已读 | P1+ | 广播改"公告表+已读表"，不再 N 行扇出 |
| F13 | 个人资料 | 头像（选材质渲染头部）、昵称、改密码、改邮箱（需密码）、注销账号（管理员不可） | P1+ | 改密/改邮箱吊销其它会话；注销走软删除 + 宽限期 |
| F14 | 暗色模式/语言偏好 | 存在 users 表 | P1+ | `?lang=` 必须校验（issue #684） |
| F15 | 角色管理 | 增（扣分）、改名、删（可返还）、设置/清除皮肤披风 | P1+ | 名称大小写不敏感唯一（DB 约束）；类型校验（披风不能当皮肤）；改名保持 UUID |
| F16 | 衣柜 | 皮肤/披风分栏、搜索、分页、收藏（扣分+奖励上传者）、重命名、移除（返还）、应用到角色、设为头像 | P1+ | 分页上限；积分事务化 |
| F17 | 我的举报 | 列表 | P1 | |
| F18 | OAuth 应用管理 | Passport 自建客户端 | P1 | 新系统内置 OAuth2 Provider |
| F19 | 外置登录配置引导 | yggdrasil 插件的"拖拽配置启动器"卡片 | P1 | authlib-injector 拖拽 URL + 文档 |

### 1.3 皮肤库

| # | 功能 | BS 现状 | 新系统 | 备注 |
|---|---|---|---|---|
| F20 | 浏览 | 类型筛选（skin/steve/alex/cape）、关键字、上传者、排序（时间/收藏数），20/页 | P1+ | 游标分页、排序白名单、索引；总数缓存近似值（issue #614） |
| F21 | 材质详情 | 3D 预览（skinview3d）、上传者+徽章、收藏数、下载原图、收藏、举报、所有者/管理员编辑 | P1 | SEO：服务端注入 OG 标签 |
| F22 | 上传 | 名称规则、类型、公开/私有、尺寸校验、最大宽度、最大文件大小、内容政策、去重、计费 | P1+ | 先读 IHDR 再解码（防解压炸弹）；Go 重新编码去元数据 |
| F23 | 材质编辑 | 重命名、改类型、改公开性、删除 | P1+ | 改类型要重新校验尺寸并处理已引用的角色；删除按引用计数删文件 |
| F24 | 材质描述 | 插件 texture-description | P2（可内置） | |
| F25 | 评论 | 插件 embedded-comment | P3 | |

### 1.4 管理后台

| # | 功能 | BS 现状 | 新系统 | 备注 |
|---|---|---|---|---|
| F30 | 概览 | 用户/角色/材质/存储总数；31 天注册与上传曲线；发通知 | P1+ | SQL 聚合，不再把一个月数据读进内存 |
| F31 | 用户管理 | 搜索（search-string 语法）、改邮箱/验证状态/昵称/密码/积分/权限、删除 | P1+ | 积分调整写流水（带操作人）；删除走统一的级联/软删 |
| F32 | 角色管理 | 搜索、改名、改所属用户、按 tid 设材质、删除 | P1+ | 类型校验 |
| F33 | 衣柜管理 | 仅 API：给用户加/删收藏 | P1 | |
| F34 | 举报管理 | 搜索、删除材质/封禁上传者/驳回，举报者奖惩 | P1+ | 删除材质走统一删除流程（引用计数） |
| F35 | 个性化 | 首页图、favicon、版权文案、导航/侧栏颜色、自定义 CSS/JS | P1+ | 主题色改 CSS 变量；自定义 JS 需超管且带 CSP nonce |
| F36 | 积分设置 | 见 01 §5.5 | P1 | |
| F37 | 常规设置 | 见 §4 选项映射 | P1 | |
| F38 | 资源设置 | CDN 地址、头像/预览缓存开关、缓存时间、强制 SSL、自动检测资源 URL | P1+ | 改为存储/CDN/渲染缓存设置 |
| F39 | 翻译覆盖 | 后台按 group/key 覆盖任意语言文本 | P1 | |
| F40 | 系统状态 | 版本、环境、DB 连接信息、已启用插件 | P1+ | 加健康检查、队列、存储、缓存状态；不显示 DB 用户名 |
| F41 | 插件管理 | 启用/禁用/删除/配置/README；上传 zip/URL 安装 | P2 | 新插件体系（04 §6） |
| F42 | 插件市场 | 远程 registry，依赖检查，安装/更新 | P2 | 签名校验 |
| F43 | 在线更新 | 下载 zip 覆盖站点根目录 | ✗ | 改为"有新版本"提示 + 各平台升级指引（二进制/镜像/Helm） |
| F44 | 浏览器内 CLI | xterm.js，apt/dnf/pacman 装插件、closet 命令 | ✗ | 用真正的 CLI 子命令替代 |
| F45 | 审计日志 | 无 | P1 新增 | 管理操作、积分变动、登录 |

### 1.5 游戏协议与开放接口

| # | 功能 | BS 现状 | 新系统 | 备注 |
|---|---|---|---|---|
| F50 | CSL（CustomSkinAPI R1） | 核心 | P1 · L-A | |
| F51 | 材质文件 | `/textures/{hash}` | P1 · L-A | 加 `immutable` 缓存头、Range、可选 302 到 CDN |
| F52 | 头像/预览渲染 | 2D/3D 头像、全身/披风预览、png/webp、size/height | P1+ · L-B | 尺寸白名单；缓存键包含所有参数 |
| F53 | 原图下载 `/raw/{tid}` | 核心 | P1 · L-A | |
| F54 | USM | 插件 | P1 · L-A | 内置 |
| F55 | Legacy `/skin/{name}.png` | 插件 | P1 · L-A | 内置 |
| F56 | 不存在角色返回 204 | 插件 204-for-unexisted-players | P1 | 内置开关 |
| F57 | Yggdrasil API（authlib-injector） | 插件 yggdrasil-api | P1+ · L-A | 内置；令牌落库；UUID 表原样迁移；修复 §2.4 所列 bug |
| F58 | Yggdrasil Connect（OIDC） | 插件 yggdrasil-connect + Janus | P2 | 内置 OIDC Provider 后可不需要 Janus |
| F59 | 正版验证 | 插件 mojang-verification | P2 | 改为 Microsoft 账号 OAuth 验证 |
| F60 | OAuth2 Provider + 资源 API | Passport + `/api/*` | P1 · L-B | |
| F61 | API 根 `/api` | 返回版本信息 | P1 · L-B | |
| F62 | 新版 REST API `/api/v1` | 无 | P1 新增 | OpenAPI 3.1，前端与第三方共用 |
| F63 | Webhooks | 无 | P2 新增 | |

### 1.6 运维与平台

| # | 功能 | BS 现状 | 新系统 |
|---|---|---|---|
| F70 | 数据库 | MySQL/PG/SQLite | P1：SQLite / MySQL 8+ / MariaDB 10.6+ / PostgreSQL 13+ |
| F71 | 材质存储 | 本地；OSS/COS/七牛靠插件 | P1：本地 + S3 兼容（覆盖 OSS/COS/七牛 Kodo/MinIO/R2） |
| F72 | CDN 刷新 | 插件（阿里/腾讯/Azure） | P1：Provider 抽象，首批阿里云 CDN/ESA、腾讯云、Cloudflare |
| F73 | 缓存/会话 | file 默认，Redis 可选 | P1：内存（单机）/ Redis·Valkey（多实例） |
| F74 | 邮件 | SMTP，同步发送 | P1：SMTP，异步队列 + 重试 |
| F75 | 从 BS 迁移 | — | P1：`import` 子命令 |
| F76 | 部署 | 源码 + PHP 环境 | P1：单二进制（Win/Linux/macOS）、Docker、Compose、Helm（k3s/k8s）、systemd/Windows 服务 |
| F77 | 可观测性 | 日志文件 | P1：结构化日志、Prometheus 指标、健康检查；P2：OpenTelemetry |
| F78 | 备份 | 无 | P1：`backup` 子命令（SQLite 在线备份 + 材质） |

---

## 2. 外部接口契约（L-A / L-B 详表）

### 2.1 CustomSkinLoader / 通用（L-A）

| 方法 路径 | 成功响应 | 失败 |
|---|---|---|
| `GET /{name}.json`<br>`GET /csl/{name}.json` | `200 application/json`<br>`{"username":"<name>","skins":{"<default\|slim>":"<hash>\|null"},"cape":"<hash>\|null"}`<br>头：`Cache-Control: public, max-age=<cache_expire_time>`、`Last-Modified` | 角色不存在 404（开 204 开关时 204）；所属用户被封禁 403 |
| `GET /textures/{hash}`<br>`GET /csl/textures/{hash}` | `200 image/png` 原始字节，`Content-Length`、`Last-Modified`、ETag | 404 |
| `GET /raw/{tid}` | 同上 | 关闭下载 403；不存在 404 |

注意：`skins` 只有一个键，键名 = 皮肤材质的模型（steve→`default`，alex→`slim`，无皮肤→`default: null`）。`name` 匹配**大小写敏感性取决于 DB 排序规则**——MySQL 默认不敏感，PG/SQLite 敏感。新系统统一为**大小写不敏感匹配、返回库内原始大小写**（这也是 yggdrasil-connect 修过的 #232）。

### 2.2 USM（L-A）
`GET /usm/{name}.json` →
```json
{"player_name":"Notch","last_update":1700000000,"model_preference":["default"],"skins":{"default":"<hash>"},"cape":"<hash>|null"}
```
不存在：`404 {"errno":1,"msg":"Player not found."}`；封禁 403。`GET /usm/textures/{hash}` 同 2.1。

### 2.3 Legacy（L-A）
`GET /skin/{name}.png`、`GET /cape/{name}.png` → `200 image/png` 直接返回材质字节；角色/材质不存在 404；封禁 403。

### 2.4 Yggdrasil（L-A，前缀 `/api/yggdrasil`）

| 方法 路径 | 说明 | BS 插件的已知偏差（新系统修正） |
|---|---|---|
| `GET /` | 元数据：`meta{serverName, implementationName, implementationVersion, links{homepage, register?}, feature.non_email_login:true}`、`skinDomains`、`signaturePublickey`（PEM，RSA ≥ 4096） | — |
| `POST /authserver/authenticate` | 邮箱或角色名登录；仅一个角色时自动 `selectedProfile` | 用角色名登录不会绑定该角色（#123）；节流按 `username` 入参计，可被他人刷到锁定 |
| `POST /authserver/refresh` | 可附 `selectedProfile` | 邮箱含大写时刷新出的令牌无法校验（#212） |
| `POST /authserver/validate` / `invalidate` | 204 / 403 | |
| `POST /authserver/signout` | 吊销该用户全部令牌 | 用角色名或大写邮箱 signout 时什么都不会吊销（缓存键大小写/类型不一致） |
| `POST /sessionserver/session/minecraft/join` | 204 | |
| `GET /sessionserver/session/minecraft/hasJoined?username&serverId[&ip]` | 200 带签名 profile / 204 | 角色在 join 与 hasJoined 之间被删 → 500 |
| `GET /sessionserver/session/minecraft/profile/{uuid}[?unsigned=false]` | 200 / 204 | 返回名大小写与库内不符（#232） |
| `POST /api/profiles/minecraft` | 名字数组 → `[{id,name}]`，上限 `ygg_search_profile_max` | N 次查询 |
| `GET /api/users/profiles/minecraft/{name}` | `{id,name}` / 204 | |
| `PUT /api/user/profile/{uuid}/{skin\|cape}` | multipart 上传材质（Bearer） | |
| `DELETE /api/user/profile/{uuid}/{skin\|cape}` | 清除 | |
| `GET /minecraftservices/publickeys` | `profilePropertyKeys`、`playerCertificateKeys` | |
| 响应头 | `X-Authlib-Injector-API-Location: <site>/api/yggdrasil`（开关 `ygg_enable_ali`，加在**全站**响应上） | |

**UUID 规则（必须延续）**：
- 已有映射：以迁移过来的 `uuid` 表为准（含 v4 随机生成的历史数据）。
- 新角色：`ygg_uuid_algorithm=v3` → `md5("OfflinePlayer:" + name)` 设置版本位 3（与离线服一致）；`v4` → 随机。
- v4 模式下改名保持 UUID；v3 模式下改名 = 新 UUID（与离线服行为一致，这是预期）。
- 新系统把 UUID 存进 `players.uuid`（唯一），关联键从"角色名"改为角色 ID（修 #151/#152/#202）。

**令牌**：JWT HS256，`iss=Yggdrasil-Auth`、`sub=uuid5(DNS, email)`、`yggt`、`spr`；新系统沿用格式（启动器不解析，但调试工具会看），**持久化到 DB**（存哈希）+ 缓存前置，过期参数 `ygg_token_expire_1/2`、单用户上限 `ygg_tokens_limit` 照搬。

### 2.5 头像与预览（L-B）

| 路径 | 参数 | 新系统约束 |
|---|---|---|
| `GET /avatar/player/{name}` | `size`(默认 100)、`3d`、`png` | `size` 取白名单 {16,24,32,48,64,96,100,128,256,512}，其它值就近取整 |
| `GET /avatar/user/{uid}` | 同上 | |
| `GET /avatar/hash/{hash}` | 同上 | |
| `GET /avatar/{tid}` | 同上 | 披风返回 422（保持） |
| `GET /preview/{tid}` | `height`(默认 200)、`png` | `height` 白名单；缓存键含 height（BS 漏了） |
| `GET /preview/hash/{hash}` | 同上 | |

默认输出 WebP（客户端不支持或 `?png` 时 PNG），保持。

### 2.6 OAuth2 与资源 API（L-B）

| 路径 | scope |
|---|---|
| `GET /oauth/authorize`、`POST /oauth/token`（authorization_code / refresh_token / client_credentials? / password?） | — |
| `GET /api/user` | `User.Read` |
| `GET /api/user/notifications`、`POST /api/user/notifications/{id}` | `Notification.Read` |
| `GET /api/players`；`POST`、`DELETE /{pid}`、`PUT /{pid}/name`、`PUT|DELETE /{pid}/textures` | `Player.Read` / `Player.ReadWrite` |
| `GET /api/closet`；`POST`、`PUT /{tid}`、`DELETE /{tid}` | `Closet.Read` / `Closet.ReadWrite`（BS 定义成了拼错的 `Closet.ReadWrtie`，新系统两者都接受） |
| `/api/admin/users|players|closet|reports`、`POST /api/admin/notifications` | `*Management.Read/ReadWrite`、`Notification.ReadWrite` |

OAuth 客户端（`oauth_clients`）迁移：client_id 保留；secret 若在 BS 中是明文则导入后哈希化；redirect URI 保留。已签发的 access/refresh token **不迁移**（用户重新授权）。

### 2.7 BS 页面内部 JSON 接口（L-C）
`/user/*`、`/texture/*`、`/skinlib/list`、`/admin/*` 这些是给 BS 自带页面用的，第三方一般不会依赖 → 新前端全部改用 `/api/v1`，旧路径不保留（页面类路径如 `/skinlib/show/{tid}`、`/user/closet` 作为 Next.js 页面路由保留，旧书签照样能打开）。

---

## 3. 新系统必须保留的页面 URL（Next.js 页面路由，L-B）

`/`、`/auth/login`、`/auth/register`、`/auth/forgot`、`/auth/reset/*`、`/auth/verify/*`、`/user`、`/user/closet`、`/user/player`、`/user/profile`、`/user/reports`、`/user/oauth/manage`、`/skinlib`、`/skinlib/show/{tid}`、`/skinlib/upload`、`/admin/**`、`/.well-known/change-password`（→ `/user/profile`）。

---

## 4. 配置项映射（BS option → 新 settings）

| BS option | 新键（分组.键） | 类型 | 说明 |
|---|---|---|---|
| site_name / site_description / site_url | `site.name` / `site.description` / `site.url` | i18n string / url | 多语言值内建（替代 `*_{locale}` 后缀键） |
| announcement / content_policy / copyright_text / copyright_prefer | `site.announcement` / `texture.content_policy` / `site.copyright_html` / `site.powered_by_style` | i18n markdown | |
| home_pic_url / favicon_url / transparent_navbar / hide_intro / fixed_bg / navbar_color / sidebar_color / custom_css / custom_js | `theme.*` | | 颜色改为主题 token |
| meta_keywords / meta_description / meta_extras | `seo.*` | | |
| register_with_player_name / require_verification / regs_per_ip | `auth.register_with_player_name` / `auth.require_email_verification` / `auth.max_registrations_per_ip` | bool/bool/int | |
| recaptcha_sitekey / recaptcha_secretkey / recaptcha_invisible | `captcha.provider` + `captcha.<provider>.*` | | |
| player_name_rule / custom_player_name_regexp / player_name_length_min / max | `player.name_rule` / `player.name_regexp` / `player.name_length` | enum/regex/range | |
| user_initial_score / sign_score / sign_gap_time / sign_after_zero / score_per_* / private_score_per_storage / return_score / take_back_scores_after_deletion / score_award_per_* / reporter_* | `score.*` | int | `sign_score "10,100"` → `{min,max}` |
| max_upload_file_size / max_texture_width / texture_name_regexp / allow_downloading_texture / status_code_for_private / auto_del_invalid_texture | `texture.*` | | `auto_del_invalid_texture` ✗（改为后台"孤儿检查"任务） |
| cache_expire_time / enable_avatar_cache / enable_preview_cache / cdn_address / force_ssl / auto_detect_asset_url | `delivery.*` | | `force_ssl`/`auto_detect_asset_url` 由 `site.url` + 可信代理推导 |
| plugins_enabled / version | — | | 插件状态独立表；版本由迁移表决定 |
| ygg_* | `yggdrasil.*` | | 私钥移入密钥存储（加密） |

---

## 5. 数据迁移映射（BS → 新库）

| BS 表 | 新表 | 转换 |
|---|---|---|
| users | users | `email` 小写规范化（冲突时报告人工处理）；`permission` → `role`+`status`；`password` 保留并记录算法（读 BS `.env` 的 `PWD_METHOD`、`SALT`）；`ip` → `register_ip`；`avatar` 0 → NULL |
| players | players | `tid_skin/tid_cape` 0/-1 → NULL；悬空引用（材质已删）→ NULL；重名（并发 bug 产物）→ 报告，保留最早的，其余追加后缀并通知用户；补 `uuid`（来自插件 uuid 表或按算法生成） |
| uuid（插件） | players.uuid | 按名关联；一名多 UUID 时取最早记录；UUID 无对应角色的孤儿行丢弃（写报告） |
| textures | textures + blobs | hash 去重生成 blobs（引用计数）；校验文件存在性，缺失的标记 `missing` 不删 |
| user_closet | closet_items | 去重（无主键导致的重复行）；丢弃悬空行 |
| reports | reports | |
| notifications | notifications | 只迁移未读 |
| options | settings | 按 §4 映射 |
| language_lines | 设置 `i18n.overrides` | 按语言与消息键（ADR 0031）；BS 的 group/key 换成 SkinLoom 的消息键，对不上的写进报告 |
| oauth_clients | oauth_clients | 见 2.6 |
| ygg_log | （可选）audit_log | 默认不迁移 |
| 其它插件表 | — | 按插件逐个出转换器（邀请码、正版验证等） |

迁移器要求：只读源库、可 dry-run、幂等可重跑、输出报告（冲突/修复/丢弃明细）、支持 MySQL/PG/SQLite 源、支持表前缀（`DB_PREFIX`）。

---

## 6. 契约测试清单（CI 必跑）

1. **黄金文件**：在真实 BS 6.0.2 + 插件环境里录制 CSL/USM/Legacy/Yggdrasil 全部端点的响应（覆盖存在/不存在/封禁/私有/无皮肤/slim/披风），新系统逐字段比对（时间戳类字段忽略）。
2. **authlib-injector 端到端**：CI 里用 authlib-injector 的 Java 测试工具（或最小 Java harness）跑 authenticate → join → hasJoined → 签名验证。
3. **密码兼容**：10 种 BS 算法 × 有/无盐的测试向量，确认导入后能登录并自动升级为 argon2id。
4. **迁移回归**：一份脱敏的真实 BS 库（含重名、悬空、重复衣柜等脏数据）跑 import，断言报告与结果。
5. **渲染快照**：头像 2D/3D、全身预览、披风预览对标 BS 输出（像素差阈值）。
