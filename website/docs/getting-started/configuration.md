---
title: 配置
sidebar_position: 2
---

# 配置

SkinLoom 有两类配置：

| 类别 | 放在哪 | 改了之后 |
|---|---|---|
| **静态配置**：端口、数据库、前端进程、日志 | `data/config.toml` 或环境变量 | 大部分键立即生效（热键），少数需要重启（冷键） |
| **站点设置**：站点名、主题、首页布局、各类规则 | 数据库，通过后台 / API / 命令行修改 | 全部立即生效，已打开的页面几秒内自动刷新 |

## config.toml

首次启动时自动生成，每个键都有注释标明 `[hot]` 或 `[cold]`：

```toml
[server]
listen = ":17546"         # [cold] 唯一需要开放的端口
role = "all"              # [cold] all | api
trusted_proxies = []      # [hot]  允许设置 X-Forwarded-* 的代理（IP 或网段），例如 ["127.0.0.1", "10.0.0.0/8"]

[database]
driver = "sqlite"         # [cold] sqlite | mysql | postgres
dsn = "skinloom.db"       # [cold]

[storage]
driver = "local"          # [cold] local | s3（S3 兼容的对象存储）
path = "textures"         # [cold] local：材质文件目录，相对数据目录

[web]
mode = "managed"          # [hot]  managed | dev | external | off
port = 17547              # [hot]  前端进程的内部端口；被占用或设为 0 时自动选择空闲端口
url = ""                  # [hot]  mode = external 时的前端地址
node = ""                 # [hot]  指定 node 可执行文件
package_manager = "auto"  # [hot]  仅 dev 模式：auto（依次找 bun、pnpm、npm）| bun | pnpm | npm

[log]
level = "info"            # [hot]  debug | info | warn | error
format = "text"           # [hot]  text | json
```

- 编辑器用"原子保存"（先写临时文件再替换）也没关系，监听的是所在目录。
- 文件写错时，SkinLoom 会保留正在运行的配置并在日志里报错，不会把站点弄挂。
- 改了冷键会在日志中提示"需要重启才能生效"。
- 拼错的键（例如 `lisen`、`[databse]`）会在启动和热加载时告警，不会被悄悄忽略。

### 对象存储（S3 兼容）

材质文件可以放在任何 S3 兼容的对象存储里：Amazon S3、阿里云 OSS、腾讯云 COS、七牛 Kodo、Cloudflare R2、MinIO 等。

```toml
[storage]
driver = "s3"
endpoint = "oss-cn-hangzhou.aliyuncs.com"   # 主机名（和端口），不带 https://
bucket = "my-skins"
region = "cn-hangzhou"                       # 部分服务需要；不填由客户端自动探测
access_key = "LTAI..."
prefix = "textures/"                         # 桶内的目录，可以留空
path_style = false                           # MinIO 和多数自建服务设为 true
insecure = false                             # 同一台机器上的 MinIO 用纯 HTTP 时设为 true
public_url = "https://cdn.example.com"       # 可选：桶的公开地址或 CDN，材质文件直接跳转过去
```

- 密钥不要写进 `config.toml`，用环境变量 `SKINLOOM_STORAGE__SECRET_KEY` 传入。
- 启动时会检查存储桶是否存在，填错的地址或密钥在启动时就会报错，不会等到第一次上传。
- 设置了 `public_url` 时，`/textures/<哈希>` 会跳转到 `public_url/前缀/哈希`，文件直接由 CDN 发给客户端，不经过 SkinLoom；没有设置时由 SkinLoom 转发（支持断点续传）。CDN 要能读到桶里的对象（公共读，或用回源鉴权）。
- 从本地存储迁移到对象存储：把数据目录下 `textures/` 里的文件原样上传到桶的 `prefix` 下（文件名就是哈希），再改配置重启。
- 头像和预览图在内存里缓存渲染结果，不写进存储。

### 数据库连接串

| 驱动 | `dsn` 示例 |
|---|---|
| sqlite | `skinloom.db`（相对 `data/`） |
| mysql / mariadb | `user:pass@tcp(127.0.0.1:3306)/skinloom` |
| postgres | `postgres://user:pass@127.0.0.1:5432/skinloom?sslmode=disable` |

### 环境变量

任意键都可以用环境变量覆盖，格式 `SKINLOOM_<分组>__<键>`（双下划线分隔层级）：

```bash
SKINLOOM_DATABASE__DRIVER=postgres
SKINLOOM_DATABASE__DSN=postgres://...
SKINLOOM_SERVER__LISTEN=:9000
```

另有 `SKINLOOM_DATA`（数据目录，默认 `./data`）与 `SKINLOOM_CONFIG`（配置文件路径）。

### web.mode

| 值 | 用途 |
|---|---|
| `managed` | 发行包默认：由 SkinLoom 启动并监管 `web/` 里的前端 |
| `dev` | 源码开发：用 `web.package_manager` 选出的包管理器（默认 bun，没有则 pnpm、npm）在 `./web` 运行前端开发服务器 |
| `external` | 前端单独部署（容器、k8s），SkinLoom 把页面请求转发到 `url` |
| `off` | 只提供 API 与游戏接口 |

## 站点设置

```bash
skinloom settings get                                   # 列出全部设置（* 表示改过）
skinloom settings set site.name '{"zh-CN":"我的皮肤站","en":"My Skins"}'
skinloom settings set theme.overrides '{"light":{"primary":"oklch(0.6 0.2 300)"}}'
skinloom settings set site.name null                    # 恢复默认
```

