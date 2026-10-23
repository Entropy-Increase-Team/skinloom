# 01 · Blessing Skin Server 架构拆解

> 基线版本：`bs-community/blessing-skin-server` dev 分支 @ `52f6fefe`（2026-03-30，package.json 版本 6.0.2，Laravel 10 / PHP ≥ 8.1）
> 参考仓库：`blessing-skin-plugin-docs` @ 2026-03-30、`blessing-skin-plugins` @ 2026-01-14、`skinview3d` 3.4.2
> 本文只描述"现状是什么、怎么跑起来的"；缺陷见 [03](03-bs-defects.md)，重构决策见 [04](04-rewrite-plan.md)。

---

## 1. 一句话定位

Blessing Skin（下称 BS）是一个**自托管的 Minecraft 皮肤站**：用户上传皮肤/披风材质，把材质"穿"到自己的**角色（Player）**上；游戏客户端或服务端通过若干**皮肤加载协议**（CustomSkinLoader/CSL、UniSkinMod/USM、Legacy、Yggdrasil/authlib-injector）按角色名拉取材质。它同时承担了一个**外置登录（Yggdrasil）认证服务器**的角色——这一部分在 BS 里是插件，但实际上是绝大多数站点的刚需。

```
 ┌────────────┐  浏览器 (Twig 页面 + React 孤岛)   ┌──────────────────────────────┐
 │  站点用户   │ ─────────────────────────────────▶ │                              │
 └────────────┘                                    │      Blessing Skin Server     │
 ┌────────────┐  CSL/USM/Legacy: GET /{name}.json   │  Laravel 10 · PHP-FPM/Apache │
 │ 游戏客户端  │  GET /textures/{sha256}            │                              │
 │ (CSL Mod)  │ ─────────────────────────────────▶ │  ┌───────── 插件 ─────────┐   │
 └────────────┘                                    │  │ yggdrasil-api / usm …   │   │
 ┌────────────┐  authlib-injector:                  │  └─────────────────────────┘   │
 │ 启动器/服务端│  /api/yggdrasil/authserver/*       │                              │
 │ (Paper等)  │  /api/yggdrasil/sessionserver/*    │                              │
 └────────────┘ ─────────────────────────────────▶ └──────┬──────────┬────────────┘
 ┌────────────┐  OAuth2 (Laravel Passport)                │          │
 │ 第三方应用  │  /oauth/* + /api/*                ┌──────▼───┐ ┌────▼──────────────┐
 └────────────┘ ─────────────────────────────────▶ │ MySQL/PG/ │ │ storage/textures/ │
                                                   │ SQLite    │ │ (sha256 命名 PNG)  │
                                                   └──────────┘ └───────────────────┘
```

## 2. 技术栈与运行模型

| 层 | 现状 | 备注 |
|---|---|---|
| 语言/框架 | PHP ≥ 8.1，Laravel 10 | 两者均已过安全支持期（Laravel 10 安全修复止于 2025-02，PHP 8.1 止于 2025-12），见上游 issue #674 |
| 运行模型 | PHP-FPM / Apache mod_php，**shared-nothing**：每个请求重新引导整个框架 + 扫描插件 + 读配置 | 性能问题的根源之一 |
| 模板 | Twig（`rcrowe/twigbridge`）为主，少量 Blade（插件配置页、邮件） | |
| 前端 | Twig 服务端页面 + **React 17 "孤岛"**（按 URL 正则挂载）+ jQuery + AdminLTE 3 + Bootstrap 4，webpack 5 打包 | `resources/assets/src` |
| 3D 预览 | skinview3d `^3.0.0-alpha.1`（上游已到 3.4.2） | |
| 图像 | `intervention/image` v2（**imagick 驱动**）+ `blessing/texture-renderer`（GD，渲染头像/预览） | 必须同时装 gd + imagick |
| 认证 | Laravel Session（web guard）+ Laravel Passport 11（OAuth2，api guard `oauth`） | |
| 数据库 | MySQL/MariaDB、PostgreSQL、SQLite | 迁移 20 个，**无任何索引/唯一约束** |
| 缓存/会话/队列 | 默认 `file` / `file` / `sync` | 无法水平扩展 |
| i18n | YAML 语言包 13 种语言（`resources/lang`）+ DB 覆盖（`spatie/laravel-translation-loader`）+ Crowdin | 前端语言包运行时编译成 `public/lang/{locale}.js` |
| 部署 | 上传源码包到虚拟主机/宝塔，或自建 Docker（仓库 Dockerfile） | 要求 `public/`、`plugins/`、`storage/`、`bootstrap/cache` 可写 |

