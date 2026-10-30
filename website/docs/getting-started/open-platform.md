---
title: 开放平台
sidebar_position: 7
---

# 开放平台（OAuth 2.0 与 API）

其它网站和程序（论坛、服务器面板、启动器、机器人）可以让用户"用本站账号登录"，并在用户允许的范围内读写他们的角色、衣柜等数据。云织是一个 OAuth 2.0 授权服务器，资源接口与 Blessing Skin 相同：为 BS 写的应用把地址换成云织即可。

用户在用户中心的 **应用与令牌**（`/user/oauth/manage`）里：

- 注册自己开发的应用；
- 查看、撤销允许过的应用；
- 创建个人访问令牌，给自己的脚本使用。

下面的地址以 `https://skins.example.com` 为例，换成你的 `site.url`。

## 注册应用

在"我的应用"里填写名称和回调地址（每行一个，最多 10 个），选择类型：

- **服务端应用**：有密钥（`client_secret`），密钥只在创建或重置时显示一次。适合有后端的网站。
- **公开客户端**：没有密钥，必须使用 PKCE。适合纯前端网页、桌面和手机应用。

`client_id` 是应用的编号。回调地址必须与登记的**完全一致**（包括路径和查询参数），可以是 `https://`、`http://`（开发时）或应用自己的协议（如 `com.example.app:/callback`）。

## 授权码流程

1. 把用户带到授权页：

   ```text
   https://skins.example.com/oauth/authorize?response_type=code&client_id=1
     &redirect_uri=https%3A%2F%2Fforum.example%2Fcallback
     &scope=User.Read%20Player.Read&state=随机字符串
   ```

   公开客户端还要带 `code_challenge` 和 `code_challenge_method=S256`（PKCE）。用户登录并点"授权"后，浏览器回到 `redirect_uri?code=…&state=…`；拒绝时是 `?error=access_denied&state=…`。用户以前允许过同样（或更多）的权限时不会再问。

2. 应用的服务器用授权码换令牌（10 分钟内，只能用一次）：

   ```bash
   curl -u '1:应用密钥' https://skins.example.com/oauth/token \
     -d grant_type=authorization_code -d code=授权码 \
     -d redirect_uri=https://forum.example/callback
   ```

   公开客户端不带密钥，改为 `-d client_id=1 -d code_verifier=…`。也可以用表单字段 `client_id`、`client_secret` 代替 HTTP Basic，或者发 JSON。

   ```json
   {"token_type": "Bearer", "expires_in": 86400, "access_token": "slo_…", "refresh_token": "slr_…", "scope": "User.Read Player.Read"}
   ```

3. 用访问令牌调用接口：`Authorization: Bearer slo_…`。

4. 访问令牌过期后用刷新令牌换一对新的（旧的随即失效）：

   ```bash
   curl -u '1:应用密钥' https://skins.example.com/oauth/token -d grant_type=refresh_token -d refresh_token=slr_…
   ```

   可以带 `scope` 缩小权限，不能扩大。

退出登录时可以吊销令牌：`POST /oauth/revoke`，带 `token` 和客户端凭据（RFC 7009）。服务器元数据在 `/.well-known/oauth-authorization-server`。

错误是 OAuth 的格式，例如 `{"error": "invalid_grant", "error_description": "…", "message": "…"}`。

## 权限（scope）

| scope | 允许 |
|---|---|
| `User.Read` | 读取账号信息（不请求 scope 时默认就是它） |
| `Notification.Read` | 读取站内通知并标为已读 |
| `Player.Read` / `Player.ReadWrite` | 查看 / 管理角色 |
| `Closet.Read` / `Closet.ReadWrite` | 查看 / 管理衣柜（BS 的旧拼写 `Closet.ReadWrtie` 也接受） |
| `UsersManagement.*`、`PlayersManagement.*`、`ClosetManagement.*`、`ReportsManagement.*`、`Notification.ReadWrite` | 管理类，只有管理员能授予 |

`ReadWrite` 包含 `Read`。普通用户授权时，管理类权限会被去掉。

## 资源接口

与 Blessing Skin 相同，需要对应的 scope：

| 接口 | scope | 返回 |
|---|---|---|
| `GET /api` | — | 站点名、兼容的 BS 版本 |
| `GET /api/user` | `User.Read` | `{uid, email, nickname, locale, score, avatar, permission, last_sign_at, register_at, verified}` |
| `GET /api/user/notifications` | `Notification.Read` | 未读通知 `[{id, title}]` |
| `POST /api/user/notifications/{id}` | `Notification.Read` | 标为已读，返回 `{title, content（HTML）, time}` |
| `GET /api/players` | `Player.Read` | 角色 `[{pid, uid, name, tid_skin, tid_cape, last_modified}]` |
| `POST /api/players`（`name`） | `Player.ReadWrite` | 添加角色 |
| `PUT /api/players/{pid}/name`（`name`） | `Player.ReadWrite` | 改名 |
| `PUT /api/players/{pid}/textures`（`skin`、`cape`：材质编号） | `Player.ReadWrite` | 换上衣柜里的材质 |
| `DELETE /api/players/{pid}/textures`（`type: ["skin", "cape"]`） | `Player.ReadWrite` | 脱下 |
| `DELETE /api/players/{pid}` | `Player.ReadWrite` | 删除角色 |
| `GET /api/closet?category=skin\|cape&q=&page=&perPage=` | `Closet.Read` | 分页的衣柜（每页最多 100） |
| `POST /api/closet`（`tid`、`name`） | `Closet.ReadWrite` | 收藏 |
| `PUT /api/closet/{tid}`（`name`） | `Closet.ReadWrite` | 改名 |
| `DELETE /api/closet/{tid}` | `Closet.ReadWrite` | 移出衣柜 |

