# 0022 Yggdrasil 外置登录（authlib-injector）

- 状态：已接受（2026-10-29）
- 相关：设计文档 02 §1.2（F19 启动器引导）、§1.5（F57）、§2.4；03（S02、Y01–Y09）；04 §13（M3）；ADR 0014、0018

## 背景
M3 第一部分：内置 Blessing Skin 的 yggdrasil-api 插件所提供的外置登录，让 HMCL、PCL2、BakaXL 等启动器通过 authlib-injector 用站点账号进服，并保持与插件相同的线上协议，迁移过来的启动器与服务器不需要改配置。插件的问题（03 §6）：
- 令牌只放在缓存里，清缓存（BS 升级时会 `Cache::flush()`）就全站掉线（Y01）；用户的令牌列表是缓存里的数组，并发登录会丢令牌、永不过期（Y08）。
- 缓存键有时用原始邮箱、有时用小写：邮箱含大写字母时刷新出的令牌无法校验（#212），用角色名或大写邮箱 signout 什么都不吊销（Y02）。
- 用角色名登录不绑定该角色（#123）；改名后旧令牌仍按旧名工作（#231）；profile 返回的名字大小写与库内不符（#232）。
- 登录节流按请求体里的 `username` 计：换个用户名就绕过，也能故意刷某个用户名把真用户锁住（Y04）。
- `hasJoined`：join 之后角色被删 → 500（Y05）；批量查角色名每个名字一次查询；每次序列化 profile 都探测表是否存在、重新解析私钥（Y06）。
- 材质上传接口接受任何有效令牌，可以给别人的角色换皮肤；JWT 的 `exp` 从不检查，只看缓存里的记录。
- 启用时由 Web 进程往 `.env` 追加 `JWT_SECRET`（Y09）。

## 决策
- **接口**（`internal/yggdrasil`，HTTP 在 `internal/httpapi/yggdrasil.go`，前缀 `/api/yggdrasil`，路径与响应同 02 §2.4）：
  - 元数据 `GET /`：`serverName`（站点名）、`implementationName` = SkinLoom、版本、`links.homepage` / `register`、`feature.non_email_login`；`skinDomains` 是站点域名、`storage.public_url` 的域名和 `yggdrasil.skin_domains`；`signaturePublickey`。
  - `authserver/authenticate | refresh | validate | invalidate | signout`、`sessionserver/session/minecraft/join | hasJoined | profile/{uuid}`、`api/profiles/minecraft`、`api/users/profiles/minecraft/{name}`、`PUT/DELETE api/user/profile/{uuid}/{skin|cape}`、`minecraftservices/publickeys`。
  - 错误一律是 Yggdrasil 格式 `{"error", "errorMessage"}`，文案用 authlib-injector 文档里启动器认得的那几句（`Invalid token.`、`Invalid credentials. Invalid username or password.`）；请求体最多 64 KB，响应 `Cache-Control: no-store`。
  - 这组接口用 chi 直接挂载，不进 Huma 与 OpenAPI：协议是外部定好的，错误格式与 Huma 的 Problem Details 不同，也不需要生成前端客户端。它不读 Cookie，所以不需要 CSRF 头（`csrfGuard` 放行该前缀）。
- **令牌落库存哈希**（修 Y01、Y08、Y02）：
  - `ygg_tokens`：`token_hash`（SHA-256，唯一）、`client_token`、`user_id`、`player_id`（绑定的角色，可空）、`expires_at`、`refresh_until`。按 `user_id` 归属，与邮箱写法无关，#212 与 signout 的问题随之消失。
  - 访问令牌仍是 BS 格式的 HS256 JWT（`iss`、`sub` = 用户 UUID、`yggt`、`spr` = 绑定角色、`iat`、`exp`），启动器当它是不透明字符串。是否有效只看数据库里那一行：过期时间、`clientToken`、账号状态（封禁返回 "Your account has been banned."，已注销视为无效）。
  - 有效期 `yggdrasil.token_valid_seconds`（默认 3 天），过期后 `yggdrasil.token_refresh_seconds`（默认 7 天，从签发算）内还能 refresh；refresh 删除旧令牌、签发新令牌，旧的立即失效。每人最多 `yggdrasil.tokens_per_user` 个（默认 10），超出时在同一个事务里删掉最旧的。
  - 每小时清理过了 refresh 期限的令牌和过期的 join 记录。
  - 设计文档的"缓存前置"没有做：按唯一的哈希查一行足够快，缓存反而要处理多实例下的吊销。
- **角色绑定**：
  - 用角色名登录绑定那个角色（修 #123）；用邮箱登录、只有一个角色时绑定它；否则不绑定，由启动器 refresh 时选择（只能选自己的角色，已绑定的令牌不能再选）。
  - 绑定存的是角色 ID：改名不影响令牌（#231），返回的名字永远是库内的写法（#232）。角色被删，绑定它的令牌一并删除；管理员把角色转给别人后，旧主人的令牌不再能用它进服或换皮肤（refresh 后变为未绑定）。