### 2.1 一个请求的生命周期

```
public/index.php
 ├─ bootstrap/chkenv.php        # 每个请求检查 PHP 扩展 + 目录可写（缺 imagick 直接 die）
 ├─ bootstrap/app.php           # 创建 Application
 └─ Http\Kernel
     ├─ 全局中间件: PreventRequestsDuringMaintenance → ValidatePostSize → TrimStrings
     │             → ConvertEmptyStringsToNull → DetectLanguagePrefer(?lang/cookie/Accept-Language)
     ├─ ServiceProviders.boot
     │    ├─ AppServiceProvider      # 根据 option(site_url/force_ssl) 强制 URL 根与 https
     │    ├─ AuthServiceProvider     # 注册 Passport scopes（DB scopes 表 + 缓存）
     │    ├─ PluginServiceProvider   # ★ 扫描 plugins 目录、读每个 package.json、注册 autoload、执行 bootstrap.php
     │    └─ RouteServiceProvider    # 加载 static.php / web.php / api.php，然后派发 ConfigureRoutes 让插件加路由
     ├─ 路由组中间件
     │    ├─ static（无组）: 材质/JSON/头像/预览 —— 无 session
     │    ├─ web: Cookie加密 → Session → CSRF → EnforceEverGreen(拒绝旧浏览器) → RedirectToSetup(未安装跳转/版本变化时在请求内跑 update) → SubstituteBindings
     │    ├─ authorize: auth:web → RejectBannedUser → EnsureEmailFilled → FireUserAuthenticated
     │    └─ api: SubstituteBindings + throttle:60,1
     └─ Controller → json()/view()
          └─ View Composers 注入：站点名、菜单(派发 Configure*Menu)、未读通知、语言菜单、head/foot 注入(插件脚本/样式、custom_css/js)
```

关键点：**插件在 `PluginServiceProvider::boot` 阶段以普通 PHP 代码的方式同进程执行**，可以注册路由、中间件、事件监听、过滤器、视图，甚至替换容器绑定。

## 3. 目录结构（与职责）

```
blessing-skin-server/
├── app/
│   ├── Console/Commands/      bs:install · options:cache · plugin:enable/disable · salt:random · update
│   ├── Events/                23 个类事件（Player*/Plugin*/User*/Rendering*/Configure*Menu/TextureDeleting）
│   ├── Exceptions/            Handler（JSON 异常带裁剪后的 trace）、PrettyPageException
│   ├── Http/
│   │   ├── Controllers/       21 个控制器，业务逻辑几乎全写在这里（胖控制器）
│   │   ├── Middleware/        13 个
│   │   └── View/Composers/    6 个视图合成器（菜单、head/foot 注入、用户面板）
│   ├── Listeners/             9 个（积分返还、衣柜清理、重置角色材质、复制插件资源…）
│   ├── Models/                User · Player · Texture · Report · Scope (+ HasPassword trait)
│   ├── Notifications/         SiteMessage（database 通道）
│   ├── Rules/                 Captcha（图形/reCAPTCHA）· PlayerName（四种命名规则）
│   ├── Services/
│   │   ├── Cipher/            10 种密码哈希算法（BCRYPT/ARGON2I/MD5/SALTED2MD5/SHA256/…）
│   │   ├── Option.php         站点配置（options 表 KV）
│   │   ├── OptionForm.php     管理后台配置表单 DSL（562 行）
│   │   ├── PluginManager.php  插件发现/启停/依赖/冲突
│   │   ├── Hook.php           插件钩子 API（菜单/路由/脚本/样式/通知/中间件）
│   │   └── Translations/      YAML 加载器 + 前端语言包生成
│   └── helpers.php            json() / option() / option_localized() / plugin()
├── config/                    options.php（51 个默认配置）· menu.php · plugins.php（市场源）· secure.php（密码算法/盐）…
├── database/migrations/       20 个迁移（2016 → 2025）
├── resources/
│   ├── assets/src/            前端 TS/React（74 个视图文件 + 组件 + 浏览器内 CLI）
│   ├── views/                 Twig 模板（~100 个）
│   ├── lang/                  13 种语言 YAML
│   └── misc/backgrounds/      7 张首页背景
├── routes/                    web.php · api.php · static.php
├── plugins/                   插件目录（可用 PLUGINS_DIR 指定多个）
└── storage/textures/          材质文件（文件名 = sha256）
```

