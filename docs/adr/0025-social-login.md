# 0025 社交登录（GitHub、Microsoft、OpenID Connect）

- 状态：已接受（2026-10-30）
- 相关：设计文档 02（F07、F08）；04 §13 M3；ADR 0015、0023

## 背景
M3 第四部分，也是 M3 的最后一块。BS 的社交登录是 `oauth` 核心插件加各家的插件（GitHub、Google、Microsoft Live、LittleSkin），基于 Laravel Socialite，问题：
- **只按邮箱认人**：回调拿到邮箱就找同邮箱的账号直接登录，不看提供方是否验证过这个邮箱。Microsoft 工作账号的邮箱由组织管理员随便填（"nOAuth"），谁控制一个这样的组织就能登录任何人的账号；先用别人的邮箱注册一个账号再等对方用 GitHub 登录，也能共用账号（预先劫持）。
- 没有"绑定"：已有账号的人不能把 GitHub 关联上来，也看不到、解除不了。
- 回调没有 state 校验之外的保护，新建的账号密码为空字符串，配置写在 `.env` 里，每加一家就要装一个插件。

## 决策
- **一套通用客户端，三种类型**（`internal/social`）：`github`（OAuth 2.0 + REST API）、`microsoft`（Microsoft 身份平台 v2.0，OpenID Connect）、`oidc`（任何 OpenID Connect 提供方，按 issuer 自动发现，Google、Keycloak、Authentik 等）。不引入 Socialite 式的库：三种协议各几十行，依赖反而更多。
- **配置**：一个设置 `auth.social.providers`，是 `{id, type, clientId, clientSecret, name?, issuer?, tenant?, scopes?}` 的列表，保存时校验；密钥只在服务端。`id` 出现在回调地址 `<site.url>/api/v1/auth/social/<id>/callback` 里，绑定也记在它上面，所以选定后不要改。没有设置 `site.url` 时不提供任何登录方式：回调地址只用它生成，绝不取请求的 Host（同 ADR 0015）。站点 bootstrap 只公开 `id`、名称和类型。
- **流程**：
  - 登录页、注册页显示"使用 … 登录"按钮。`POST /api/v1/auth/social/{id}/start` 在数据库里记一条进行中的登录（`social_flows`，10 分钟），把随机的 state 写进只发往 `/api/v1/auth/social` 的 HttpOnly cookie，返回提供方的登录地址。始终带 PKCE（S256），OpenID Connect 还带 nonce。
  - 回调要求 state 与 cookie 一致（另一个网站不能把访客带到回调、登进攻击者的账号），记录只能用一次。
  - 已绑定：直接登录（封禁的账号不行；宽限期内注销的账号像密码登录一样恢复）。
  - 未绑定：如果提供方验证过邮箱、本站有同一邮箱且**也已验证**的账号，视为同一个人，自动绑定并登录（BS 用户迁移过来时只有邮箱这条线索）。任何一边没验证都不自动绑定。
  - 其余情况进入 `/auth/social`（15 分钟内）。账号要填了邮箱才创建，所以没有 BS 那种空邮箱的账号，也就不需要"先绑定邮箱"的拦截页（F07）：创建新账号（邮箱和昵称预填，可改），或"登录并绑定"已有账号（用密码登录后绑定，走登录页原有的验证码与限流）。已登录的人在这里直接绑定到当前账号。
- **认什么邮箱是验证过的**：GitHub 取 `/user/emails` 里主邮箱的 `verified`；OpenID Connect 取 `email_verified`（也接受字符串 `"true"`）；Microsoft 只认个人账号（租户 `9188040d-…`）或带 `xms_edov` 的邮箱，工作账号的 `email` 不算（防 nOAuth）。新账号保留提供方验证过的邮箱就直接视为已验证，换了邮箱则照常发验证邮件。
- **ID Token**：校验 `iss`、`aud`（多个时还要 `azp`）、`exp`、`nonce`、`sub`，不校验签名：它是本站直接从提供方的令牌端点经 TLS 取得的，OpenID Connect Core §3.1.3.7 第 6 条允许以 TLS 代替签名。ID Token 没有邮箱时再查 userinfo（`sub` 必须一致）。提供方地址只允许 https（本机的 http 用于测试）。Microsoft 按 `tenant`（`common`、`consumers`、`organizations` 或租户 ID）限制谁能登录。
- **没有密码的账号**：通过提供方创建的账号没有密码（`hasPassword: false`），不能用密码或启动器（Yggdrasil）登录，直到在个人资料页设置一个；设置第一个密码、改邮箱、注销账号都不需要当前密码。没有密码时，最后一个仍在配置里的提供方的绑定不能解除（409），以免账号再也登不进去；已从配置中删掉的提供方的绑定不算登录方式，随时可以解除。检查时先锁住用户行，两个同时的解绑不会把绑定全部删掉。
- **绑定管理**：个人资料页"已绑定的账号"列出绑定（提供方、对方账号、绑定时间），可以绑定其它提供方、解除绑定。每个提供方一个账号只能绑定一个本站账号，一个本站账号在每个提供方也只绑一个。绑定、解绑写审计日志，登录与注册的审计记录带 `method`。
- **接口**：`POST /api/v1/auth/social/{id}/start`、`GET /api/v1/auth/social/{id}/callback`、`GET /api/v1/auth/social/pending`、`POST /api/v1/auth/social/pending/register`、`POST /api/v1/auth/social/pending/link`、`GET /api/v1/me/social`、`POST /api/v1/me/social/{id}`、`DELETE /api/v1/me/social/{id}`。`/api/v1/auth/social/` 下的写请求总是需要 `X-SkinLoom-Request` 头。开始登录每个地址 10 分钟最多 30 次；新账号同样受 `auth.registration` 和 `auth.registrations_per_ip` 限制，提供方代替了验证码。

## 备选方案
- 照搬 BS 只按邮箱认人：迁移最省事，但就是上面的账号接管问题。
- 验证 ID Token 签名（JWKS）：要引入 JOSE 库、处理密钥轮换；令牌来自直连的令牌端点时规范不要求。将来做 Yggdrasil Connect（F58）需要校验第三方转交的令牌时再加。
- 每家一个设置项（`social.github.client_id`…）：命令行好改，但只能配一个 OpenID Connect 提供方。
- 第一次登录直接建账号（BS 的做法）：少点一下，但用户没有机会改昵称、也不知道可以绑定已有账号。

## 影响
- 数据库新增 `social_accounts`（`(provider, subject)` 与 `(user_id, provider)` 唯一）、`social_flows` 表（迁移 `20261030040409_social`）。未完成的登录每小时清理。
- 新设置 `auth.social.providers`；`UserView` 增加 `hasPassword`，站点 bootstrap 增加 `auth.social`。
- 后台还没有设置页面，提供方用 `skinloom settings set` 或设置接口配置；后台的用户详情暂不显示绑定。
- Google、LittleSkin 等不再需要单独的插件：Google 用 `oidc` 类型；不支持 OpenID Connect 的 BS 站点（LittleSkin）以后可以加一个 `blessing-skin` 类型（用对方的 `/api/user`）。
- 至此 M3（外置登录与开放平台）的功能全部完成。
