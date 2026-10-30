# 0023 OAuth 2.0 提供方、个人访问令牌与 Blessing Skin 兼容资源 API

- 状态：已接受（2026-10-30）
- 相关：设计文档 02 §1.2（F18）、§1.5（F60、F61）、§2.6、§3；03（S02、C09、C13、C15、§7.4–7.5）；04 §3（D4）、§4；ADR 0022

## 背景
M3 第二部分。Blessing Skin 用 Laravel Passport 做 OAuth 2.0 提供方：用户在 `/user/oauth/manage` 自建应用，第三方网站（论坛、服务器面板）用授权码流程拿到令牌，调用 `/api/user`、`/api/players`、`/api/closet` 等资源接口。问题：
- 应用密钥明文存储并一直显示在页面上；访问令牌默认一年有效。
- 衣柜写权限的 scope 注册成了拼错的 `Closet.ReadWrtie`，路由却要求 `Closet.ReadWrite`，第三方永远拿不到衣柜写权限（C13）。
- 改密码、找回密码不吊销 OAuth 令牌（S02）；删除用户不删令牌（C09）。
- 读一条不存在的通知直接 500（C15）；衣柜分页的 `perPage` 没有上限。
- 没有个人访问令牌：脚本只能自己实现一遍授权流程。

## 决策
- **自己实现授权服务器**（`internal/oauth`），不用设计文档 04 选的 zitadel/oidc：
  - 需要的只是授权码 + PKCE、刷新令牌、吊销和元数据，几百行，状态全在我们自己的表里，与会话一起在事务里吊销。
  - zitadel/oidc 以 OIDC 为中心，要实现一整套 Storage 接口，还会带进 go-jose、securecookie、otel 等依赖。
  - 以后做 Yggdrasil Connect（F58，OIDC）时再评估：在这套表上加 ID Token，或者那时再引入库。
- **数据**（迁移 `20261029155749_oauth`）：
  - `oauth_clients`：数字 id 就是 `client_id`（与 Passport 一致，迁移时保留），名称、回调地址列表、密钥哈希（公开客户端为空）。
  - `oauth_grants`：用户允许过某个应用的 scope；再次请求不超出这些 scope 时直接通过，不再询问（同 Passport）。
  - `oauth_codes`：授权码（哈希、10 分钟、一次性）与 PKCE 质询。
  - `oauth_tokens`：应用的访问令牌和刷新令牌，以及个人访问令牌（`kind` 区分），只存 SHA-256。
  - 令牌带前缀：`slo_`（访问）、`slr_`（刷新）、`slp_`（个人访问令牌），一眼能分辨，也方便密钥扫描工具识别泄露。
- **授权流程**：
  - `GET /oauth/authorize` 302 到前端页面 `/user/oauth/authorize`（同样的查询参数），未登录先去登录再回来。页面调 `GET /api/v1/oauth/authorize` 检查请求，用户点"授权"或"拒绝"时调 `POST /api/v1/oauth/authorize`，拿到要跳转的地址。
  - `client_id` 不存在、回调地址没有登记（必须完全一致；只登记了一个时可以省略）时，错误显示在页面上，不跳转，免得伪造的请求把用户带到任意地址。其它错误（`response_type` 不是 `code`、未知 scope、PKCE 参数不对）按 RFC 6749 跳回应用，带上 `error` 和 `state`。
  - 公开客户端（没有密钥：浏览器、桌面、手机应用）必须用 PKCE；服务端应用可选（Passport 兼容）。支持 `S256` 和 `plain`。
  - 普通用户的授权里会去掉管理类 scope；全部被去掉时返回 `invalid_scope`。
  - 应用主人被封禁或注销后，应用不再能用，它已经签发给其他用户的令牌也立即失效（M3 收尾时补上）。
  - 授权写审计 `oauth.authorize`。
- **令牌端点** `POST /oauth/token`：
  - 接受表单（RFC 6749）或 JSON（Laravel 也接受）；客户端凭据放 HTTP Basic 或表单都行。
  - 支持 `authorization_code` 和 `refresh_token`。不支持 `password` 和 `client_credentials`：BS 的资源接口都需要具体的用户，这两种拿到的令牌在 BS 上本来也用不了。
  - 授权码只能用一次：在事务里删除授权码，删到的那个请求才拿到令牌，同时兑换只有一个成功。授权码没通过检查（客户端不对、回调地址不对、PKCE 校验失败）也会作废，防止被偷的授权码被反复尝试。
  - 刷新令牌每次使用都换新的，旧的一对立即失效；刷新时可以缩小 scope，不能扩大。
  - 错误是 RFC 6749 §5.2 的格式，另加 Laravel 的 `message` 字段（Passport 客户端读这个）。
  - 访问令牌默认 1 天（`oauth.access_token_ttl_seconds`），刷新令牌 30 天未用即失效（`oauth.refresh_token_ttl_seconds`）。BS 默认一年；需要的站点可以把访问令牌调到最长一年。
