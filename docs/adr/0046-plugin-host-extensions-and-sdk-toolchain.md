# 0046 插件全量宿主扩展点、单源类型生成与 SDK/CLI 工具链

- 状态：已接受（2026-11-18）
- 相关：设计文档 `09-plugin-platform-plan.md` §3.1、`05-plugin-theme-standard.md`；ADR 0005（TS+WASM 插件标准）、0045（存储槽位与 `jsrt` 沙箱池）

## 背景

P2-1 已建立 QuickJS-NG/WASM 沙箱池、三方言存储表与核心 `ctx` 能力（`hooks`/`events`/`http`/`kv`/`db`/`files`/`fetch`/`settings`/`site`/`crypto`/`i18n`/`log`/`cache`）。要让第三方开发者能端到端编写认证、验证码、CDN 刷新、Yggdrasil 扩展、定时任务与前端 UI 插件并获得静态类型提示与无配置打包签名，仍需解决四个架构问题：

1. **宿主核心模块双向扩展而不破坏依赖方向**：认证（外部登录提供方与遗留密码校验器）、验证码、CDN 缓存清理与 Yggdrasil `/api/yggdrasil` 元信息属于领域模块，插件运行时卸载或热重载时必须原子清理其注册的回调，不能留下悬空处理器。
2. **多副本部署下定时任务去重**：水平扩展部署多台无状态实例时，同一插件的 `ctx.cron.schedule` 不能在每个副本上重复触发。
3. **前后端契约零漂移**：Manifest 校验字段、23 项权限、32+ 个 Hook、20 个前端 `<Slot>` 及 7 个 Level 5 主题覆盖目标必须从唯一权威源推导。
4. **前端共享依赖与沙箱安全边界**：插件前端 UI 不能各自打包一份 `react`/`@skinloom/ui`/`@tanstack/react-query`，后端 `server.js` 构建阶段即应阻止误引入 Node 内置模块。

## 决策

### 插件归属注册表与动态设置枚举

- 各宿主领域模块（`account`、`captcha`、`cdn`、`yggdrasil`）对外暴露按 `pluginID` 索引的线程安全扩展注册表；`settings.Registry` 增加 `DynamicEnum` 生成函数，在 `GET /api/v1/admin/settings` 时合并内置与插件注册的 CAPTCHA / CDN 提供方选项。
- `plugin.Manager` 在原子激活运行时时通过 `registerHostExtensions` 登记插件贡献的提供方，并在卸任、崩溃停用或下一次热加载替换旧运行时时执行 `unregisterHostExtensions(pluginID)` 精确回收。

### 基于 `plugin_kv` 的集群唯一 Cron 租约

- `ctx.cron.schedule` 支持标准 5 段 Cron 与 `@hourly` / `@daily` 等宏表达式；每个调度器仅在获取 `plugin_kv` 中键 `__cron:<pluginID>:<job>` 对应的分布式租约（带周期窗口到期时间与随机实例所有者标识）成功后才执行 JS 处理器，避免多副本重复执行。

### `plugingen` 单源派生与 `-check` 门禁

- 将 UI `<Slot>`、Level 5 `ThemeOverrideTargets`、扩展 `FormScopes` 与 `PluginSettingTypes` 收口至 `internal/plugin/slots.go`，与 `internal/hook/catalog.go`、权限表共同构成权威源。
- `go generate ./internal/plugin/...` 运行 `internal/tools/plugingen` 确定性生成 `website/static/schema/plugin.v1.json` 与 `web/packages/plugin-sdk/src/gen/catalog.ts`，并以 `go run ./internal/tools/plugingen -check` 作为 CI 门禁阻止手改或漂移。

### `@skinloom/plugin-sdk`、`@skinloom/cli` 与 `create-skinloom-plugin`

- `@skinloom/plugin-sdk` 提供 `./server`（强类型 `PluginContext`、`definePlugin`、`reject`）、`./web`（`defineWebPlugin`、`usePlugin*`、`usePluginApiQuery`/`Mutation`）、`./testing`（基于内存 KV/Doc/Route/Hook 模拟宿主的 `createTestHost`）及 `./catalog`。
- `@skinloom/cli`（`skinloom-plugin`）提供 `build` / `dev` / `lint` / `pack` / `sign` / `verify` / `keygen`：`server.js` 使用 esbuild (`neutral`/`es2023`) 并阻断 `node:*`/`bun:*`/UI 库；`web/index.js` 将共享前端依赖重映射为 `globalThis.__SKINLOOM_SHARED__`；`.slp` 包附带确定性 SHA-256 `checksums.txt` 与可选 Ed25519 `signature`；`dev` 命令监听源码变更并推送至超管专用的 `POST /api/v1/admin/plugins/dev-reload`。
- `create-skinloom-plugin` 提供 `basic`、`page`、`auth-provider`、`block` 与 `theme`（Level 5 `overrides`）五个模板，开箱通过 `lint` 与 `build`。

## 备选方案

- **独立代码生成器与手写 `.d.ts`**：双写极易在新增 Hook 或 `<Slot>` 时失步，因此统一由 `plugingen` 自动生成并加 `-check`。
- **要求每个插件自带独立进程 / Redis 锁运行 Cron**：违背单二进制与轻量部署原则，且 SkinLoom 已有全方言事务表，复用 `plugin_kv` 租约无需引入额外基础设施。