- **join / hasJoined**：
  - `ygg_sessions`：`server_id`（唯一）、`player_id`、`ip`、`expires_at`。join 只接受令牌绑定的那个角色，30 秒内有效，同一个 `serverId` 再 join 覆盖旧记录。
  - hasJoined 按 `serverId` 一次查询，名字不区分大小写，给了 `ip` 就核对；角色在中间被删或主人被封禁时返回 204（修 Y05）。
  - 封禁或已注销用户的角色对游戏服务器"不存在"：profile、查询、hasJoined 都不返回。
- **签名**：
  - RSA 4096 位私钥（PKCS#8）与令牌 HMAC 密钥存在新表 `site_secrets`，启动时加载或生成并常驻内存（修 Y06）。多个实例同时首次启动时各自生成，`INSERT … DO NOTHING` 后再读回，以先写入的为准。
  - 设计文档原写"数据目录的密钥文件"（Y09）；放进数据库是为了多实例共享同一把签名密钥（否则不同实例签出的 profile 互不认），并且随数据库一起备份。
  - profile 属性用 SHA1withRSA 签名（同 Mojang 与 authlib-injector）。`textures` 的 `timestamp` 取角色的更新时间而不是当前时间，同一份内容的签名不变，签名结果按值缓存（最多 10000 条）：一次 4096 位签名要几毫秒，hasJoined 是热路径。
  - `profile/{uuid}` 默认不签名，`?unsigned=false` 才签；hasJoined 总是签名。
- **登录与节流**（修 Y04）：
  - 新增 `account.VerifyCredentials`：与网页登录相同的三个计数器（每个地址、每个"地址 + 账号"、每个账号），没有验证码可显示，所以按账号的上限总是生效；不创建会话。按"地址 + 账号"的计数（15 分钟 10 次）先拦住单一来源，按账号的（15 分钟 100 次）只在分布式猜密码时才会触发。
  - 不存在的角色名与密码错误的回应相同，同样计数。
  - 登录写审计 `yggdrasil.login`。
- **材质上传**：Bearer 令牌的主人必须就是角色的主人（BS 接受任何有效令牌）。上传走统一的上传管线：私有材质（同 BS），照常收积分；库里已有同一张图时加进衣柜；然后穿上。错误同样是 Yggdrasil 格式（积分不足、尺寸不对），令牌无效 401，角色不存在 404。
- **吊销**：改密码、重置密码、封禁、注销账号、"退出其它所有设备"时，启动器令牌与网页会话一起失效（03 S02）。个人资料页的"游戏启动器"显示登录着的启动器数量，可以一键全部退出（`GET /api/v1/me/yggdrasil`、`POST /api/v1/me/yggdrasil/signout`）。
- **启动器引导**（F19）：
  - `yggdrasil.enable_ali`（默认开）时，网关在**所有**响应上加 `X-Authlib-Injector-API-Location: /api/yggdrasil`（`gateway.SetHeaders`，包括前端页面），启动器只填站点地址就能找到接口。
  - 首页"启动器"区块与个人资料页给出接口地址、复制按钮，以及可以拖进启动器的 `authlib-injector:yggdrasil-server:<地址>` 链接。
- **UUID**：用户的 UUID 与 BS 插件相同（邮箱的 v5 UUID），但一律用规范化后的邮箱计算；角色 UUID 沿用 ADR 0018。

## 备选方案
- 不透明随机令牌：更短，但迁移过来的启动器拿着 BS 签的 JWT。保持格式没有成本，有效性本来就看数据库。
- 令牌放 Redis：单机部署没有 Redis；数据库按唯一索引查一行已经够快，而且可以和会话一起在事务里吊销。
- 签名密钥放数据目录的文件：多实例需要另外同步，备份也要单独处理。
- 用 Huma 定义这组接口：错误格式、状态码（204 表示"没有"）都与 Huma 的约定冲突，需要大量覆盖，收益只是一份没人用的 OpenAPI。

## 影响
- 迁移 `20261029123138_yggdrasil`：`site_secrets`、`ygg_tokens`、`ygg_sessions` 三张新表（只建表，SQLite 上不重建已有表）。
- 新设置 `yggdrasil.token_valid_seconds`、`yggdrasil.token_refresh_seconds`、`yggdrasil.tokens_per_user`、`yggdrasil.search_profile_max`、`yggdrasil.skin_domains`、`yggdrasil.enable_ali`（对应 BS 的 `ygg_*` 选项）。
- 新包 `internal/yggdrasil`；`account.VerifyCredentials`；`gateway.SetHeaders`。
- 前端：首页启动器区块的拖拽链接，个人资料页"游戏启动器"。
- 没有做：Yggdrasil Connect（OIDC，F58）与正版验证（F59），在 OAuth2 Provider 之后再看；BS `uuid` 表与令牌的迁移由 M5 的迁移工具负责（令牌不迁移，玩家重新登录一次）。
