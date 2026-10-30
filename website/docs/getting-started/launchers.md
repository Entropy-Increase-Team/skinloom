---
title: 外置登录
sidebar_position: 6
---

# 外置登录

云织内置 [authlib-injector](https://github.com/yushijinhun/authlib-injector) 的 Yggdrasil API：玩家在启动器里用站点账号登录，进入开了正版验证的服务器，游戏里显示站点上的皮肤。协议与 Blessing Skin 的 yggdrasil-api 插件相同，原来连 BS 的启动器和服务器换成云织的地址即可。

接口地址是 `https://skins.example.com/api/yggdrasil`（把域名换成你的 `site.url`）。首页的"启动器"区块和个人资料页都会显示它。

## 玩家：在启动器里登录

1. 在 HMCL、PCL2、BakaXL 等支持 authlib-injector 的启动器里找到"添加外置登录 / 认证服务器"。
2. 把首页或个人资料页的"拖到启动器"直接拖进这个窗口；或者粘贴接口地址，只填站点地址也行，启动器会通过页面的 `X-Authlib-Injector-API-Location` 头找到接口。
3. 用站点的**邮箱**或**角色名**和密码登录：
   - 用角色名登录，启动器就以这个角色进游戏；
   - 用邮箱登录，账号只有一个角色时自动选中它，有多个时启动器会让你选。
4. 进游戏后，皮肤与披风就是这个角色在站点上穿的那套。在站点换了皮肤，重新进服就能看到。

支持的启动器也可以直接上传皮肤：材质会作为**私有材质**上传到你的衣柜（照常消耗积分）并穿到这个角色上。

**退出启动器**：个人资料页的"游戏启动器"显示有几个启动器登录着账号，"退出所有启动器"让它们全部需要重新登录。修改或重置密码、"退出其它所有设备"时，启动器也会一起退出。

## 服务器：开启外置登录

服务器同样需要 authlib-injector，并且开启正版验证：

1. 下载 [authlib-injector](https://github.com/yushijinhun/authlib-injector/releases) 的 jar，放到服务器目录。
2. 启动参数加上（放在 `-jar` 之前）：

   ```bash
   java -javaagent:authlib-injector.jar=https://skins.example.com/api/yggdrasil -jar server.jar
   ```

3. `server.properties` 里 `online-mode=true`。

使用 BungeeCord、Velocity 等代理时，authlib-injector 加在**代理**的启动参数上，代理开启正版验证，后端服务器按代理的要求关闭正版验证。

## 站长：设置

| 键 | 默认 | 说明 |
|---|---|---|
| `yggdrasil.token_valid_seconds` | `259200` | 启动器令牌的有效期（3 天） |
| `yggdrasil.token_refresh_seconds` | `604800` | 从签发起多久内过期的令牌还能刷新（7 天）；超过后需要重新输入密码 |
| `yggdrasil.tokens_per_user` | `10` | 每个账号最多同时登录几个启动器，再登录时最早的那个失效 |
| `yggdrasil.search_profile_max` | `5` | 按名字批量查询角色时一次最多几个 |
| `yggdrasil.skin_domains` | 空 | 另外允许游戏加载皮肤的域名，逗号分隔。站点域名和 `storage.public_url` 的域名总是包括在内 |
| `yggdrasil.enable_ali` | `true` | 在所有页面加 `X-Authlib-Injector-API-Location` 头 |
| `yggdrasil.uuid_algorithm` | `v3` | 新角色的 UUID，见[角色与游戏客户端](./players.md) |

注意：

- **设置 `site.url`**。元数据里的主页、注册地址和材质地址都由它生成；没有设置时按请求的地址推算，经过反向代理时需要代理正确转发 `Host` 与 `X-Forwarded-Proto`。
- 游戏只从 `skinDomains` 里的域名加载皮肤。材质文件放在 CDN（`storage.public_url`）上时，它的域名会自动加入；如果 CDN 还有别的域名，写进 `yggdrasil.skin_domains`。
- 签名密钥（RSA 4096 位）在第一次启动时生成，保存在数据库里，多个实例共用同一把；备份数据库就备份了它。
- 登录有频率限制：同一地址、同一地址对同一账号、同一账号各自计数，猜错太多次需要等一会儿再试，网页登录的计数也会算进来。被封禁的账号在启动器里会看到账号被封禁的提示（英文 "Your account has been banned."）。
- 启动器的令牌只在数据库里存哈希，升级、重启都不会让玩家掉线。

## 接口

| 地址 | 用途 |
|---|---|
| `GET /api/yggdrasil/` | 元数据：站点名、签名公钥、`skinDomains` |
| `POST /api/yggdrasil/authserver/authenticate`、`refresh`、`validate`、`invalidate`、`signout` | 启动器登录、刷新、校验、退出 |
| `POST /api/yggdrasil/sessionserver/session/minecraft/join` | 客户端进服 |
| `GET /api/yggdrasil/sessionserver/session/minecraft/hasJoined` | 服务器校验进服的玩家（带签名的角色信息） |
| `GET /api/yggdrasil/sessionserver/session/minecraft/profile/{uuid}` | 按 UUID 查角色，`?unsigned=false` 带签名 |
| `POST /api/yggdrasil/api/profiles/minecraft` | 按名字批量查角色 |
| `GET /api/yggdrasil/api/users/profiles/minecraft/{name}` | 按名字查一个角色 |
| `PUT`、`DELETE /api/yggdrasil/api/user/profile/{uuid}/{skin\|cape}` | 上传 / 清除皮肤或披风（`Authorization: Bearer <令牌>`） |
| `GET /api/yggdrasil/minecraftservices/publickeys` | 签名公钥 |

错误是 Yggdrasil 的格式：`{"error": "ForbiddenOperationException", "errorMessage": "Invalid token."}`。角色不存在时返回 204。封禁或已注销用户的角色对游戏服务器视为不存在。

从 Blessing Skin 迁移时，角色的 UUID 会原样保留（M5 的迁移工具），启动器里的登录不迁移，玩家重新登录一次即可。