## 4. 领域模型与数据库

### 4.1 ER 图（现状）

```
users (uid PK)                          players (pid PK)
 ├ email        varchar(100)  ✗无唯一    ├ uid         int     ✗无索引/无外键 ──▶ users.uid
 ├ nickname     varchar(50)              ├ name        varchar(50) ✗无唯一（应用层校验，存在并发重名 #443）
 ├ locale       nullable                 ├ tid_skin    int  (0/-1 表示无) ──▶ textures.tid
 ├ score        int                      ├ tid_cape    int  (0 表示无)    ──▶ textures.tid
 ├ avatar       int (tid, 0=无)          └ last_modified datetime (UPDATED_AT)
 ├ password     varchar(255)
 ├ ip           varchar(45)   ✗无索引（注册限流时全表 count）
 ├ is_dark_mode bool                    textures (tid PK)
 ├ permission   int  -1封禁/0普通/1管理/2超管  ├ name     varchar(50)
 ├ last_sign_at / register_at           ├ type     varchar(10)  steve|alex|cape
 ├ verified / verification_token(未使用) ├ hash     varchar(64)  sha256，✗无索引；同一 hash 可多行（私有重复上传）
 └ remember_token                       ├ size     int (KB)
                                        ├ uploader int ✗无索引 ──▶ users.uid
user_closet（无主键！）                    ├ public   tinyint
 ├ user_uid     int ✗无索引               ├ likes    int  反范式计数（= 收藏人数）
 ├ texture_tid  int ✗无索引               └ upload_at datetime
 └ item_name    text
                                        reports (id PK)
options (id PK)                          tid · uploader · reporter · reason(longtext) · status(0/1/2) · report_at
 ├ option_name  varchar(50) ✗无唯一
 └ option_value longtext                 notifications (uuid PK, Laravel 标准, morphs 有索引)
                                        jobs (Laravel 队列)        language_lines (i18n 覆盖)
scopes (id, name UNIQUE, description)   oauth_* (Passport 5 张表)
```

**插件建的表**（最重要的是 yggdrasil-api）：
- `uuid (id, name varchar, uuid varchar(255))` —— 角色名 ↔ 游戏 UUID 映射，**无唯一约束、无索引**，按 `name` 关联而非 `pid`
- `ygg_log (action, user_id, player_id, parameters, ip, time)`
- 其它：`mojang_verifications`、邀请码、`texture-description` 等

### 4.2 核心概念

| 概念 | 说明 |
|---|---|
| User | 站点账号，用邮箱登录（也可用自己名下的角色名登录）；有积分、权限等级、头像（引用某个材质的 tid 渲染头部） |
| Player（角色） | 游戏内名字。一个用户可拥有多个角色；角色绑定一个皮肤 tid + 一个披风 tid。**皮肤模型（default/slim）不存在角色上，而是由皮肤材质的 type（steve/alex）推导** |
| Texture（材质） | 一张 PNG，type ∈ {steve, alex, cape}；按 sha256 内容寻址存储；有公开/私有；`likes` = 被收藏次数 |
| Closet（衣柜） | 用户收藏的材质列表（多对多 + 自定义名）。**角色只能使用衣柜里的材质**；上传即自动入衣柜 |
| Score（积分） | 站内经济：注册送分、签到得分；上传按 KB 扣分（私有更贵）、建角色扣分、收藏扣分；被收藏/上传可奖励；删除可返还 |
| Report（举报） | 对材质举报，管理员处理：删除材质 / 封禁上传者 / 驳回；可对举报者奖惩积分 |
| Option | 站点全局配置 KV（字符串存储，`'true'/'false'/'(null)'` 字符串转布尔/空） |
| Notification | 站内信（管理员群发或插件发送），Markdown |
| Plugin | 同进程 PHP 扩展，见 §8 |

## 5. 核心业务流程（代码级）

