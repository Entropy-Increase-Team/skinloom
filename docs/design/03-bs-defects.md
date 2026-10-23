# 03 · 缺陷与性能问题清单

> 基线：`blessing-skin-server@52f6fefe`、`blessing-skin-plugins@2026-01-14`（yggdrasil-api 5.2.1）。
> 严重度：🔴 高（可被利用 / 数据损坏 / 大站明显故障）· 🟠 中 · 🟡 低。
> 依据：✅ 代码可直接确认 · 🔶 由代码推断，需在运行环境复现 · 📌 上游 issue 已有报告。
> 每一条最后一列写"新系统怎么从结构上避免"，这些就是 [04-重构方案](04-rewrite-plan.md) 里设计约束的来源。

代码链接固定到 blessing-skin-server@`52f6fefe` 与 blessing-skin-plugins@`de016f4c` 的永久链接。

---

## 0. 总结：问题的五个根源

| 根源 | 表现 | 影响条目 |
|---|---|---|
| **数据库没有约束** | 20 个迁移里没有一个业务索引、唯一约束或外键；全项目 `transaction` 出现次数 = 0 | C01–C09、P01–P02 |
| **读-改-写的积分与计数** | `$user->score -= x; $user->save()` 写回绝对值，并发即丢更新 | C03–C05 |
| **PHP 每请求冷启动 + 文件驱动** | 每个请求扫描插件目录、读全部配置、引导框架；默认 file 缓存/会话、sync 队列 | P03–P08 |
| **插件 = 同进程任意代码，契约无版本** | 事件参数不一致、过滤器无类型、插件能改任何东西、市场无签名 | A02–A04、S09–S10 |
| **"请求里顺便干活"** | GET 删数据、读配置时写库、请求内跑迁移、同步发邮件、同步扇出通知 | C10、P06、D03 |

---

## 1. 安全（S）