运行中的站点会在 2 秒内感知命令行或其它节点做的修改。也可以调用 `PATCH /api/v1/admin/settings`：浏览器里以管理员身份登录即可（写请求需带 `X-SkinLoom-Request: 1` 头，站点前端会自动加上）；脚本使用 `skinloom admin token` 输出的令牌（`Authorization: Bearer …`）。每次修改都会记录修改人和审计日志。

目前可用的设置（全部即时生效）：

| 键 | 默认 | 说明 |
|---|---|---|
| `site.name` / `site.description` / `site.announcement` | — | 站点名称、简介、公告（多语言对象） |
| `site.url` | 空 | 站点公开地址，如 `https://skins.example.com`。邮件里的链接（验证邮箱、重置密码）只用它生成，没有设置时这两项功能不可用 |
| `site.timezone` | `Asia/Shanghai` | 站点时区（IANA 名称，如 `Europe/Berlin`），决定每日签到在哪个零点刷新 |
| `theme.active` / `theme.overrides` / `home.blocks` | `default` | 当前主题、主题 token 覆盖、首页区块 |
| `auth.registration` | `open` | `open` 开放注册；`closed` 关闭注册 |
| `auth.registrations_per_ip` | `3` | 同一地址（IPv6 按 /64）24 小时内最多注册几个账号，`0` 不限 |
| `auth.login_captcha_after` | `3` | 同一客户端对同一账号登录失败几次后要求验证码，`0` 每次都要 |
| `score.initial` | `1000` | 新账号的初始积分 |
| `score.per_storage` / `score.private_per_storage` | `1` / `10` | 上传公开 / 私有材质每 KB 消耗的积分 |
| `score.per_closet_item` | `0` | 每个衣柜条目消耗的积分（上传的材质也会进衣柜） |
| `score.award_per_texture` | `0` | 上传公开材质奖励的积分 |
| `score.award_per_like` | `0` | 材质被别人收藏时上传者获得的积分（取消收藏时收回） |
| `score.reporter_modification` | `0` | 提交举报时的积分变化：负数为举报费（举报成立后退还），正数为奖励（被驳回时收回） |
| `score.reporter_reward` | `0` | 举报成立（材质被删除或上传者被封禁）时举报者获得的积分 |
| `score.sign_min` / `score.sign_max` | `10` / `100` | 签到随机获得的积分范围 |
| `score.sign_gap_hours` | `24` | 两次签到至少间隔几小时（1–8760） |
| `score.sign_daily` | `false` | 开启后改为每天签到一次，按 `site.timezone` 的零点刷新（忽略间隔） |
| `texture.max_size_kb` | `1024` | 上传文件的最大大小（KB，上限 8192） |
| `texture.max_width` | `2048` | 材质的最大宽度（像素，上限 2048） |
| `texture.name_regexp` | 空 | 材质名称必须匹配的正则表达式（Go RE2 语法），空表示不限 |
| `texture.allow_download` | `true` | 是否允许通过 `/raw/{tid}` 下载原图 |
| `texture.private_status` | `403` | 别人访问私有材质时返回的状态码：`403`，或 `404`（不透露材质存在） |
| `texture.content_policy` | — | 上传页显示的内容政策（多语言，Markdown） |
| `score.per_player` | `100` | 创建一个角色消耗的积分 |
| `score.return_on_delete` | `true` | 删除角色、移出衣柜、删除材质时退还当时支付的积分 |
| `player.name_rule` | `official` | 角色名规则：`official`（英文字母、数字、下划线）/ `cjk`（再加汉字）/ `utf8`（不含空格的任意字符）/ `custom` |
| `player.name_regexp` | 空 | `custom` 规则使用的正则表达式（Go RE2 语法） |
| `player.name_length_min` / `player.name_length_max` | `3` / `16` | 角色名长度（字符，上限 32） |
| `yggdrasil.uuid_algorithm` | `v3` | 新角色的 UUID：`v3` 与离线服相同（由名字计算，改名会变）；`v4` 随机（改名不变） |
| `delivery.profile_cache_seconds` | `60` | 游戏客户端与 CDN 可以缓存角色数据（`/{角色名}.json` 等）多少秒 |
| `delivery.missing_player_status` | `404` | 不存在的角色在 CustomSkinLoader 接口返回的状态码：`404` 或 `204` |
| `captcha.provider` | `image` | `none` / `image`（内置图形验证码）/ `turnstile` / `recaptcha` / `hcaptcha` |
| `captcha.<turnstile\|recaptcha\|hcaptcha>.site_key` / `.secret_key` | 空 | 第三方验证码的站点密钥（公开）与服务端密钥（不会发给浏览器） |
| `mail.driver` | `none` | `none` 不发邮件（无法验证邮箱、找回密码）/ `smtp` / `log`（写进服务端日志，开发用） |
| `mail.from` | 空 | 发件人，如 `云织 <noreply@skins.example.com>` |
| `mail.smtp.host` / `.port` / `.username` / `.password` | 空 / `465` | SMTP 服务器与账号（很多邮箱要用"授权码"而不是登录密码） |
| `mail.smtp.encryption` | `tls` | `tls`（465 端口）/ `starttls`（587）/ `none`（仅限本机中继） |

账号、验证码与邮件的详细说明见 [账号与用户管理](accounts.md)。