### 5.1 上传材质 `POST /texture`（`SkinlibController::handleUpload`）

```
校验 name(可选正则)/file(png, ≤ max_upload_file_size KB)/type/public
 → filter: uploaded_texture_file / uploaded_texture_name / can_upload_texture(可拒绝)
 → getimagesize：宽 ≤ max_texture_width；宽%64==0 且 高%32==0
   皮肤：宽高比 2:1 或 1:1（alex 必须 1:1）；披风：2:1
 → Imagick 重新编码 PNG（去掉所有 chunk、压缩 9）→ sha256(编码后字节) = hash
 → filter: uploaded_texture_hash
 → 去重：存在 hash 相同且 (公开 或 自己上传) 的材质 → 返回 code=2 + 已有 tid
 → 计费：ceil(字节/1024) × (公开? score_per_storage : private_score_per_storage)
         + score_per_closet_item − score_award_per_texture
 → 插入 textures(likes=1) → 文件不存在则写入 storage/textures/{hash}
 → user.score -= cost；自动加入衣柜 → event texture.uploaded
```

### 5.2 角色换肤 `PUT /user/player/{pid}/textures`（`PlayerController::setTexture`）
请求体 `{skin?: tid, cape?: tid}` → 对 skin/cape 分别：filter `can_set_texture` → 材质存在 → **材质必须在自己衣柜** → 写 `tid_skin`/`tid_cape` → 每个字段单独 `save()`（触发 `PlayerProfileUpdated`）。

### 5.3 游戏拉取皮肤（CSL，`TextureController::json`）
```
GET /{name}.json  或  /csl/{name}.json
 → Player::where('name', $name)->firstOrFail()        # 无索引，全表扫描
 → 若所属用户被封禁 → 403
 → Player::toJson()  (CustomSkinAPI R1)
   {"username":"Notch","skins":{"default"|"slim": "<hash>"},"cape":"<hash>"}
 → Cache-Control: public, max-age=cache_expire_time(默认 1 年) · Last-Modified=last_modified

GET /textures/{hash}  → 读整个文件进内存返回 image/png（etag 中间件对 body 做 md5）
```

### 5.4 Yggdrasil 外置登录（yggdrasil-api 插件）

```
启动器                                   皮肤站 /api/yggdrasil
 POST authserver/authenticate {username,password,clientToken}
   → 账号可为邮箱或角色名 → 校验密码/封禁/邮箱验证
   → 签发 JWT accessToken(HS256, sub=uuid5(email), yggt=随机, spr=角色UUID?)
   → Token 对象序列化存 Cache: yggdrasil-token-{accessToken}（TTL=ygg_token_expire_2）
     并追加到 Cache: yggdrasil-id-{email} 列表（最多 ygg_tokens_limit 个）
 POST authserver/refresh | validate | invalidate | signout
 POST sessionserver/session/minecraft/join {accessToken, selectedProfile, serverId}
   → Cache: yggdrasil-server-{serverId} = {profile, ip}  (120s)
游戏服务端
 GET sessionserver/session/minecraft/hasJoined?username&serverId[&ip]
   → 取 Cache → Profile::serialize(false) 用 RSA 私钥(option ygg_private_key)签名 textures 属性
 GET sessionserver/session/minecraft/profile/{uuid}[?unsigned=false]
 POST api/profiles/minecraft  (批量按名查 UUID，上限 ygg_search_profile_max)
 PUT/DELETE api/user/profile/{uuid}/{skin|cape}  (Bearer，上传材质)
 GET  /api/yggdrasil  → 元数据（站点名、skinDomains、signaturePublickey…）
 响应头 X-Authlib-Injector-API-Location（ALI，可选）
UUID 生成：v3 = 离线模式算法 md5("OfflinePlayer:"+name)（兼容盗版服） / v4 = 随机
```

**令牌只存在缓存里**：缓存被清（例如 `php artisan update` 会 `Cache::flush()`）= 全站玩家掉线。

### 5.5 积分规则一览

