---
title: 开放平台与接口
sidebar_position: 3
---

# 开放平台与接口

SkinLoom 对外提供四组接口：

| 接口 | 给谁用 | 文档 |
|---|---|---|
| REST API `/api/v1` | 站点前端、脚本 | 运行中的站点访问 `/api/docs`（OpenAPI 3.1，源文件 `api/openapi.json`） |
| OAuth 2.0 与资源接口 | 第三方网站、程序（"用本站账号登录"） | 本页 |
| 游戏接口 | 启动器、游戏客户端、服务器 | 本页[游戏接口](#游戏接口) |
| Blessing Skin 兼容接口 | 为 BS 写的应用 | 本页，与 BS 相同 |

下面的地址以 `https://skins.example.com` 为例。

## 注册应用

用户中心 →"应用与令牌"→"我的应用"，填名称和回调地址（每行一个，最多 10 个），选择类型：

- **服务端应用**：有密钥（`client_secret`），只在创建或重置时显示一次。适合有后端的网站。
- **公开客户端**：没有密钥，必须用 PKCE。适合纯前端网页、桌面和手机应用。

`client_id` 是应用的编号。回调地址必须与登记的**完全一致**。

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

用户中心 →"应用与令牌"→"个人访问令牌"，填用途、勾选权限、选择有效期，得到 `slp_` 开头的令牌（只显示一次），用法与访问令牌相同：

```bash
curl -H 'Authorization: Bearer slp_…' https://skins.example.com/api/players
```

用户改密码、"退出其它所有设备"、被封禁或注销时，所有应用令牌和个人访问令牌都会失效。令牌在数据库里只存哈希。

## 游戏接口

### 皮肤加载

| 地址 | 返回 |
|---|---|
| `/{角色名}.json`、`/csl/{角色名}.json` | CustomSkinAPI：`{"username", "skins": {"default" 或 "slim": "<哈希>"}, "cape"}` |
| `/usm/{角色名}.json` | UniSkinMod 格式 |
| `/skin/{角色名}.png`、`/cape/{角色名}.png` | 图片 |
| `/textures/{哈希}` | 材质文件，内容永不改变，可以永久缓存 |

角色名不区分大小写。角色不存在返回 404（站长可设为 204）；主人被封禁返回 403。响应可以缓存 `delivery.profile_cache_seconds` 秒，并支持 `If-Modified-Since`。

### Yggdrasil（authlib-injector）

| 地址 | 用途 |
|---|---|
| `GET /api/yggdrasil/` | 元数据：站点名、签名公钥、`skinDomains` |
| `POST /api/yggdrasil/authserver/{authenticate,refresh,validate,invalidate,signout}` | 启动器登录、刷新、校验、退出 |
| `POST /api/yggdrasil/sessionserver/session/minecraft/join` | 客户端进服 |
| `GET /api/yggdrasil/sessionserver/session/minecraft/hasJoined` | 服务器校验进服的玩家 |
| `GET /api/yggdrasil/sessionserver/session/minecraft/profile/{uuid}` | 按 UUID 查角色，`?unsigned=false` 带签名 |
| `POST /api/yggdrasil/api/profiles/minecraft` | 按名字批量查角色 |
| `GET /api/yggdrasil/api/users/profiles/minecraft/{name}` | 按名字查角色 |
| `PUT`、`DELETE /api/yggdrasil/api/user/profile/{uuid}/{skin\|cape}` | 上传 / 清除材质（`Authorization: Bearer <令牌>`） |
| `GET /api/yggdrasil/minecraftservices/publickeys` | 签名公钥 |

错误格式：`{"error": "ForbiddenOperationException", "errorMessage": "Invalid token."}`。

### 头像与预览图

| 地址 | 说明 |
|---|---|
| `/avatar/player/{角色名}`、`/avatar/user/{用户 ID}`、`/avatar/{材质编号}`、`/avatar/hash/{哈希}` | 头像。`size`（16–512，取最接近的一档），`3d` 立体 |
| `/preview/{材质编号}`、`/preview/hash/{哈希}` | 预览图。`height`（64–512） |

默认输出 WebP；加 `png`、客户端不接受 WebP 或图片大于 300×300 时输出 PNG。
