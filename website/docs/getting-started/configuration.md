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
driver = "local"          # [cold] local（S3 兼容存储将在 M2 后续部分支持）
path = "textures"         # [cold] 材质文件目录，相对数据目录

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
