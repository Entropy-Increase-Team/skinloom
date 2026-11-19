# 0048 `.slp` 扩展包签名校验、热升级与多副本聚类同步

- 状态：已接受（2026-11-18）
- 相关：设计文档 `09-plugin-platform-plan.md` §0（决策 3、4、6）、§3.3；`05-plugin-theme-standard.md` §4；ADR 0029（只写敏感设置保护）、0045（`jsrt` 沙箱与两阶段激活）

## 背景

P2-1 至 P2-3 已建成插件与主题的前后端运行时、CLI 开发打包和签名命令。要将该能力安全交付给生产环境与多副本集群站点，还必须解决：

1. **安全解包与密码学来源校验**：站点管理员上传任意 `.slp` 归档时，必须防御 Zip-Slip、路径穿越、符号链接、超限单文件、`server.js` 膨胀以及解压放大（Zip Bomb），并在执行前验证所有文件的 SHA-256 与 Ed25519 签名；未签名或未知公钥签名只能在明确的“开发者确认风险”路径下由超级管理员显式安装。
2. **多节点共享存储与防孤儿清理**：各副本的容器本地磁盘不是共享文件系统，CLI 或某一节点安装的 `.slp` 必须持久化至宿主 `storage.Store`（Local 或 S3 兼容对象存储），且不能被纹理孤儿清扫任务误删。
3. **升级不停机排空与权限最小化**：插件版本热升级时须让在途 JS 请求与请求连接优雅排空，保留插件已有配置、KV、文档和私有文件，并在升级新增权限时向超级管理员高亮 diff 与分级确认；同时保证 `secret` 类插件配置永不回显且保留未改动的既有密钥。

## 决策

### 1. `.slp` 包解析与 Ed25519 验签（`internal/plugin/package.go`）

- 限定归档体积与累计解压字节上限均为 **20 MiB**、单文件 **10 MiB**、最多 **4096** 个条目、`server.js` 束上限 **5 MiB**，通过 `io.LimitReader` 读取真实解压流防伪造 ZIP 头触发 Zip Bomb，并拒绝任何绝对路径、反斜杠、`.`/`..` 路径段与符号链接。
- 强制存在并逐一校对 `checksums.txt`（排序 `<64-hex>  <path>` 格式，与 `@skinloom/cli pack` 完全一致），任何篡改或增删均在解析期直接失败（`ErrBadPackage`）。
- 支持内置官方 Ed25519 公钥，以及超级管理员通过 `plugins.trusted_keys`（SPKI PEM / Base64 / 64 位 Hex）或静态注入追加的自托管信任公钥；针对 `checksums.txt` 原文字节验证 `signature`。缺少签名或公钥不在信任列表时不阻止预检（返回明确风险告警），但除非调用方显式传 `AllowUnsigned: true`，否则禁止安装（`ErrUntrustedPackage`）。
- 针对 `kind == "theme"` 的 `.slp` 在预检和安装阶段复用 `theme.Check` 直接在内存 `fstest.MapFS` 验证样式、Token 与资源声明，并将通过超级管理员确认安装后的主题扩展包自动纳入 `HasTrustedThemeWeb` 信任链。

### 2. 内容寻址存储保护与多副本热同步（`packages_mgr.go` + `upkeep.go`）

- 为遵守 `storage.Store` 64 位小写十六进制 SHA-256 键规范，`.slp` 安装时直接以其归档完整 SHA-256（即数据库中 `plugins.package_sha256`）写入 `Store`，在升级更换版本或卸载插件且无其他记录引用时才删除旧归档对象。
- 在 `texture.SweepFile` 与 `texture.SweepOrphans` 中联表过滤 `plugins.package_sha256`，彻底避免对象存储清扫任务误删插件包；同时天然兼容备份恢复流程。
- `Manager` 与 `app.OpenCoreWithOptions` 注入 `Store` 和 `Themes`；任一节点或 `skinloom plugin` CLI 变更插件版本、启停、配置或卸载时通过 `settings.Touch` 广播全局 `config_version` 递增；其他副本监听 `SetOnReload` 执行 `SyncCluster`，按依赖拓扑顺序拉取缺失 `.slp` 解压至本机 `data/plugins/<id>/<version>`（主题包同步写 `data/themes/<id>` 并刷新 `Themes`）、热更新 `ctx.settings.onChange`、排空旧 `jsrt.Pool`（最长 `30s`）或清理已卸载运行态。

### 3. 管理端 REST API、CLI 与分级权限控制台（`admin_plugins.go` / `cli/plugin.go` / `admin/plugins`）

- 后台接口实行严格 RBAC 分权：**普通管理员**可查看列表/详情/日志、预检 `.slp`、启停、重载与修改配置；仅**超级管理员**（或管理员令牌 / 本机 CLI）可执行 `POST /api/v1/admin/plugins/install`、`DELETE /api/v1/admin/plugins/{id}` 及操作 `plugins.trusted_keys`，关键变更均记入 `audit_logs`。
- `/api/v1/admin/plugins/{id}/settings` 读写全量掩码：读取永不返回 `type == "secret"` 字段明文（仅返回 `isSet` 状态），写入遇到 `"********"` 哨兵或省略字段时保留数据库原有密钥，收到 `null` 才清除。
- 后台提供 `/admin/plugins` 管理页面：支持拖拽上传预检、官方/未验签告警、三档权限与升级新增标记核对（含高危与未验签时手动输入插件 ID 二次确认）、覆盖 14 种字段类型的动态配置抽屉，以及展示 WASM 实例池指标、持久化用量、`ctx.log` 最近日志与路由表的详情抽屉；同步提供 `skinloom plugin {list,install,enable,disable,remove,reload}` 子命令。

## 备选方案

- **在 `plugins` 表新增大型 `BLOB`/`BYTEA` 列存放 `.slp` 字节**：会显著膨胀关系型数据库体积并影响热备份/主从复制；复用现成内容寻址 `storage.Store`（Local/S3）配合 `plugins.package_sha256` 与已有 `SweepOrphans` 防护，读写分离且扩展性更佳。
- **使用路径前缀 Key 如 `plugins/<id>/<ver>.slp` 存入 `storage.Store`**：需改动 `storage.ValidKey` 及 S3/Local 安全路径约束并可能引入备份清单兼容问题；保持纯粹的 64 位 SHA-256 键最简单稳健。