- **其它端点**：`POST /oauth/revoke`（RFC 7009）、`GET /.well-known/oauth-authorization-server`（RFC 8414 元数据）。`/oauth/token`、`/oauth/revoke` 和资源 API 允许任意来源的 CORS：它们不读 Cookie，浏览器应用（PKCE）可以直接调用。跨域预检只回应这些路径：`/api/v1` 的 CSRF 防护靠的是别的网站永远得不到预检许可（M3 收尾时修正：原来 `/api/*` 的通配预检也会回应 `/api/v1`）。
- **Scope** 沿用 BS 的名字。`X.ReadWrite` 包含 `X.Read`；`Closet.ReadWrtie` 当作 `Closet.ReadWrite`（修 C13）。管理类 scope（`*Management.*`、`Notification.ReadWrite`）只有管理员能授予，使用时还要求令牌主人仍是管理员。没有请求 scope 时默认 `User.Read`（同 BS）。
- **个人访问令牌**：用户在"应用与令牌"页面创建，选 scope 和有效期（7、30、90、365 天或永不过期），令牌只显示一次；列表显示最近使用时间（最多每分钟写一次）。每人最多 50 个，每人最多 25 个应用。创建写审计 `token.create`。
- **吊销**：改密码、找回密码、封禁、注销、"退出其它所有设备"时，应用的令牌、未兑换的授权码和个人访问令牌与网页会话、启动器令牌一起失效（S02）。授权记录保留，用户下次使用应用时不必重新确认。用户在"已授权的应用"里撤销某个应用，就删除授权记录、令牌和授权码。删除应用会删掉与它有关的一切。
- **兼容 BS 的资源 API**（`internal/httpapi/bsapi.go`，L-B）：
  - 本部分：`/api`（根）、`GET /api/user`、`GET /api/user/notifications`、`POST /api/user/notifications/{id}`、`GET|POST /api/players`、`DELETE /api/players/{pid}`、`PUT /api/players/{pid}/name`、`PUT|DELETE /api/players/{pid}/textures`、`GET|POST /api/closet`、`PUT|DELETE /api/closet/{tid}`。管理类 `/api/admin/*` 放到 M3 第三部分。
  - 响应保持 BS 的形状：模型对象用 snake_case 字段（`uid`、`pid`、`tid_skin`、`last_modified`……）；操作结果是 `{"code", "message", "data"}`，失败时 code 不为 0 但 HTTP 仍是 200（BS 的客户端靠 code 判断）；输入错误是 Laravel 的 422 `{"message", "errors"}`；401、403、404、429 是 `{"message"}`。通知正文是渲染好的 HTML。
  - 修正：读不存在或别人的通知 404（BS 500，C15）；衣柜每页最多 100 条；改别人的角色 403、不存在 404。
  - 根 `/api` 的 `blessing_skin` 字段写 `6.0.2`，表示与这个版本的 API 兼容，另有 `skinloom` 字段是真实版本。
  - 限流同 BS：每个用户每分钟 60 次。
  - 这组接口直接挂在 chi 上，不进 OpenAPI，理由同 Yggdrasil（ADR 0022）。
- **前端**：`/user/oauth/authorize`（授权确认页）、`/user/oauth/manage`（BS 的同名页面，现在包含"我的应用""已授权的应用""个人访问令牌"三部分），用户中心增加入口。

## 备选方案
- zitadel/oidc 或 ory/fosite：见上，暂不需要 OIDC，依赖与适配成本都偏高。
- 授权码失败不作废：RFC 只要求成功使用后作废；作废失败的授权码让偷到授权码的人只能试一次。
- 资源 API 失败时返回 4xx：更规范，但 BS 的客户端会把 4xx 当异常，读不到 `code` 和 `message`。
- 个人访问令牌也用于 `/api/v1`：需要给每个操作定义 scope，等 API 稳定后再做；脚本目前用兼容接口和 `skinloom admin token`。

## 影响
- 迁移 `20261029155749_oauth`：`oauth_clients`、`oauth_grants`、`oauth_codes`、`oauth_tokens` 四张新表（只建表）。
- 新设置 `oauth.access_token_ttl_seconds`、`oauth.refresh_token_ttl_seconds`。
- 新包 `internal/oauth`；`texture.ClosetPage`；`account` 吊销会话时一并吊销 OAuth 与个人访问令牌（`revokeTokens`）。
- `csrfGuard` 只检查 `/api/v1/`：其它 `/api/*`（Yggdrasil、兼容资源 API）和 `/oauth/*` 只认令牌。
- 从 BS 迁移（M5 迁移工具）：应用的 `client_id`、名称、回调地址原样导入，明文密钥导入时哈希；已签发的令牌不迁移，用户重新授权。