| ID | 问题 | 位置 | 影响 | 依据 | 新系统对策 |
|---|---|---|---|---|---|
| S01 🔴 | **客户端 IP 可伪造**：`new Whip()` 默认读取 `X-Forwarded-For` 等代理头（官方另出 remote-addr 插件改用 `REMOTE_ADDR`，侧面印证）。注册 IP 限流、登录失败验证码阈值、找回密码频率限制都基于它 | [AuthController.php:27](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L27)、:81、:187、:266 | 批量注册、暴力破解绕过验证码、邮件轰炸 | 🔶 | 只信任配置的代理 CIDR（`trusted_proxies`）；限流同时按账号维度 |
| S02 🔴 | **密码重置链接 1 小时内可无限次使用**，重置后不作废，也不吊销已有会话/记住我/OAuth 令牌/Yggdrasil 令牌 | [AuthController.php:282](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L282)、:317 | 链接泄露（邮件转发、日志、代理）即可反复改密 | ✅📌#675 | 一次性令牌（存哈希）、30 分钟；重置/改密后全部会话与令牌失效 |
| S03 🔴 | **邮箱验证链接永久有效**（`signedRoute` 无过期）且与 APP_KEY 绑定 | [UserController.php:160](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UserController.php#L160)、[SendEmailVerification.php:15](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Listeners/SendEmailVerification.php#L15) | 同上；APP_KEY 泄露时可批量伪造 | ✅📌#675 | 一次性令牌 + 过期 |
| S04 🔴 | **`?lang=` 未校验即持久化到 `users.locale`**，之后该用户每个请求 `setLocale(非法值)` 报 500 | [SetAppLocale.php:23](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Listeners/SetAppLocale.php#L23) | 任何已登录用户（含管理员，诱导点击即可）账号永久不可用 | ✅📌#684 | locale 白名单校验 |
| S05 🟠 | **用户枚举**：登录区分"用户不存在(code 2)/密码错误"；找回密码返回"邮箱未注册" | [AuthController.php:92](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L92)、:277 | 撞库前置 | ✅ | 统一响应；找回密码总是返回"若存在则已发送" |
| S06 🟠 | **异常信息直出**：邮件发送失败把 `$e->getMessage()`（含 SMTP 主机/认证错误）返回给用户；JSON 异常带文件路径 trace | [AuthController.php:294](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L294)、[Handler.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Exceptions/Handler.php) | 信息泄露 | ✅ | 对外只返回错误码 + request id，详情进日志 |
| S07 🟠 | **皮肤库排序字段未白名单**：`orderBy($request->sort)`，且查询 join 了 users 表 → 可按 `email`/`ip`/`password` 排序（侧信道），非法列名直接 500 | [SkinlibController.php:69](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L69) | 信息侧信道、错误日志刷屏 | ✅ | 排序枚举 |
| S08 🟠 | **改密码/改邮箱只登出当前会话**，其它设备与"记住我"仍有效 | [UserController.php:246](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UserController.php#L246)、:271 | 账号被盗后无法踢人 | ✅ | 会话表 + 一键吊销 |
| S09 🟠 | **普通管理员即可安装市场插件、启用/删除任意插件**（只有上传 zip 与 URL 下载要求超管）；插件 = 任意 PHP 代码 | [web.php:186-195](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/routes/web.php#L186) | 管理员→服务器代码执行，权限边界失效 | ✅ | 插件安装/启用仅超管；插件沙箱 + 权限声明 |
| S10 🟠 | **插件市场与在线更新无签名/校验和**：下载 zip 直接解压到插件目录或站点根目录 | [MarketController.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/MarketController.php)、[UpdateController.php:48](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UpdateController.php#L48) | 源被劫持/投毒 = 全站 RCE | ✅ | 包签名（ed25519）+ sha256；取消在线覆盖更新 |
| S11 🟠 | **安装向导把表单值用正则写进 `.env`**，未转义换行/`$`，可注入任意配置项；未安装状态下任何访问者都能抢先完成安装；GET 请求时把 `.env` 里的 DB 密码回显到表单 | [SetupController.php:66](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SetupController.php#L66) | 首装窗口期被接管 | ✅ | 不写 .env；setup token（控制台打印）才能进入向导 |
| S12 🟠 | **Dockerfile 在构建期 `key:generate`**，用同一镜像起的所有实例共享 APP_KEY（会话 Cookie 加密、签名 URL 都依赖它） | [Dockerfile](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/Dockerfile) | 社区分发的镜像可被伪造签名链接 | ✅ | 首次启动生成密钥写入数据卷 |
| S13 🟡 | `NotifyFailedPlugin` 把插件标题拼进 `<script>` 字符串 | [NotifyFailedPlugin.php:18](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Listeners/NotifyFailedPlugin.php#L18) | 插件标题含引号时 XSS（需恶意插件） | ✅ | 前端渲染，不拼脚本 |
| S14 🟡 | 信任任意来源的 `X-Forwarded-Proto` 来决定生成 https URL | [AppServiceProvider.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Providers/AppServiceProvider.php) | 链接方案错乱 | ✅ | 同 S01 的可信代理 |
| S15 🟡 | 登录密码 `max:32`、管理员改密 `max:16`、注册 `min:8`、登录 `min:6`，规则不一致 | [AuthController.php:57](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L57)、[UsersManagementController.php:94](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UsersManagementController.php#L94) | 长密码用户无法登录 | ✅ | 统一策略：8–128 |
| S16 🟡 | 自定义 JS/CSS 直接注入每个页面，无 CSP | [HeadComposer.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/View/Composers/HeadComposer.php)、[FootComposer.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/View/Composers/FootComposer.php) | 管理员账号被盗 = 全站存储型 XSS | ✅ | 仅超管可改 + CSP nonce |

## 2. 正确性与数据完整性（C）

| ID | 问题 | 位置 | 影响 | 依据 | 新系统对策 |
|---|---|---|---|---|---|
| C01 🔴 | **角色名无唯一约束**，校验-插入之间有竞态 → 同名角色；CSL/Yggdrasil 只取第一条 | [PlayerController.php:89](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayerController.php#L89)、迁移 | 皮肤串号、UUID 错乱 | ✅📌#443 | `UNIQUE(name_lower)` |
| C02 🔴 | **邮箱无唯一约束且大小写敏感**（PG/SQLite 下 `A@x.com` 与 `a@x.com` 可并存）；注册唯一性校验同样有竞态 | [AuthController.php:170](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L170)、迁移 | 重复账号、登录歧义 | ✅📌#581 | 规范化后 `UNIQUE(email_normalized)` |
| C03 🔴 | **积分读-改-写**：签到、上传、建角色、收藏、举报、管理员改分全部 `score -= x; save()`。并发签到可多次得分，并发消费可透支 | [UserController.php:127](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UserController.php#L127)、[SkinlibController.php:326](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L326)、[PlayerController.php:115](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayerController.php#L115) | 刷分、负积分 | ✅ | 原子 `UPDATE … SET score = score - ? WHERE score >= ?` + 积分流水表 + 签到唯一约束 |
| C04 🟠 | `likes` 计数同样读-改-写，与衣柜实际行数漂移 | [ClosetController.php:117](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/ClosetController.php#L117) | 排行不准 | ✅ | 原子增减 + 定期校准任务 |
| C05 🟠 | **积分规则不对称**：收藏自己的材质不奖励自己，但取消收藏时照样从自己（上传者）扣 `score_award_per_like`；上传者账号已删时取消收藏直接 500 | [ClosetController.php:193](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/ClosetController.php#L193) | 积分错误、500 | ✅ | 积分规则集中在 score 服务，按流水反向冲正 |
| C06 🔴 | **举报处理"删除材质"直接删文件，不检查同 hash 的其它材质**（私有重复上传会共享文件）→ 其它用户的材质变成坏图 | [ReportController.php:112](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/ReportController.php#L112)（对比 [SkinlibController.php:347](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L347) 有检查） | 误删他人材质 | ✅ | blob 引用计数，删除走唯一入口 |
| C07 🟠 | **角色可以把披风设成皮肤**：`setTexture` 只校验"在衣柜里"，不校验材质类型；管理员接口同样不校验 | [PlayerController.php:222](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayerController.php#L222)、[PlayersManagementController.php:114](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayersManagementController.php#L114) | 客户端显示异常 | ✅ | 类型约束 |
| C08 🟠 | **修改材质类型**（steve↔cape）不重新校验尺寸，也不处理已经在用它的角色 | [SkinlibController.php:454](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L454) | 角色挂着错误类型材质 | ✅ | 改类型 = 校验尺寸 + 解除不兼容引用 |
| C09 🟠 | **删除用户只删角色**，材质、衣柜、举报、通知、OAuth 令牌全部留成孤儿；之后：皮肤库 `INNER JOIN users` 让这些材质"消失"，`/{name}.json`、管理端角色操作遇到孤儿角色 `$player->user->permission` 直接 500 | [User.php:101](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Models/User.php#L101)、[TextureController.php:36](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/TextureController.php#L36)、[PlayersManagementController.php:26](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayersManagementController.php#L26) | 数据垃圾、500 | ✅ | 外键 + 明确的删除策略（材质转"已注销用户"，其余级联） |
| C10 🟠 | **GET 请求删数据**：`auto_del_invalid_texture` 开启时，访问详情页发现文件缺失就删材质记录 | [SkinlibController.php:105](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L105) | 存储短暂抖动 = 永久删数据 | ✅ | 后台巡检任务 + 人工确认 |
| C11 🟠 | **材质删除时上传者自己的衣柜行不清理**（只清"非上传者"），孤儿行累积 | [CleanUpCloset.php:20](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Listeners/CleanUpCloset.php#L20) | 数据垃圾 | ✅ | 外键级联 |
| C12 🟠 | **公开↔私有来回切换会持续丢分**：公开→私有收回上传奖励，私有→公开不归还 | [SkinlibController.php:358](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L358) `privacy()` | 积分错误 | ✅ | 按流水计算差额 |
| C13 🟠 | **OAuth scope 拼写错误** `Closet.ReadWrtie`，路由要求 `Closet.ReadWrite` → 第三方应用无法获得衣柜写权限 | [AuthServiceProvider.php:33](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Providers/AuthServiceProvider.php#L33) | 功能不可用 | ✅ | 修正并兼容旧拼写 |
| C14 🟠 | **头像/预览缓存键缺参数**：预览缓存键不含 `height`，第一次请求的高度被缓存一年，其它高度全部拿到错误尺寸 | [TextureController.php:62](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/TextureController.php#L62) | 显示错误 | ✅ | 缓存键 = 规范化后的全部参数 |
| C15 🟠 | **通知已读接口 id 不存在时 500**；OAuth API 发送通知返回的是 302 跳转 `/admin` | [NotificationsController.php:62](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/NotificationsController.php#L62)、:43 | API 不可用 | ✅ | 统一错误模型 |
| C16 🟡 | 密码重置时 `User::find($uid)` 为空直接 500 | [AuthController.php:317](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AuthController.php#L317) | | ✅ | |
| C17 🟡 | 昵称修改无长度校验（列宽 50）→ MySQL 严格模式下 500 | [UserController.php:221](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UserController.php#L221) | | ✅ | 统一校验层 |
| C18 🟡 | **事件参数签名不一致**：`player.texture.updating` 在用户端是 `[player, texture]`，管理端是 `[player, type, tid]`；`player.renamed` 一处传旧 Player 对象，一处传旧名字符串 | [PlayerController.php:184/220](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayerController.php#L220)、[PlayersManagementController.php:65/108](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/PlayersManagementController.php#L108) | 插件只能猜参数 | ✅ | 强类型、带版本的事件定义 |
| C19 🟡 | `Option::get` 对 `null` 调 `strtolower`（PHP 8.1 起弃用告警）；默认值 `score_per_storage => 'true'` 靠"true 被当成 1"碰巧工作 | [Option.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Services/Option.php)、[config/options.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/config/options.php) | 隐患 | ✅ | 强类型配置 + schema |
| C20 🟡 | 皮肤库带参数访问会发出多次请求；高清皮肤预览显示异常；Alex 材质渲染错误 | 前端 | | 📌#634 #682 #361 | 新前端 + 渲染器重写 |

## 3. 性能（P）

> 上游 issue #614 的实测：LittleSkin（146 万用户、35.7 万材质）`/skinlib/list` TTFB 1.3–1.8s；红石皮肤站（20.6 万用户、5.2 万材质）0.8–1.2s；MUA（2 千用户）0.12–0.2s —— **响应时间随数据量线性增长**，典型的全表扫描特征。

| ID | 问题 | 位置 | 影响 | 依据 | 新系统对策 |
|---|---|---|---|---|---|
| P01 🔴 | **热路径全表扫描**：`players.name`（每次 CSL/USM/Legacy/Yggdrasil 请求、角色名登录）、`textures.hash`（去重、按 hash 取头像/预览、删除前计数）、`users.email`（登录）、`users.ip`（注册限流）、`textures.uploader`（用量统计）、`user_closet` 两列（每次衣柜操作）、插件 `uuid.name/uuid`（每次 join/hasJoined/profile）都**没有索引** | 全部迁移 | 数据量一大所有接口一起变慢 | ✅ | 见 04 §5 的索引设计 |
| P02 🔴 | **皮肤库列表**：`COUNT(*) + INNER JOIN users + LIKE '%kw%' + OFFSET 分页`，每翻一页都做一次全量 count，排序列无索引 | [SkinlibController.php:69-94](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/SkinlibController.php#L69) | 见上方实测 | ✅📌#614 | 复合索引 + 游标分页 + 总数缓存 + 可选全文索引 |
| P03 🟠 | **每个请求扫描插件目录**并读取解析每个插件的 `package.json`、注册 autoload/视图/翻译；已启用插件全部执行 `bootstrap.php` | [PluginManager.php:62](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Services/PluginManager.php#L62) | 插件越多越慢，包括材质与 JSON 这种最热的接口 | ✅ | 插件在启动时加载一次（常驻进程） |
| P04 🟠 | **每个请求读整张 options 表**（除非手动 `options:cache`，而缓存后改配置又不生效）；读取不存在的键时**在读路径上写库** | [Option.php:36/71](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Services/Option.php#L36) | 额外 DB 往返、写放大 | ✅ | 内存配置 + 变更时广播失效 |
| P05 🟠 | **材质文件经 PHP 输出**：整个文件读进内存、无 Range、无 sendfile；`cache.headers:etag` 让 Laravel 对每次响应体算 md5 | [TextureController.php:100](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/TextureController.php#L100) | 最热的接口最浪费 | ✅ | `http.ServeContent` / 302 到 CDN；hash 即 ETag；`immutable` |
| P06 🟠 | **渲染结果把整个 Response 对象序列化进缓存**（默认 file 驱动），尺寸参数无上限 → `?size=8192` 既打满 CPU 又往缓存里塞大文件，且缓存一年 | [TextureController.php:59/142/61/157](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/TextureController.php#L142) | CPU/磁盘 DoS | ✅ | 尺寸白名单、只缓存图片字节、singleflight 合并并发、有界渲染池 |
| P07 🟠 | **群发通知 = `User::all()` + 每人一行**，`sync` 队列下在请求里完成 | [NotificationsController.php:27](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/NotificationsController.php#L27) | 大站群发必超时/内存溢出 | ✅ | 广播公告表 + 已读表 |
| P08 🟠 | **每个页面渲染都加载当前用户全部未读通知**（无 limit） | [ViewServiceProvider.php:33](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Providers/ViewServiceProvider.php#L33) | 通知多的用户页面变慢 | ✅ | 计数 + 分页接口 |
| P09 🟠 | **后台图表把近 31 天的注册/上传记录全部读进 PHP 再分组** | [AdminController.php:61/68](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/AdminController.php#L61) | 大站后台首页慢 | ✅ | SQL `GROUP BY date` + 缓存 |
| P10 🟠 | **前端语言包每次页面渲染都 stat 所有已启用插件的语言文件**，并由 Web 进程写 `public/lang/*.js` | [JavaScript.php:37](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Services/Translations/JavaScript.php#L37) | IO 抖动；要求 public 可写 | ✅ | 构建期/启动期生成，内存提供 |
| P11 🟠 | **静态路由（材质/JSON）也被设置 `locale` Cookie**：全局中间件只对带 `api` 中间件的路由跳过 | [DetectLanguagePrefer.php:32](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Middleware/DetectLanguagePrefer.php#L32) | 带 `Set-Cookie` 的响应多数 CDN 不缓存 → CDN 命中率下降 | 🔶 | 游戏/静态接口不碰会话与 Cookie |
| P12 🟠 | **注册时同步发验证邮件**、找回密码同步发信 | [SendEmailVerification.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Listeners/SendEmailVerification.php) | SMTP 慢 = 注册慢/超时 | ✅ | 异步任务 + 重试 |
| P13 🟡 | `RedirectToSetup` 每个 web 请求 stat `install.lock` + 比较版本 | [RedirectToSetup.php:23](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Middleware/RedirectToSetup.php#L23) | | ✅ | 启动时确定 |
| P14 🟡 | 存储用量 `SUM(size) WHERE uploader=?` 无索引；后台总存储 `SUM` 全表 | [UserController.php:92](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/UserController.php#L92) | | ✅ | 索引 + 冗余计数 |
| P15 🟡 | `user/closet/list` 的 `perPage` 无上限 | [ClosetController.php:64](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Controllers/ClosetController.php#L64) | 一次拉全量 | ✅ | 上限 100 |
| P16 🟡 | 默认 file 缓存/会话、本地材质盘、插件资源复制进 `public/` → 无法多实例 | `.env.example` | 只能纵向扩展 | ✅ | 无状态进程 + 可插拔后端 |

> 侧证：官方插件里有一个 **insane-profile-cache**（"直接生成玩家 Profile 的文件缓存，交给 Nginx 处理，慎用"）——说明 profile JSON 的性能问题在大站真实存在，只能靠绕开 PHP 解决。

## 4. 架构与可维护性（A）

| ID | 问题 | 新系统对策 |
|---|---|---|
| A01 | **胖控制器**：校验、权限、积分、事件、持久化全在控制器里，同一逻辑在用户端/管理端/OAuth API 三处各写一份且行为不一致（C07、C18） | 分层：handler → service（唯一业务入口）→ repository |
| A02 | **钩子没有契约**：72 个字符串事件 + 23 个类事件 + 45 个过滤器，参数靠文档（文档有的还是 Vue 时代的描述），无版本 | 事件/过滤器用强类型结构定义，SDK 生成文档，带 API 版本号 |
| A03 | **插件与核心同进程、同权限**：一个插件 bootstrap 出错，debug 模式下全站 500；插件能替换任意容器绑定 | 沙箱插件 + 权限声明 + 故障隔离 |
| A04 | **插件没有迁移机制**：在 `callbacks.php` 里 `Schema::hasTable` 判断后建表 | 插件自带版本化迁移或使用宿主提供的 KV/命名空间表 |
| A05 | **前端技术债**：Twig + React 17 孤岛 + jQuery + AdminLTE 3/Bootstrap 4；`window.React/$` 成了插件公共 API；TS 4.3、ESLint 7、webpack-dev-server 3 | 新前端（Next.js），插件前端通过 SDK 的扩展点在运行时挂载 |
| A06 | **配置无类型**：字符串 `'true'`/`'(null)'`；多语言配置靠 `key_{locale}` 后缀 | settings schema（类型、默认值、校验、多语言） |
| A07 | **框架/运行时过保**：Laravel 10、PHP 8.1 都已停止安全更新 | 📌#674 |
| A08 | 业务副作用藏在监听器里（积分返还、衣柜清理、角色重置），调用方看不出一个操作会改哪些表 | 显式的领域服务 + 事务；事件只用于通知，不承载核心一致性 |

## 5. 部署与运维（D）

| ID | 问题 | 位置 | 新系统对策 |
|---|---|---|---|
| D01 🔴 | **仓库 Dockerfile 构建出来的镜像无法启动**：基础镜像只装 `gd zip`，但 `chkenv.php` 强制要求 imagick（缺失直接 die），上传也依赖 imagick 驱动 | [Dockerfile](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/Dockerfile)、[chkenv.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/bootstrap/chkenv.php)、[config/image.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/config/image.php) | 纯 Go、`CGO_ENABLED=0`，无系统依赖 |
| D02 🟠 | 基础镜像用浮动标签 `php:8-apache`（已到 8.4/8.5，超出 Laravel 10 支持范围） | 同上 | 固定版本 + Renovate |
| D03 🟠 | **升级在第一个 web 请求里执行**迁移 + `Cache::flush()`（顺带清空所有 Yggdrasil 令牌 → 全站掉线），多个并发请求可能同时迁移 | [RedirectToSetup.php:24](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Http/Middleware/RedirectToSetup.php#L24)、[UpdateCommand.php](https://github.com/bs-community/blessing-skin-server/blob/52f6fefed08e091c08e409c67e31d00b00ff00ee/app/Console/Commands/UpdateCommand.php) | 启动时迁移 + 分布式锁；令牌持久化 |
| D04 🟠 | 运行时要求 `public/`、`plugins/`、`bootstrap/cache` 可写（Web 进程能写 Web 根目录） | chkenv | 代码只读，数据只写数据目录 |
| D05 🟡 | 安装选 SQLite 仍要求填写地址；邮件链接在反代/CDN 后 403（签名 URL 与 Host 绑定） | 📌#622 #608 #636 | 安装流程按驱动区分；令牌链接不依赖 Host |

## 6. Yggdrasil API 插件（Y）

| ID | 问题 | 位置 | 依据 | 新系统对策 |
|---|---|---|---|---|
| Y01 🔴 | **令牌只存在缓存里**：缓存清空/驱逐（升级时 `Cache::flush()`、Redis maxmemory）= 所有玩家掉线 | [Token.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Models/Token.php) | ✅ | DB 持久化（存哈希）+ 缓存前置 |
| Y02 🔴 | **缓存键大小写/标识不一致**：authenticate 用原始邮箱存令牌列表（[AuthController.php:76](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Controllers/AuthController.php#L76)），signout/refresh 用 `strtolower(username)`（:24、:246）。结果：邮箱含大写字母的用户刷新后的令牌校验失败；用角色名或大写邮箱 signout 不会吊销任何令牌 | ✅📌插件#212 | 令牌按 user_id 归属 |
| Y03 🟠 | **`uuid` 表无唯一约束、按角色名关联**：并发 `getUuidFromName` 会插重复行；删角色不删映射；改名只差大小写会丢 UUID | [Profile.php:145](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Models/Profile.php#L145)、[callbacks.php:12](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/callbacks.php#L12) | ✅📌插件#151 #152 #202 | `players.uuid UNIQUE`，按角色 ID 关联 |
| Y04 🟠 | **登录节流按请求体里的 `username` 计**：攻击者换用户名即可绕过，也能故意刷某个用户名让真用户被锁 | [Throttle.php:13](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Middleware/Throttle.php#L13) | ✅ | 账号 + IP 双维度令牌桶 |
| Y05 🟠 | `hasJoined`：join 之后角色被删，`createFromUuid` 返回 null → 500 | [SessionController.php:113-121](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Controllers/SessionController.php#L113) | ✅ | 显式空值处理 |
| Y06 🟠 | **每次序列化 profile 跑两次 `Schema::hasTable('mojang_verifications')`**（元数据查询）+ 每次重新解析 RSA 私钥 PEM | [Profile.php:49/70/85](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Models/Profile.php#L70) | ✅ | 启动时解析密钥；功能开关替代运行时探测表 |
| Y07 🟠 | 改角色名后旧令牌仍有效；用角色名登录不自动选择该角色；profile 返回名大小写与库不符 | — | 📌插件#231 #123 #232 | 照 yggdrasil-connect 的修法内置 |
| Y08 🟡 | 用户令牌列表 `yggdrasil-id-{email}` 是缓存里的数组，读-改-写，并发登录会丢令牌；无 TTL 永不过期 | [AuthController.php:372](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/src/Controllers/AuthController.php#L372) | ✅ | DB 表 + 上限裁剪 |
| Y09 🟡 | 启用时由 Web 进程往 `.env` 追加 `JWT_SECRET` | [callbacks.php](https://github.com/bs-community/blessing-skin-plugins/blob/de016f4cbb43df7bd6d46193d5625693f8c79b3f/plugins/yggdrasil-api/callbacks.php) | ✅ | 密钥统一由数据目录的密钥文件管理 |

---

## 7. 对重构的直接约束（从本清单推导）

1. **所有不变量进数据库**：唯一（邮箱规范化、角色名小写、角色 UUID、衣柜 (user,texture)、签到周期）、外键、CHECK（score ≥ 0）。
2. **所有写操作在事务里**，积分与计数用原子 SQL + 流水。
3. **游戏协议接口是"静态级"热路径**：不碰会话、不设 Cookie、单次索引查询、可被 CDN 长缓存、变更时主动刷新。
4. **任何用户可控的数值参数都有上限或白名单**（size、height、perPage、批量查询数）。
5. **令牌类数据（重置、验证、会话、OAuth、Yggdrasil）一律落库存哈希**，缓存只做加速。
6. **插件拿不到裸数据库和进程权限**，只能通过声明过的能力访问宿主。
7. **请求路径上不做运维动作**（迁移、清缓存、写配置文件、删数据）。