| 动作 | 积分变化 | 配置项 |
|---|---|---|
| 注册 | + `user_initial_score`（默认 1000） | |
| 签到 | + rand(`sign_score` 区间，默认 10~100)；间隔 `sign_gap_time` 小时，或 `sign_after_zero` 每日零点重置 | |
| 新建角色 | − `score_per_player`（100） | 删角色时若 `return_score` 返还 |
| 上传材质 | − KB×`score_per_storage`（公开，默认 1）/ KB×`private_score_per_storage`（私有，10）− `score_per_closet_item` + `score_award_per_texture` | 删除时按 `return_score`、`take_back_scores_after_deletion` 返还/收回 |
| 切换公开/私有 | 补差价；公开→私有还会收回上传奖励 | |
| 收藏 | − `score_per_closet_item`；上传者 + `score_award_per_like` | 取消收藏返还/收回 |
| 举报 | ± `reporter_score_modification`；举报成立 + `reporter_reward_score` | 驳回时扣回正向修改 |

## 6. 接口面（三套路由）

完整列表见 [02-功能清单与兼容契约](02-feature-contract.md) §3，这里只给结构：

| 路由文件 | 中间件 | 内容 |
|---|---|---|
| `routes/static.php` | 无 session | `/{player}.json`、`/csl/{player}.json`、`/textures/{hash}`、`/csl/textures/{hash}`、`/raw/{tid}`、`/avatar/*`、`/preview/*` |
| `routes/web.php` | web(session+CSRF) | 页面 + 页面用的 JSON 接口：`/auth/*`、`/user/*`、`/texture/*`、`/skinlib/*`、`/admin/*`、`/setup/*` |
| `routes/api.php` | api + `throttle:60,1` + `auth:oauth` + scope | OAuth2 资源 API：`/api/user`、`/api/players`、`/api/closet`、`/api/admin/*` |
| Passport | | `/oauth/authorize`、`/oauth/token`、`/oauth/clients`（用户自建 OAuth 应用）… |
| 插件 | `ConfigureRoutes` 事件 | yggdrasil `/api/yggdrasil/*`、usm `/usm/*`、legacy `/skin/{name}.png` `/cape/{name}.png` … |

**统一响应格式（web/api 的业务接口）**：`{"code": 0, "message": "...", "data": {...}}`，`code=0` 成功；非 0 失败（1 通用失败、2 重复/需要验证码等、7 积分不足、-1 被封禁）。HTTP 状态码大多仍是 200，校验失败为 422（Laravel 默认格式）。

## 7. 配置系统（Option）

- 存储：`options` 表 KV，值一律字符串；默认值在 `config/options.php`（51 项）。
- 读取：`option($key)` → `Option::get`：若内存里没有但 config 里有默认值，**就在读的时候写 DB**；字符串 `'true'/'false'/'null'`（含括号形式）转成 PHP 类型。
- 每个请求启动时把整张 options 表读进内存；可用 `php artisan options:cache` 导出为 `storage/options.php` 跳过 DB（但之后改配置需要重新导出）。
- 多语言配置：`option_localized('site_name')` 优先读 `site_name_{locale}`。
- 后台表单：`OptionForm` DSL（text/checkbox/select/textarea/group/addon/hint），插件也可用它生成配置页。
- 敏感/基础设施配置在 `.env`：DB、`PWD_METHOD`、`SALT`、`APP_KEY`、邮件、`CACHE_DRIVER`、`PLUGINS_DIR`、`PLUGINS_REGISTRY`。

## 8. 插件系统

### 8.1 插件包结构
```
my-plugin/
├── package.json      必需：name, version, title, description, author, namespace, require, enchants{providers,conflicts,config,icon}, config
├── bootstrap.php     必需：return function (Dispatcher $events, Filter $filter, $plugin, ...) {...}（容器自动注入）
├── callbacks.php     可选：[PluginWasEnabled::class => fn, PluginWasDisabled::class => fn, PluginWasDeleted::class => fn]
├── src/              PSR-4 autoload 到 namespace
├── views/            视图（命名空间 = namespace），config.blade.php 为默认配置页
├── lang/{en,zh_CN}/  语言包；front-end.yml 会合并到前端 blessing.i18n[plugin-name]
├── assets/           启用时被复制到 public/plugins/{name}/assets
└── vendor/           可自带 composer 依赖（自动 require autoload.php）
```

