---
title: 外置登录与游戏接口
sidebar_position: 7
---

# 外置登录与游戏接口

SkinLoom 内置了游戏客户端需要的全部接口，不用装插件：

| 接口 | 地址 | 玩家怎么用 |
|---|---|---|
| authlib-injector 外置登录（Yggdrasil） | `/api/yggdrasil` | [外置登录](../players/in-game.md#外置登录) |
| CustomSkinLoader | `/` 或 `/csl/` | [皮肤 Mod](../players/in-game.md#皮肤-mod) |
| UniSkinMod | `/usm/` | 同上 |
| 固定图片地址 | `/skin/<角色名>.png`、`/cape/<角色名>.png` | 同上 |

协议与 Blessing Skin（含 yggdrasil-api 插件）相同，原来连 BS 的启动器、服务器和 Mod 把地址换成新站即可。

## 上线前检查

- **设置 `site.url`**：外置登录的元数据和材质地址都由它生成。
- **材质文件放在 CDN 上**（`storage.public_url`）时，它的域名会自动加入允许加载皮肤的列表；CDN 还有别的域名，写进 `yggdrasil.skin_domains`。
- 反向代理要正确转发 `Host` 和 `X-Forwarded-Proto`，见[反向代理与 HTTPS](../deployment/reverse-proxy.md)。

## 相关设置

| 键 | 默认 | 说明 |
|---|---|---|
| `yggdrasil.uuid_algorithm` | `v3` | 新角色的 UUID。`v3` 与离线服相同，改名后 UUID 跟着变；`v4` 随机，改名不变。服务器按 UUID 存玩家数据，所以开站前定好 |
| `yggdrasil.token_valid_seconds` | `259200` | 启动器登录 3 天有效 |
| `yggdrasil.token_refresh_seconds` | `604800` | 7 天内可以自动续期，超过后要重新输入密码 |
| `yggdrasil.tokens_per_user` | `10` | 每个账号最多同时登录几个启动器 |
| `yggdrasil.enable_ali` | `true` | 启动器只填站点地址就能找到接口 |
| `delivery.profile_cache_seconds` | `60` | 客户端和 CDN 缓存角色数据多少秒；改小换皮肤生效更快，改大服务器压力更小 |
| `delivery.missing_player_status` | `404` | 角色不存在时 CustomSkinLoader 接口返回 `404` 还是 `204`；`204` 让客户端继续试下一个皮肤站 |

## 你需要知道的

- 签名密钥在第一次启动时自动生成，存在数据库里，多个副本共用；备份数据库就备份了它。
- 启动器登录同样受登录保护：输错太多次需要等待。被封禁的账号在启动器里会看到提示。
- 用户改密码、"退出所有启动器"后，启动器需要重新登录。
- 接口的完整列表见[开放平台与接口](../development/api.md#游戏接口)。
