# 0018 角色、UUID 与游戏皮肤接口

- 状态：已接受（2026-10-24）
- 相关：设计文档 02 §1.2（F15）、§1.5（F50、F54–F56）、§2.1–2.4、§4；03（C01、C05、C07、P01）；04 §5.1、§5.4

## 背景
M2 第二部分：角色与游戏客户端拉取皮肤的接口。Blessing Skin 的问题：`players.name` 没有唯一约束，校验与插入之间有竞态，会出现同名角色（C01），而 CSL/Yggdrasil 只取第一条；角色可以把披风当皮肤穿（C07）；按名字查角色全表扫描（P01）；删除角色按"当前的" `score_per_player` 退分（C05 同类问题）；UUID 存在插件表里、按名字关联，改名、大小写会让角色和 UUID 对不上。

## 决策
- **players 表**：`name`（库内原样）、`name_lower`（唯一索引，查找与判重都用它：任何数据库下名字都不区分大小写，修 C01 与 P01）、`uuid`（32 位十六进制、无连字符，唯一）、`skin_id` / `cape_id`（材质删除时置空）、`updated_at`（作为 CSL 的 `Last-Modified` 与 USM 的 `last_update`）。用户删除时角色级联删除。
- **名字规则**沿用 BS 的 `player_name_rule`：`official`（`[A-Za-z0-9_]`，默认）、`cjk`（再加汉字与 §，与 BS 相同）、`utf8`（不含空白的任意可打印字符）、`custom`（`player.name_regexp`，Go RE2 语法）；长度 `player.name_length_min/max`（默认 3–16，数据库上限 32）。无论哪条规则，名字都不能包含空白、控制字符和 `/ \ ? # %`：这些字符会破坏游戏客户端拼出的 `/{name}.json`、`/skin/{name}.png` 地址（BS 的 utf8 规则允许 `/`，这样的角色根本拉不到皮肤）。设计文档写的是一个 `player.name_length` 区间，这里拆成两个整数设置，便于校验和在后台编辑。
- **UUID**（设计 02 §2.4）：`yggdrasil.uuid_algorithm`，`v3`（默认，与 BS 插件一致）= `md5("OfflinePlayer:" + 名字)` 设置版本位 3，与离线服完全相同，改名得到新 UUID；`v4` = 随机，改名保留。UUID 现在就随角色生成并落库，M3 的 Yggdrasil 直接使用，不需要回填。
- **积分**：创建扣 `score.per_player`（默认 100）；删除时若 `score.return_on_delete`（BS `return_score`），退还**当时支付的数额**（从积分流水里找到 `player.create` 那一条冲正），而不是按现在的价格重算（设计 04 §5.2）。
- **穿戴**：`PUT /api/v1/me/players/{id}/textures`，`skin` / `cape` 各自：省略 = 不变，`0` = 脱下，材质 id = 穿上。材质必须在角色主人的衣柜里（同 BS），并且类型正确：皮肤只能是 steve/alex，披风只能是 cape（修 C07）。
- **游戏接口**（契约 02 §2.1–2.3，均不读会话、不写 Cookie）：
  - CustomSkinLoader：`/{name}.json`、`/csl/{name}.json` → `{"username","skins":{"default"|"slim": hash|null},"cape": hash|null}`，`skins` 只有一个键，按皮肤模型命名。
  - UniSkinMod：`/usm/{name}.json` → `player_name`、`last_update`、`model_preference`、`skins`、`cape`；不存在时 `404 {"errno":1,"msg":"Player not found."}`。
  - Legacy：`/skin/{name}.png`、`/cape/{name}.png` 直接返回图片。
  - 名字不区分大小写，返回库内原样（修 BS #232）；主人被封禁 403；主人已注销（宽限期内）视为不存在；`delivery.missing_player_status` 为 204 时 CSL 的"不存在"返回 204（内置 BS 插件 204-for-unexisted-players，只作用于 CSL）。
  - 缓存：`Cache-Control: public, max-age=delivery.profile_cache_seconds`（默认 60 秒）+ `Last-Modified`，支持 `If-Modified-Since`。BS 的 `cache_expire_time` 默认一年，玩家换了皮肤，客户端和 CDN 却一直拿旧的；变更时主动刷新 CDN 在 M4。
  - 路由：路径参数取整个路径段、在处理函数里去掉 `.json` / `.png` 后缀（`/{player}.json` 这样的模式会在名字里的第一个点处截断）；chi 在原始路径带编码时给出的参数仍是编码过的，所以先 `PathUnescape`（客户端会把汉字名百分号编码，小写十六进制时尤其如此）。
- **前端**：`/user/player`（创建、改名、从衣柜里选皮肤与披风、脱下、删除、复制 UUID、查看 CSL 数据）；材质页对衣柜里的材质提供"穿到角色上"。

## 备选方案
- 进程内缓存序列化好的 JSON（设计 04 §5.4）：现在每次请求是一次唯一索引查询加两次按主键的材质查询，在压测（M5）证明需要之前不引入缓存与失效。
- 注册时用角色名代替昵称（BS `register_with_player_name`）：牵涉注册流程与界面，留待后续。
- 后台角色管理（F32）：随 M2 第四部分的后台概览一起做。

## 影响
- 新表 `players`（迁移 `20261024075025_players`）。
- 新设置 `player.name_rule`、`player.name_regexp`、`player.name_length_min`、`player.name_length_max`、`yggdrasil.uuid_algorithm`、`delivery.profile_cache_seconds`、`delivery.missing_player_status`、`score.per_player`、`score.return_on_delete`。
- 网关早已把 `/{name}.json`、`/csl/`、`/usm/`、`/skin/`、`/cape/` 交给后端，不需要改动。
