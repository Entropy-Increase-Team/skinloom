---
title: 站点设置
sidebar_position: 2
---

# 站点设置

站点名称、注册、积分、上传规则等都是**站点设置**：保存在数据库里，改了立即生效，已打开的页面几秒内自动刷新，不需要重启。

## 怎么改

**后台**：管理员进入"后台 → 设置"，按分组修改，每组单独保存。数字有上下限，填错时页面直接提示。

**命令行**（在服务器上，值是 JSON）：

```bash
skinloom settings get                                    # 列出全部（* 表示改过）
skinloom settings set score.initial 500
skinloom settings set site.name '{"zh-CN":"我的皮肤站","en":"My Skins"}'
skinloom settings set site.name null                     # 恢复默认
```

**接口**：`PATCH /api/v1/admin/settings`，用管理员登录状态或 `skinloom admin token` 输出的令牌。

几点说明：

- 密码和密钥类设置**只写不读**：保存后后台只显示"已设置"。
- 多语言的设置（站点名、公告等）是 `{"zh-CN": "…", "en": "…"}` 这样的对象。
- 每次修改都记入审计日志。

## 速查表

### 站点

| 键 | 默认 | 说明 |
|---|---|---|
| `site.name` / `site.description` | 云织 / 开源、自托管的 Minecraft 皮肤站 | 站点名称与简介（多语言） |
| `site.url` | 空 | 站点的公开地址，如 `https://skins.example.com`。**一定要设置**：邮件链接、第三方登录回调、外置登录都依赖它。安装向导会自动填写 |
| `site.announcement` | 空 | 首页与用户中心的公告（多语言，Markdown） |
| `site.timezone` | `Asia/Shanghai` | 时区：决定每日签到的刷新时间和后台统计的日期 |
| `site.logo` / `site.favicon` | 空 | Logo 与浏览器图标的地址，空表示用主题自带的 |
| `site.copyright` | 空 | 页脚文字（多语言，Markdown） |
| `seo.keywords` | 空 | 给搜索引擎的关键词（多语言，逗号分隔） |

### 注册与登录

| 键 | 默认 | 说明 |
|---|---|---|
| `auth.registration` | `open` | `open` 开放注册，`closed` 关闭 |
| `auth.registrations_per_ip` | `3` | 同一地址 24 小时内最多注册几个账号，`0` 不限 |
| `auth.login_captcha_after` | `3` | 登录失败几次后要求验证码，`0` 每次都要 |
| `auth.require_email_verification` | `false` | 开启后，未验证邮箱的账号不能创建角色、上传或修改材质、举报、创建应用与令牌、在启动器登录 |
| `auth.social.providers` | `[]` | 第三方登录，见[第三方登录](./social-login.md) |
| `captcha.provider` | `image` | 验证码：`image`（内置）、`turnstile`、`recaptcha`、`hcaptcha`、`none` |
| `captcha.<提供方>.site_key` / `.secret_key` | 空 | 第三方验证码的密钥 |

详见[注册、登录与邮件](./accounts.md)。

### 邮件

| 键 | 默认 | 说明 |
|---|---|---|
| `mail.driver` | `none` | `none` 不发邮件、`smtp`、`log`（只写进日志，测试用） |
| `mail.from` | 空 | 发件人，如 `云织 <noreply@skins.example.com>` |
| `mail.smtp.host` / `.port` | 空 / `465` | SMTP 服务器 |
| `mail.smtp.username` / `.password` | 空 | SMTP 账号（很多邮箱要用"授权码"） |
| `mail.smtp.encryption` | `tls` | `tls`（465 端口）、`starttls`（587）、`none`（仅限本机） |

### 积分

| 键 | 默认 | 说明 |
|---|---|---|
| `score.initial` | `1000` | 新账号的积分 |
| `score.per_storage` / `score.private_per_storage` | `1` / `10` | 上传公开 / 私有材质每 KB 消耗的积分 |
| `score.per_closet_item` | `0` | 每个衣柜条目消耗的积分（收藏、上传都算） |
| `score.award_per_texture` | `0` | 上传公开材质的奖励 |
| `score.award_per_like` | `0` | 材质被别人收藏时上传者得到的积分 |
| `score.per_player` | `100` | 创建一个角色消耗的积分 |
| `score.return_on_delete` | `true` | 删除角色、移出衣柜、删除材质时退还当时花的积分 |
| `score.sign_min` / `score.sign_max` | `10` / `100` | 签到随机获得的积分 |
| `score.sign_gap_hours` | `24` | 两次签到至少间隔几小时 |
| `score.sign_daily` | `false` | 改为每天签到一次（按 `site.timezone` 的零点） |
| `score.reporter_modification` | `0` | 举报时的积分变化：负数是举报费（成立后退还），正数是奖励（驳回时收回） |
| `score.reporter_reward` | `0` | 举报成立时额外奖励 |