### 8.2 加载与生命周期
1. **每个请求**：`PluginManager::all()` 列出 `PLUGINS_DIR` 下所有子目录 → 读 `package.json` → 对比 `option('plugins_enabled')`（JSON 数组 `[{name, version}]`）→ 检查依赖（`require`：`blessing-skin-server`/`php`/其它插件的 semver）与冲突。
2. 所有插件（含未启用）注册视图与翻译命名空间；**已启用**插件注册 PSR-4、加载 vendor、注册 ServiceProvider、执行 `bootstrap.php`。
3. bootstrap 抛异常：记录日志、派发 `PluginBootFailed`（给管理员页面弹 toast），debug 模式下直接抛出。
4. 启用/禁用/删除时执行 `callbacks.php`（插件自行建表——没有迁移机制，靠 `Schema::hasTable` 判断）。
5. 版本号变化时重新复制 assets、清理前端语言包缓存。

### 8.3 扩展点（插件能做什么）

| 类别 | API | 数量 |
|---|---|---|
| Hook 静态方法 | `addMenuItem(user/admin/explore)`、`addRoute`、`addStyleFileToPage`、`addScriptFileToPage`、`addUserBadge`、`sendNotification`、`pushMiddleware` | 7 |
| 类事件 | `ConfigureRoutes`、`Configure{User,Admin,Explore}Menu`、`Rendering{Header,Footer}`、`Player*`、`User*`、`Plugin*`、`TextureDeleting` | 23 |
| 字符串事件 | `auth.login.attempt/ready/succeeded/failed`、`player.add.attempt/adding/added`、`texture.uploading/uploaded`、`closet.*`、`report.*`、`user.*.updating/updated`… | 72 |
| 过滤器（`blessing/filter`） | 守卫类 `can_*`（返回 `Rejection` 即拒绝，共 16 个）；数据类 `client_ip`、`user_password`、`verify_password`、`new_player_name`、`uploaded_texture_*`、`user_avatar`、`user_badges`、`head_links`、`scripts`、`side_menu`、`user_menu`；页面布局类 `grid:*`（8 个）、`auth_page_rows:*` | 45 |
| 前端 | 全局 `blessing`（base_url、locale、route、extra、i18n、fetch、event、notify）+ `window.React/ReactDOM/$`；`blessing.event.on/emit`（beforeFetch、fetchError、mounted） | |
| 其它 | 替换容器绑定、注册中间件、自定义 Artisan 命令、自定义 OAuth scope（写 `scopes` 表） | |

**本质**：插件 = 拥有全部权限、与核心同进程、无隔离、无版本化契约的 PHP 代码。灵活，但也意味着核心任何内部改动都可能弄坏插件。

### 8.4 插件市场
- 源：`PLUGINS_REGISTRY`（默认 `https://bs-plugins.littleservice.cn/registry_{lang}.json`，可逗号分隔多个），返回 `{packages: [{name, version, title, description, require, dist: {url}}...]}`。
- 安装：下载 zip → `ZipArchive::extractTo(plugins 目录)`；**无签名、无校验和**。
- 超级管理员额外可：上传 zip、填 URL 远程下载（wget）。
- 浏览器内 CLI（xterm.js）：`apt/dnf/pacman install|upgrade|remove <plugin>`、`closet add|remove <uid> <tid>`。

### 8.5 官方插件生态（44 个，`blessing-skin-plugins`）

| 分类 | 插件 | 重构时的处置建议（详见 04） |
|---|---|---|
| **游戏协议** | yggdrasil-api、yggdrasil-connect（LittleSkin 重构版，修了 6 个 bug）、usm-api、legacy-api、204-for-unexisted-players、mojang-verification | **内置为核心模块** |
| 存储/CDN | textures-aliyun-oss、textures-qcloud-cos、textures-qiniu-storage、refresh-ali-cdn、refresh-qcloud-cdn、purge-azure-global-cdn、insane-profile-cache、predis | 内置：S3 兼容存储 + CDN 刷新 provider；profile 缓存问题从架构上消除 |
| 登录/账号 | oauth（核心）+ oauth-github/google/microsoft-live/littleskin、fast-login、authme-integration、forum-integration、password-algorithm-transition、disable-registration、invitation-codes、restricted-email-domains、single-player-limit、share-registration-link | 社交登录、密码算法平滑迁移、邀请码、邮箱域名限制、禁止注册 → 内置；论坛/Authme 对接 → 插件 |
| 网络 | remote-addr、trust-proxies | 用"可信代理 CIDR"配置替代 |
| 内容 | texture-description、texture-type-cast、embedded-comment、batch-import | 描述/类型转换 → 内置；评论、批量导入 → 插件或后续版本 |
| 运营/杂项 | config-generator、gravatar、gtag-js、google-adsense、hitokoto、hello-dolly、eruda、hide-advanced-menu、insider、pray-for-kyoto-animation | 插件（示例/社区） |