- 请求体可以是 JSON 或表单。
- 写操作返回 `{"code": 0, "message": "…", "data": …}`；被拒绝时 `code` 不为 0（HTTP 仍是 200），`message` 说明原因，与 BS 相同。
- 输入不合法返回 422 `{"message", "errors"}`；没有令牌或令牌失效 401；权限不够 403；不存在 404；每个用户每分钟最多 60 次请求，超出 429。
- 接口允许跨域调用（不使用 Cookie）。

### 管理类接口

需要管理员授予的管理类 scope；令牌主人不再是管理员时立即失效。权限规则与后台相同：管理员只能管理普通用户，超级管理员管理所有人，谁都不能改自己的权限（否则 403 `{"code": 1, …}`）。

| 接口 | scope | 说明 |
|---|---|---|
| `GET /api/admin/users?q=&page=` | `UsersManagement.Read` | 用户列表（每页 10 条，多一个 `ip` 字段） |
| `PUT /api/admin/users/{uid}/email`（`email`）、`/nickname`（`nickname`）、`/password`（`password`）、`/score`（`score`） | `UsersManagement.ReadWrite` | 修改；积分设为给定值，差额记入积分流水 |
| `PUT /api/admin/users/{uid}/verification` | `UsersManagement.ReadWrite` | 切换邮箱验证状态 |
| `PUT /api/admin/users/{uid}/permission`（`permission`：-1 封禁、0 普通用户、1 管理员） | `UsersManagement.ReadWrite` | 设为管理员需要超级管理员 |
| `DELETE /api/admin/users/{uid}` | `UsersManagement.ReadWrite` | 删除用户 |
| `GET /api/admin/players?q=&page=` | `PlayersManagement.Read` | 角色列表（每页 10 条） |
| `PUT /api/admin/players/{pid}/name`（`player_name`）、`/owner`（`uid`） | `PlayersManagement.ReadWrite` | 改名、转给另一个用户 |
| `PUT /api/admin/players/{pid}/textures`（`tid`、`type`：skin 或 cape） | `PlayersManagement.ReadWrite` | 换上主人衣柜里的材质，`tid` 为 0 脱下 |
| `DELETE /api/admin/players/{pid}` | `PlayersManagement.ReadWrite` | 删除角色 |
| `GET /api/admin/closet/{uid}` | `ClosetManagement.Read` | 用户的整个衣柜（最多 1000 条） |
| `POST`、`DELETE /api/admin/closet/{uid}`（`tid`） | `ClosetManagement.ReadWrite` | 免费放入、取出；返回 `data.user` 和 `data.texture` |
| `GET /api/admin/reports?q=&page=` | `ReportsManagement.Read` | 举报列表（每页 9 条，`status` 0 待处理、1 成立、2 驳回） |
| `PUT /api/admin/reports/{id}`（`action`：delete、ban、reject） | `ReportsManagement.ReadWrite` | 处理举报 |
| `POST /api/admin/notifications`（`receiver`：all、normal、uid、email；`uid` 或 `email`；`title`、`content`） | `Notification.ReadWrite` | 发送通知，正文是 Markdown |

`q` 只看第一个 `字段:值`：用户列表里匹配邮箱、昵称或 ID；角色列表里 `uid:N` 是某个用户的角色，其它值匹配角色 ID、UUID 或名字开头；举报列表支持 `status:`、`reporter:`、`uploader:`、`tid:`。BS 完整的 search-string 语法（排序、比较、多个条件）不支持。管理类接口的操作与后台页面一样记入审计日志。

## 个人访问令牌

在"个人访问令牌"里填写用途、勾选权限、选择有效期（7–365 天或永不过期），得到一个 `slp_` 开头的令牌，只显示一次。像应用的访问令牌一样使用：

```bash
curl -H 'Authorization: Bearer slp_…' https://skins.example.com/api/players
```

## 安全

- 应用密钥和所有令牌在数据库里只存哈希。
- 修改或重置密码、"退出其它所有设备"、被封禁或注销时，应用的令牌和个人访问令牌全部失效；之后用户再用应用时会直接重新授权，不必再确认。
- 撤销授权或删除应用会立即让它的令牌失效。
- 应用的创建者被封禁或注销后，应用停用。

## 站长：设置

| 键 | 默认 | 说明 |
|---|---|---|
| `oauth.access_token_ttl_seconds` | `86400` | 访问令牌的有效期（1 天）。Blessing Skin 是一年；有应用不会刷新令牌时可以调长 |
| `oauth.refresh_token_ttl_seconds` | `2592000` | 刷新令牌多久不用就失效（30 天），每次刷新重新计算 |

从 Blessing Skin 迁移时（M5 的迁移工具），应用的 `client_id`、名称和回调地址会保留（明文密钥导入时哈希化，原密钥继续可用）；已签发的令牌不迁移，用户需要重新授权一次。
