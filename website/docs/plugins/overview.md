---
title: 插件与主题
sidebar_position: 1
---

# 插件与主题

SkinLoom 提供统一的双层插件与主题平台（`apiVersion: 1`，ADR 0045–0050）。插件与主题共用同一套 `plugin.json` 清单标准、`.slp`（Ed25519 签名 + SHA-256 校验）单包分发格式与[官方签名市场（`skinloom-registry`）](https://github.com/Entropy-Increase-Team/skinloom-registry)。完整设计规范见[设计文档 05](/design/05-plugin-theme-standard)与[设计文档 10](/design/10-dual-tier-plugin-and-market-plan)。

## 双层插件运行时

SkinLoom 按扩展的复杂度与资源需求提供两层运行时，在 `plugin.json` 中通过 `"runtime"` 声明：

| 层级 | `runtime` | 适用场景 | 运行方式与隔离边界 |
|---|---|---|---|
| **Tier 1 · 轻量沙箱插件** | `"js"`（默认） | 绝大多数业务逻辑扩展：签到/积分规则、注册拦截、通知推送、Webhook、轻量第三方认证与 UI 插槽扩展 | 运行在宿主进程内的 QuickJS/WASM 沙箱实例池（`internal/jsrt`），零外部依赖、单文件跨平台分发；强制指令燃料上限、内存配额、SSRF 私网阻断与权限门控，使用宿主提供的 KV + 4 槽位索引集合（`storage`）与私有文件目录（`files`） |
| **Tier 2 · 原生高性能插件** | `"native"` | 重度或高性能扩展：复杂关系型数据库建模、大文件流式传输/SSE/WebSocket、图像处理、CGO/原生库调用、高并发外部连接池 | 作为独立 OS 子进程运行（随 `.slp` 打包 `bin/<executable>_<os>_<arch>` 多平台二进制），通过本地双向 gRPC（`api/proto/plugin/v1/plugin.proto`）与宿主通信；支持心跳探活、指数退避重启与连续崩溃自动熔断；支持本地流式 HTTP/WebSocket 反向代理（`native.httpProxy: true`），并可申请宿主隔离 SQL 数据库空间（`native.database.required: true` + `storage.sql` 权限） |

> **主题（`"kind": "theme"`）**：纯声明式主题（Level ①–④）只需 `plugin.json` 与 `theme.css`；带前端组件覆盖（Level ⑤，如自定义顶栏、页脚、材质卡片、首页 Hero）的主题可附加 `web/index.js`，详见[开发主题](./themes.md)。

---

## 快速开始：用脚手架创建插件或主题

使用官方脚手架 `create-skinloom-plugin` 一键生成含类型检查、测试与打包脚本的工程，内置 6 套模板（`basic`、`page`、`auth-provider`、`block`、`theme`、`native-go`）：

```bash
# 创建 Tier 1 TypeScript 插件（可选 basic / page / auth-provider / block / theme / native-go）
bunx create-skinloom-plugin my-plugin --template basic
cd my-plugin

# 校验清单、构建并运行单元测试
bun run lint
bun run build
bun run test

# 生成 Ed25519 密钥对并打包签名生成 .slp 扩展包
bunx skinloom-plugin keygen --out ./keys
bunx skinloom-plugin pack --key ./keys/plugin.key
```

若选择 `--template native-go`，脚手架会同时生成基于 `sdk/go` 的 Go 服务端源码（`cmd/plugin/main.go`）、`plugintest` 单元测试与前端扩展入口，运行 `bunx skinloom-plugin build --targets linux/amd64,linux/arm64,windows/amd64,darwin/arm64,darwin/amd64` 即可一键交叉编译多架构二进制并打入单个 `.slp` 包。

---

## 扩展能力一览

### 后端能力（Tier 1 `@skinloom/plugin-sdk/server` & Tier 2 `sdk/go`）

- **三类扩展 Hook（Guard / Filter / Event）**：支持同步前置拦截 Guard（如 `auth.register`、`texture.upload`、`player.create`、`closet.add`、`user.sign`）、同步数值改写 Filter（如 `player.name`、`user.badges`、`yggdrasil.profile`）以及 15 个异步领域事件 Event（如 `user.registered`、`player.updated`、`texture.uploaded`、`report.created` 等），完整清单由 `internal/hook` 单源生成。
- **命名空间 HTTP 路由**：在 `/api/plugins/<plugin_id>/*` 下暴露自定义 REST 接口，拥有 `routes.public` 权限的插件还可挂载站点根路径路由；Tier 2 声明 `native.httpProxy: true` + `http.stream` 权限后，支持通过本地 Unix Socket / Loopback 透明代理 WebSocket、SSE 与大文件流式传输。
- **宿主机能力调用（Host API）**：按 `permissions` 声明授予，支持查询/修改用户、增减积分、管理角色与材质、读写插件专属 KV（`storage.kv`）、4 槽位索引文档集合（`storage.docs`）与私有文件目录（`storage.files`）、发送站内通知与邮件、发起受 SSRF 保护的外部 HTTP 请求（`http.fetch` + `http.allow` 域名白名单）、写入审计日志以及跨插件服务调用（`services`）。
- **插件级隔离 SQL 数据库（Tier 2 专属）**：在 `plugin.json` 中声明 `"native": { "database": { "required": true } }` 与 `"storage.sql"` 权限后，宿主按当前数据库驱动自动分配隔离空间与 `plg_<safe_id>_` 规范表前缀（SQLite 为 `<数据目录>/plugins-data/<id>/plugin.db` 独立 WAL 库；MySQL/PostgreSQL 支持 `[plugins.database]` 独立配置或主库隔离前缀/Schema），可通过 `sdk/go/plugindb` 在启动时自动执行迁移。
- **可插拔底层提供方**：注册自定义第三方登录（`auth.provider`）、外部密码校验器（`auth.password`）、验证码（`captcha.provider`）、CDN 刷新器（`cdn.provider`）、存储驱动（`provider.storage`，Native）、全文搜索驱动（`provider.search`，Native）、渲染引擎驱动（`provider.render`，Native）与 Cron 定时任务（`cron`）。

### 前端能力（`@skinloom/plugin-sdk/web`）

- **20 个标准 UI 插槽（Slots）**：如 `auth.login.below-form`、`auth.register.extra-fields`、`user.dashboard.widgets`、`user.closet.toolbar`、`user.players.card-actions`、`skinlib.toolbar.filters`、`skinlib.detail.sidebar`、`admin.dashboard.cards`、`admin.users.row-actions` 等，所有插槽均包裹在独立的 React `ErrorBoundary` 中，单个插件渲染异常不会影响宿主页面。
- **插件独立路由页面**：公开页 `/p/<id>/*`、用户中心页 `/user/p/<id>/*`、管理后台页 `/admin/p/<id>/*`，并可通过 `menus.{public,user,admin}` 挂载到导航栏或侧栏「扩展」分组。
- **自定义首页区块（Blocks）**：通过 `defineBlock` 注册可视化首页区块，默认使用 Shadow DOM 隔离样式，站长可在「后台 → 外观 → 首页」直接拖拽编排。
- **Level ⑤ 主题核心组件覆盖**：`kind: "theme"` 的主题可通过 `overrides` 接管 `site.header`、`site.footer`、`texture.card`、`home.hero` 等核心区域，详见[开发主题](./themes.md)。

---

## 权限分级与安全防线

插件权限分为三个风险等级，安装与升级 `.slp` 包时管理后台会显著展示权限审查清单（新增敏感/高危权限时必须重新确认）：

1. **普通权限（`normal`）**：`storage.kv`、`storage.docs`、`storage.files`、`cron`、`admin.ui`、`users.read`、`players.read`、`textures.read`、`notify.send`。
2. **敏感权限（`sensitive`）**：`http.fetch`（必须声明 `http.allow` 域名白名单，宿主底层强制拦截私网/环回/云元数据地址）、`mail.send`、`players.write`、`textures.write`、`closet.write`、`score.write`、`users.write`、`routes.public`、`yggdrasil.extend`、`cdn.provider`、`captcha.provider`，以及 Tier 2 专属的 `storage.sql`、`http.stream`、`provider.search`、`provider.render`。
3. **高危权限（`high_risk`，需超级管理员二次高危确认）**：`users.ban`、`auth.provider`、`auth.password`，以及 Tier 2 专属的 `provider.storage`、`native.exec`（所有 `runtime: "native"` 插件自动附带，生产模式强制要求官方或可信公钥 Ed25519 签名）。

---

## 安装、管理与官方插件市场

站长可以通过**管理后台（`/admin/plugins`）**或**命令行（`skinloom plugin`）**管理插件与主题：

- **从官方市场浏览与一键安装**：后台「插件与主题 → 插件市场」默认接入[官方静态签名市场 `skinloom-registry`](https://github.com/Entropy-Increase-Team/skinloom-registry)（内置官方 Ed25519 公钥强校验 + GitHub/jsDelivr 双源自动容灾），自动按当前宿主的 `apiVersion`、版本号、操作系统架构（`os/arch`）、数据库类型与运行角色进行五维兼容性过滤，并支持依赖拓扑自动级联安装。
- **恶意包自动巡检熔断**：宿主每 6 小时及刷新市场索引时自动核对签名市场的 `blocked` 吊销黑名单；命中黑名单的已启用插件会立即被热禁用、记入审计日志并向所有超级管理员发送站内安全通知。
- **命令行常用操作**：

```bash
# 查看已安装的插件与主题
skinloom plugin list

# 在官方/已配置市场中搜索扩展并直接安装或升级
skinloom plugin search <关键词>
skinloom plugin market-install <id> [@版本] [--trust-high-risk]
skinloom plugin upgrade [<id> | --all]

# 从本地 .slp 包或目录安装、启用、禁用、卸载、热重载
skinloom plugin install ./my-plugin-1.0.0.slp
skinloom plugin enable <id>
skinloom plugin disable <id>
skinloom plugin remove <id>
skinloom plugin reload
```

---

## 发布到官方市场

官方插件与主题市场采用**纯源码登记（Source-Only）+ 官方受控编译签名**的 GitOps 流程维护：

1. 在你的开源仓库中确保 `skinloom-plugin lint` 与 `skinloom-plugin build`（原生插件含 `CGO_ENABLED=0` 交叉编译与 `go test ./...`）全部通过，并推送代码拿到不可变的 40 位 Git Commit SHA。
2. Fork [Entropy-Increase-Team/skinloom-registry](https://github.com/Entropy-Increase-Team/skinloom-registry)，在 `manifests/<你的插件id>.yaml` 中登记仓库 HTTPS 地址、版本号、固定 Commit SHA 与权限列表（**不要**上传预编译 `.slp` 包）。
3. 运行 `node scripts/validate-manifests.mjs` 本地自检通过后提交 Pull Request；CI 会检出对应 Commit SHA 校验 `plugin.json` 一致性并按权限分级运行静态扫描 / CodeQL / `govulncheck`；合并后由受保护的官方 Runner 统一交叉编译 5 平台分包并使用官方 Ed25519 私钥签名发布。

---

## 从 Blessing Skin 迁移插件

BS 插件是 PHP，无法直接运行。常用的官方插件（Yggdrasil 外置登录、GitHub/Microsoft/OIDC 第三方登录、S3 存储、CDN 刷新、批量导入、图形/Turnstile/reCAPTCHA/hCaptcha 验证码等）SkinLoom 已经原生内置。[设计文档 05 §4.4](/design/05-plugin-theme-standard) 给出了 BS 过滤器、事件与 SkinLoom Hook/Filter 的完整对照表。
