# 0049 双轨插件架构、双向 gRPC 进程引擎与隔离关系数据库

- 状态：已接受（2026-11-19）
- 相关：设计文档 `10-dual-tier-plugin-and-market-plan.md` §1、§2.1–§2.5；ADR 0045（`jsrt` 沙箱与两阶段激活）、0048（`.slp` 签名与多副本同步）

## 背景

Tier 1 轻量沙箱插件（QuickJS / `wazero` WASM）提供了零 CGO、毫秒级冷启动与严格内存/时间配额的跨平台执行环境，适用于大多数认证守卫、积分签到、Webhook 与管理面板扩展。然而在深度系统级扩展场景下，纯 WASM JS 沙箱存在固有边界：

1. **复杂关系查询与存量数据库迁移**：如从 Blessing Skin Server 导入第三方插件表、跨多张自定义业务表执行事务与索引关联查询，仅靠 `plugin_kv` 与 `plugin_docs` JSON 文档桶难以满足。
2. **高吞吐流式网络与长连接**：WebSocket 实时推送、Server-Sent Events（SSE）、大体积流式导入导出需要标准 HTTP 流或原生 Socket，不适合在单次请求限时内存缓冲的 JS 沙箱中承载。
3. **重计算与可替换基础设施驱动**：自定义对象存储（WebDAV / OSS / COS）、全文检索引擎（Meilisearch / Elasticsearch）、服务端 GPU/CPU 3D 渲染加速等需要 Go 原生并发与生态库支持。

若采用 Go 标准库 `plugin`（动态 `.so`）会导致主进程崩溃连带、无法跨平台编译且无法热卸载；若要求站点通过 Docker Compose 额外部署 Sidecar 容器则会破坏单二进制开箱即用体验。

## 决策

### 1. 统一 `PluginDriver` 抽象与双轨运行时（`internal/plugin/driver*.go`）

- 将宿主插件执行层抽象为统一的 `Driver` 接口（`Setup`、`InvokeGuard`、`InvokeFilter`、`InvokeEvent`、`InvokeHTTP`、`InvokeCron`、`InvokeService`、`InvokeExtension`、`NotifySettingsChanged`、`InvokeLifecycle`、`ProxyHandler`、`Stats`、`NativeStats`、`Close`）。
- **Tier 1（`runtime: "js"`，默认）**：由 `jsrtDriver` 实现，保持 QuickJS/WASM 沙箱的全部既有行为与 100% 向后兼容。
- **Tier 2（`runtime: "native"`）**：由 `nativeDriver` 基于 `hashicorp/go-plugin` + 双向 gRPC（`api/proto/plugin/v1/plugin.proto`）实现，插件以受控独立子进程运行：
  - **控制面（Host → Plugin `PluginService`）**：负责生命周期初始化、Hook/Cron/Service/Extension 调用、健康探活（`HealthCheck`）与底层 `StorageService` / `SearchService` / `RenderService` RPC。
  - **回调面（Plugin → Host `HostService`，复用 `GRPCBroker`）**：完整映射全部 25 个宿主 `ctx.*` 能力，且**全部复用与 JS 插件完全一致的 `PermissionSet.Check` 权限守卫**。
  - **数据面（可选本地 HTTP/WS 流式反代）**：当插件声明 `native.httpProxy: true` 并获授 `http.stream` 权限时，子进程监听本地回环端口或 Unix Domain Socket 并返回一次性 256-bit `proxy_token`；宿主先完成会话认证、CSRF/CSWSH 防护、路由级鉴权后，剥离原始 Cookie 并注入可信内部头（`X-SkinLoom-Proxy-Token`、`X-SkinLoom-User-*`），通过 `httputil.ReverseProxy` 原生透明转发 SSE、WebSocket Upgrade 与大文件流。
- **故障隔离与自愈**：`nativeDriver` 内置指数退避崩溃重启（`1s → 2s → 5s → 15s → 30s`）与连续失败熔断器；子进程崩溃或被强杀时主站零停机，超过阈值自动停用插件并向全体超级管理员发送站内告警。

### 2. 三方言隔离关系数据库（`internal/plugin/db_allocator.go` + `sdk/go/plugindb`）

- 申请并获授 `storage.sql` 权限的原生插件在 `InitRequest` 中获得由宿主 `DBAllocator` 分配的隔离数据库配置，强制分配不可伪造的表前缀 `plg_<safe_id>_`（其中 `<safe_id>` 将插件 ID 中的 `-` 替换为 `_` 并通过 `^plg_[a-z0-9_]{3,40}_$` 校验）：
  - **SQLite**：使用完全独立的数据库文件 `<DataDir>/plugins-data/<id>/plugin.db`（WAL 模式），物理杜绝跨表访问宿主核心表（如 `users`、`sessions`），并自动纳入站点热备份归档（`internal/backup`）与 `Uninstall(..., purge=true)` 清理。
  - **MySQL / PostgreSQL**：自动确保 `<main_db>_plugins` 隔离库或 `plg_<safe_id>` Schema 存在，配合官方 Go SDK 的 `plugindb.Open`（提供 `Table(name)` 与 `Migrate(ctx, migrations)`），强制所有建表与迁移语句落在 `plg_<safe_id>_*` 命名空间内。

### 3. 可热替换基础设施 Provider 钩子（`storage` / `texture` / `render`）

- 在 `storage.SwappableStore`、`texture.Service`（`SearchProvider`）与 `render.Service`（`RenderProvider`）中引入 `atomic.Pointer` 线程安全热替换包装器：
  - 当获授 `provider.storage` / `provider.search` / `provider.render` 权限的原生插件启用时，原子切换对应底层实现；
  - 当插件停用、卸载或进程熔断时，零锁争用自动回退至宿主内置 Local/S3 存储、数据库 LIKE 搜索与纯 Go 渲染实现。

## 备选方案

- **Go 标准库 `plugin`（`.so` 动态链接库）**：不支持 Windows、要求宿主与插件 Go 编译器版本及全部依赖完全字节级一致、无法卸载内存、且插件 panic/segfault 会直接杀死主进程，故排除。
- **仅提供宿主 gRPC SQL 代理而不发放直连 DSN**：会让插件无法使用标准 `database/sql` 事务、预编译语句与主流 Go ORM/迁移库；通过独立 SQLite 文件 / 独立库 Schema + `plugindb` 强制表前缀既保留了完整 SQL 生态，又实现了强数据隔离。
