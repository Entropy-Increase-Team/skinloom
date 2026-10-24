---
title: 角色与游戏客户端
sidebar_position: 5
---

# 角色与游戏客户端

角色（player）是用户在游戏里的名字。用户在 `/user/player` 创建角色，从衣柜里给它挑皮肤和披风；游戏客户端按角色名向站点要皮肤。

## 角色

- 创建角色消耗 `score.per_player` 积分（默认 100）。删除角色时，如果开启了 `score.return_on_delete`（默认开启），退还**当时支付的**积分，之后调价不影响退款。
- 角色名在全站唯一，**不区分大小写**：有了 `Steve` 就不能再建 `steve`。两个人同时抢同一个名字，只会有一个人成功。
- 名字规则 `player.name_rule`：

  | 规则 | 允许 |
  |---|---|
  | `official`（默认） | 英文字母、数字、下划线，与正版一致 |
  | `cjk` | 再加汉字 |
  | `utf8` | 除空格外的任意字符 |
  | `custom` | `player.name_regexp` 规定的格式（Go RE2 语法） |

  长度由 `player.name_length_min` / `player.name_length_max` 决定（默认 3–16，最多 32）。无论哪条规则，名字都不能包含空格和 `/ \ ? # %`，否则游戏客户端拼出的地址会出错。
- 角色只能穿**自己衣柜里**的材质，而且皮肤只能穿皮肤、披风只能穿披风。材质被删除后，穿着它的角色自动换回"没有皮肤"。
- 每个角色都有 UUID，外置登录（M3）使用它：`yggdrasil.uuid_algorithm = v3`（默认）与离线服的算法相同，改名后 UUID 随之改变；`v4` 是随机的，改名不变。

## 让游戏显示皮肤

把站点地址换成你的 `site.url`，例如 `https://skins.example.com`。

**CustomSkinLoader**：编辑 `.minecraft/CustomSkinLoader/CustomSkinLoader.json`，在 `loadlist` 最前面加一项：

```json
{
  "name": "SkinLoom",
  "type": "CustomSkinAPI",
  "root": "https://skins.example.com/"
}
```

**UniSkinMod**：添加根地址 `https://skins.example.com/usm/`。

**只支持固定图片地址的客户端**（Legacy）：皮肤 `https://skins.example.com/skin/{角色名}.png`，披风 `https://skins.example.com/cape/{角色名}.png`。

使用外置登录（authlib-injector）的启动器不需要这些配置，外置登录将在 M3 提供。

## 接口

| 地址 | 返回 |
|---|---|
| `/{角色名}.json`、`/csl/{角色名}.json` | CustomSkinAPI：`{"username": "Steve", "skins": {"default": "<哈希>"}, "cape": "<哈希>"}`。`skins` 只有一个键：经典皮肤是 `default`，纤细皮肤是 `slim`；没有皮肤时是 `{"default": null}` |
| `/usm/{角色名}.json` | UniSkinMod 格式（`player_name`、`last_update`、`model_preference`、`skins`、`cape`） |
| `/skin/{角色名}.png`、`/cape/{角色名}.png` | 图片本身 |

- 角色名不区分大小写，返回的是库内的写法。
- 角色不存在：404（`delivery.missing_player_status = 204` 时，CustomSkinLoader 接口返回 204，客户端会接着去试下一个皮肤站）；角色主人被封禁：403；主人已注销：视为不存在。
- 客户端和 CDN 可以缓存 `delivery.profile_cache_seconds` 秒（默认 60），并用 `If-Modified-Since` 验证。Blessing Skin 默认缓存一年，换了皮肤之后很久都看不到。
- 材质文件本身在 `/textures/<哈希>`，内容不会变化，可以永久缓存。