## 9. 前端结构

- **页面骨架**由 Twig 渲染（`resources/views`），按布局分 `home`、`auth/base`、`user|admin/master`、`skinlib`、`setup`。
- **React 孤岛**：`index.tsx` 读取 `blessing.route`（当前路径），在 `scripts/route.tsx` 里按正则匹配到组件后，`ReactDOM.render` 到页面里某个已有 DOM 节点（如 `#closet-list`、`.content > .container-fluid`）。
- **页面布局可被插件改写**：控制器把 `grid`（行列 + widget 模板名）交给 `filter('grid:user.index')`，插件往里塞自己的 Twig widget。
- **网络层**：`scripts/net.ts` 封装 fetch，自动带 `X-CSRF-TOKEN`，统一处理 422/419/403 与异常 trace 展示；派发 `beforeFetch`/`fetchError` 事件。
- 主要 React 视图：用户（Dashboard、Closet、Players、OAuth、profile）、管理（Users/Players/Reports/Plugins/Market/Translations 管理）、认证（Login/Registration/Forgot/Reset）、皮肤库（SkinLibrary、Show、Upload）。
- 全局变量：`window.blessing`、`window.React`、`window.ReactDOM`、`window.$` —— 插件前端依赖这些全局变量，等于把 React 17 和 jQuery 版本锁成了公共 API。

## 10. 安装、升级与运维

| 场景 | 现状 |
|---|---|
| 首次安装 | 访问 `/setup` 向导：填数据库 → **把表单值用正则写回 `.env`** → 填站点名与超管账号 → 跑迁移 + `passport:keys` → 写 `storage/install.lock`；或 CLI `php artisan bs:install email password nickname` |
| 升级 | 下载新版覆盖文件；之后**第一个 web 请求**发现 `config('app.version') > option('version')` 就在请求内 `Artisan::call('update')`（迁移 + `view:clear` + `Cache::flush()`）。后台"检查更新"可直接下载 zip 解压覆盖到站点根目录（无签名） |
| 配置缓存 | `php artisan options:cache` |
| 密码算法 | `.env PWD_METHOD`（默认 BCRYPT）+ `SALT`；旧站常见 SALTED2MD5 等 |
| Docker | 仓库 Dockerfile：`php:8-apache` 只装 `gd zip`，**但 chkenv 要求 imagick、图片驱动也配成 imagick** → 构建出的镜像启动即报错；且 `key:generate` 在构建期执行，同一镜像的所有实例共享 APP_KEY |
| 横向扩展 | 默认 file 缓存/会话、材质本地盘、插件资源复制到 `public/` → 多实例需要共享存储 + Redis + 插件存储类插件 |

## 11. 外部依赖关系（BS 生态的"协议契约"）

这些是重构后**必须字节级兼容**的外部契约（详表见 02）：

1. **CustomSkinLoader**（客户端 Mod）读 `/{name}.json` 与 `/textures/{hash}`，配置里写的是站点 URL —— 改路径 = 所有玩家要改配置。
2. **authlib-injector**（启动器/服务端）：按 [Yggdrasil 服务端技术规范](https://github.com/yushijinhun/authlib-injector/wiki/Yggdrasil-%E6%9C%8D%E5%8A%A1%E7%AB%AF%E6%8A%80%E6%9C%AF%E8%A7%84%E8%8C%83) 访问 `/api/yggdrasil`；**角色 UUID 一旦变化，服务器上的背包/权限/封禁全部错位**——`uuid` 表必须原样迁移。
3. **OAuth 应用**：第三方站点（论坛、服务器面板）通过 `/oauth/*` + `/api/*` 读取用户与角色。
4. **密码哈希**：旧站数据可能是 MD5/SALTED2MD5/SHA256 等，迁移后必须仍能登录。
5. **材质文件名 = sha256**：CDN 上已缓存的 `/textures/{hash}` URL 应继续有效。
