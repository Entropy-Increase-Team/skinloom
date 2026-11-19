---
title: 插件 API v1.0 参考手册
sidebar_position: 5
---

# 插件 API v1.0 参考手册

> **稳定性承诺**：自 SkinLoom 1.0 起，**`apiVersion: 1`** 与 **`native.protocolVersion: 1`** 契约正式冻结（仅允许向后兼容的新增字段、Hook、插槽与权限；任何重命名或破坏性变更都必须递增主版本号并发布 ADR）。

清单 JSON Schema：[`/schema/plugin.v1.json`](https://skinloom.cn/schema/plugin.v1.json)（仓库内 `website/static/schema/plugin.v1.json`，由 `internal/tools/plugingen` 单源生成）。

---

## 一、 权限目录与三级风险分级（29 项）

定义源：[`internal/plugin/permissions.go`](https://github.com/Entropy-Increase-Team/skinloom/blob/main/internal/plugin/permissions.go)。

### 普通权限（`normal`）

| 权限标识 | 说明 |
| --- | --- |
| `storage.kv` | 读写插件专属 KV 键值存储（支持 TTL 与原子计数） |
| `storage.docs` | 读写插件专属结构化文档集合（支持预置索引槽位） |
| `storage.files` | 读写插件私有文件空间（不混入材质存储） |
| `cron` | 注册定时后台任务 |
| `admin.ui` | 在管理后台注册自定义管理页面与导航菜单 |
| `users.read` | 查询基础账号信息 |
| `players.read` | 查询角色资料与穿戴材质 |
| `textures.read` | 读取材质元数据或图片字节 |
| `notify.send` | 发送站内通知或全站公告 |

### 敏感权限（`sensitive`）

| 权限标识 | 仅限 Native | 说明 |
| --- | :---: | --- |
| `http.fetch` | — | 向 `http.allow` 白名单域名发起出站 HTTP 请求（强制拦截私网/环回/云元数据 IP） |
| `mail.send` | — | 经由宿主邮件队列发送邮件 |
| `players.write` | — | 创建、改名、换装或删除角色 |
| `textures.write` | — | 创建、编辑或删除材质 |
| `closet.write` | — | 修改用户衣柜 |
| `score.write` | — | 通过积分流水给用户加减积分 |
| `users.write` | — | 更新受限用户资料字段（昵称、语言、验证状态） |
| `routes.public` | — | 在站点根路径挂载公开自定义路由 |
| `yggdrasil.extend` | — | 扩展 Yggdrasil 元数据与角色属性 |
| `cdn.provider` | — | 注册自定义 CDN 缓存刷新器 |
| `captcha.provider` | — | 注册自定义验证码提供方 |
| `storage.sql` | ✓ | 访问宿主分配的插件隔离 SQL 数据库（`plg_<id>_` 规范前缀） |
| `http.stream` | ✓ | 暴露本地 Unix Socket / Loopback 流式 HTTP/SSE/WebSocket 反向代理 |
| `provider.search` | ✓ | 注册自定义皮肤库全文搜索引擎驱动 |
| `provider.render` | ✓ | 注册自定义 2D/3D 头像与材质预览渲染引擎驱动 |

### 高危权限（`high_risk`，安装/升级需超级管理员二次确认）

| 权限标识 | 仅限 Native | 说明 |
| --- | :---: | --- |
| `users.ban` | — | 封禁或解封用户账号 |
| `auth.provider` | — | 注册自定义外部登录身份提供方 |
| `auth.password` | — | 注册外部密码哈希验证算法 |
| `provider.storage` | ✓ | 替换或提供材质对象存储后端驱动 |
| `native.exec` | ✓ | 以原生 OS 子进程方式运行在 WASM 沙箱之外（所有 `runtime: "native"` 插件自动附带） |

---

## 二、 后端 Hook 目录（47 个）

定义源：[`internal/hook/catalog.go`](https://github.com/Entropy-Increase-Team/skinloom/blob/main/internal/hook/catalog.go)。

### 1. 前置拦截 Guard（21 个，同步执行，返回 `reject(reason)` 即可阻断）

`auth.login` · `auth.register` · `closet.add` · `closet.remove` · `closet.rename` · `player.create` · `player.delete` · `player.rename` · `player.textures.clear` · `player.textures.set` · `report.submit` · `score.checkin` · `texture.delete` · `texture.rename` · `texture.type` · `texture.upload` · `texture.visibility` · `user.avatar.update` · `user.profile.update` · `yggdrasil.authenticate` · `yggdrasil.join`

### 2. 数值改写 Filter（7 个，同步链式改写）

| Hook 名称 | 改写值 | 用途 |
| --- | --- | --- |
| `closet.itemName` | `string` | 规范化衣柜条目显示名 |
| `player.name` | `string` | 规范化角色名 |
| `profile.textures` | `ProfileTextures` | 动态调整游戏资料返回的皮肤/披风哈希 |
| `score.checkinAmount` | `number` | 动态调整每日签到积分奖励 |
| `texture.name` | `string` | 规范化材质名称 |
| `user.badges` | `UserBadge[]` | 颁发或定制用户徽章（如正版认证、赞助徽章） |
| `yggdrasil.profileProperties` | `YggProperty[]` | 向 Yggdrasil Profile 附加自定义属性 |

### 3. 领域事件 Event（19 个，事务提交后经由 Outbox 队列至少一次投递）

`closet.added` · `closet.removed` · `player.created` · `player.deleted` · `player.renamed` · `player.updated` · `report.resolved` · `report.submitted` · `score.changed` · `settings.changed` · `texture.deleted` · `texture.updated` · `texture.uploaded` · `user.banned` · `user.deleted` · `user.loggedIn` · `user.registered` · `user.updated` · `yggdrasil.joined`

此外支持跨插件自定义事件：`plugin:<plugin_id>.<event_name>`。

---

## 三、 前端扩展点目录

### 1. 20 个标准 UI 插槽（`slots`）

| 区域 | 插槽名称 |
| --- | --- |
| **全站外壳** | `site.header.actions` · `site.footer` · `site.banner` |
| **登录与注册** | `auth.login.below` · `auth.login.providers` · `auth.register.below` · `auth.register.providers` |
| **用户中心** | `user.dashboard.widgets` · `user.profile.sections` · `user.player.actions` · `user.closet.item.actions` |
| **皮肤库** | `skinlib.card.badges` · `skinlib.detail.sidebar` · `skinlib.detail.below` · `skinlib.upload.below` |
| **管理后台** | `admin.dashboard.widgets` · `admin.user.actions` · `admin.user.tabs` · `admin.texture.actions` · `admin.status.cards` |

### 2. 5 个声明式表单扩展点（`forms`）

`auth.register` · `auth.login` · `user.profile` · `texture.upload` · `player.create`

### 3. 7 个 Level ⑤ 主题覆盖目标（`theme.overrides`）

`site.header` · `site.footer` · `site.logo` · `auth.background` · `home.hero` · `texture.card` · `player.card`

### 4. 共享运行时模块（零重复打包）

插件 `web/index.js` 可直接导入以下模块，由宿主在运行时通过 `globalThis.__SKINLOOM_SHARED__` 注入同一份单例：

`react` · `react/jsx-runtime` · `react-dom` · `react-dom/client` · `@tanstack/react-query` · `@skinloom/ui` · `@skinloom/plugin-sdk/web`

---

## 四、 `.slp` 包限制与官方市场分发规范

| 维度 | Tier 1 (`runtime: "js"` / `theme`) | Tier 2 (`runtime: "native"`) |
| --- | --- | --- |
| 归档体积上限 | `20 MiB`（`server.js <= 5 MiB`） | 单平台分包 `50 MiB` / 全平台胖包 `150 MiB`（单二进制 `<= 64 MiB`） |
| 签名与完整性 | `checksums.txt`（SHA-256）+ `signature`（Ed25519） | 生产环境强制要求官方公钥或超管 `plugins.trusted_keys` Ed25519 验签 |
| 官方上架仓库 | [`Entropy-Increase-Team/skinloom-registry`](https://github.com/Entropy-Increase-Team/skinloom-registry)（纯源码 Commit SHA 登记） | 同左；合并后由官方受保护 Runner 统一 `CGO_ENABLED=0` 交叉编译 5 平台分包并签名 |