### 材质

| 键 | 默认 | 说明 |
|---|---|---|
| `texture.max_size_kb` | `1024` | 上传文件最大多少 KB（上限 8192） |
| `texture.max_width` | `2048` | 图片最大宽度（像素，上限 2048） |
| `texture.name_regexp` | 空 | 材质名称必须匹配的正则表达式 |
| `texture.allow_download` | `true` | 允许下载原图（`/raw/<编号>`） |
| `texture.private_status` | `403` | 别人访问私有材质时返回 `403` 还是 `404`（不透露存在） |
| `texture.content_policy` | 空 | 上传页显示的内容政策（多语言，Markdown） |

### 角色与游戏接口

| 键 | 默认 | 说明 |
|---|---|---|
| `player.name_rule` | `official` | 角色名规则：`official`（字母、数字、下划线）、`cjk`（再加汉字）、`utf8`（除空格外任意字符）、`custom` |
| `player.name_regexp` | 空 | `custom` 规则的正则表达式 |
| `player.name_length_min` / `_max` | `3` / `16` | 角色名长度（上限 32） |
| `delivery.profile_cache_seconds` | `60` | 游戏客户端和 CDN 缓存角色数据多少秒 |
| `delivery.missing_player_status` | `404` | 角色不存在时 CustomSkinLoader 接口返回 `404` 还是 `204`（让客户端继续试下一个皮肤站） |

### 外置登录

| 键 | 默认 | 说明 |
|---|---|---|
| `yggdrasil.uuid_algorithm` | `v3` | 新角色的 UUID：`v3` 与离线服相同（由名字算出，改名会变），`v4` 随机（改名不变） |
| `yggdrasil.token_valid_seconds` | `259200` | 启动器登录的有效期（3 天） |
| `yggdrasil.token_refresh_seconds` | `604800` | 签发后多久内过期的登录还能自动续期（7 天） |
| `yggdrasil.tokens_per_user` | `10` | 每个账号最多同时登录几个启动器 |
| `yggdrasil.search_profile_max` | `5` | 按名字批量查角色时一次最多几个 |
| `yggdrasil.skin_domains` | 空 | 另外允许游戏加载皮肤的域名（逗号分隔）；站点和 `storage.public_url` 的域名自动包含 |
| `yggdrasil.enable_ali` | `true` | 页面带 `X-Authlib-Injector-API-Location` 头，启动器只填站点地址就能找到接口 |

### 开放平台

| 键 | 默认 | 说明 |
|---|---|---|
| `oauth.access_token_ttl_seconds` | `86400` | 应用的访问令牌有效期（1 天） |
| `oauth.refresh_token_ttl_seconds` | `2592000` | 刷新令牌多久不用就失效（30 天） |

### 外观

| 键 | 默认 | 说明 |
|---|---|---|
| `theme.active` | `default` | 当前主题 |
| `theme.overrides` | `{}` | 颜色与圆角覆盖 |
| `home.blocks` | 空 | 首页区块，空表示用主题自带的首页 |
| `i18n.overrides` | `{}` | 改写的界面文字 |
| `theme.custom_css` / `theme.custom_js` | 空 | 自定义 CSS / JavaScript（各最多 64 KB，只有超级管理员能改） |

见[外观与主题](./appearance.md)。

### CDN 刷新

| 键 | 默认 | 说明 |
|---|---|---|
| `cdn.provider` | `none` | `none`、`cloudflare`、`aliyun`、`tencent`、`webhook` |
| `cdn.base_url` | 空 | CDN 对外的站点地址，空表示用 `site.url` |
| `cdn.cloudflare.*`、`cdn.aliyun.*`、`cdn.tencent.*`、`cdn.webhook.*` | 空 | 各提供方的凭据 |

见[备份、恢复与 CDN](../deployment/backup-and-cdn.md)。
