# 0044 交互式安装向导与 Pre-Core 启动模式

- 状态：已接受（2026-11-18）
- 相关：设计文档 `08-setup-wizard-plan.md`；ADR 0006（热加载）、0012（账号、会话与首次安装）、0032（备份与恢复）、0035/0036（Blessing Skin 导入器）

## 背景

ADR 0012 采用先启动全部基础设施（`OpenCore` 写入默认 SQLite、`config.toml`、`secrets/` 并执行迁移）再展示创建管理员的表单模式，带来几个工程问题：
1. 未配置外部 MySQL/PostgreSQL/S3 前就已经在本地写入了空白 `skinloom.db` 与 `config.toml`，切换数据库需要停机、删文件、手写 `config.toml` 后重启。
2. 已有实例遭遇数据库短暂故障时，若被误判为需要初始化会极其危险，需要严格区分“全新未安装”与“已安装实例数据库故障”。
3. 从 Blessing Skin 迁移、从备份恢复仍然依赖命令行工具与 SSH 权限，无法通过浏览器图形界面完成连通性验证、Dry-Run 预览与迁移切换。

## 决策

### Pre-Core Setup Mode 与进程内热切换

- 进程启动依据 `config.toml`、`data/secrets/setup.pending`、`data/secrets/setup.done`、`database` 连通性与 `system_state['setup.completed']` 判定模式：
  - **全新未初始化**：不写 `config.toml`、不创建 SQLite 库、不执行迁移，直接以 **Pre-Core Setup Mode** 启动最小 API 路由器（仅开放安装接口、`/api/v1/site/bootstrap`、`needsSetup: true` 的 `/events` 与 `/healthz`、`/readyz`），非安装路由返回 503。
  - **已安装实例（`setup.done` / 非空库 / 环境变量锁定可用）**：数据库连不通时 **fail-fast 退出**，绝不退回 Setup Mode。`--role=api` / `--role=worker` 无本地初始化权同样不进 Setup Mode。
- 配置落盘与热切换：安装完成（全新配置、连接已有库、备份恢复或 BS 迁移）后原子写入 `config.toml`（`0600`、`fsperm.Private`，跳过由环境变量覆盖的字段）、清除 `setup.token` 与 `setup.pending`、写入 `setup.done`、打开完整 `Core` 并将 `gateway` 内部的 API `http.Handler` 通过 `atomic.Pointer` 原子替换为 Running Mode 路由器，同时经共享 `pubsub.Broker` 发布 `config.changed` 刷新浏览器。

### 文件化安装令牌与两步短期会话

- 一次性安装令牌（256-bit 随机熵）的 SHA-256 哈希保存在 `<data>/secrets/setup.token`，最多保留最近 5 条未过期令牌（24 小时 TTL）。`skinloom setup-link` 独立于数据库直接读取/生成该文件。
- 访问 `/setup?token=...` 时前端立即通过 `history.replaceState` 抹除查询参数，并调用 `POST /api/v1/setup/session` 将令牌换取绑定的短期 HttpOnly Cookie（`skinloom_setup`，Path `/api/v1/setup`，SameSite=Lax），其余向导读写、探活与分片上传接口均校验该 Cookie 与 `X-SkinLoom-Request` CSRF 头。

### 向导分支、安全与大数据处理

- **基础设施配置与探测**：`POST /api/v1/setup/test-db` 与 `test-storage` 以只读/临时探测前缀验证连接而不污染表结构。若探测到 `system_state['setup.completed']` 则提供“直接接入已有实例”；若检测到 `bs_users` / `bs_players` / `bs_options` 则阻止直接覆盖并引导切换到 Blessing Skin 迁移。
- **分片上传与压缩包防护**：`/api/v1/setup/uploads` 与 raw `PUT /uploads/{id}/chunks` 支持 8 MiB 分片传输（默认上限 1 GiB），解压严格校验 Zip-Slip、软硬链接、异常成员体积与解压倍率（Zip-Bomb 防护）；超过 1 GiB 引导使用服务器本地目录或远程桥接插件。
- **Blessing Skin 迁移与仅元数据后补扫**：支持解析上传 `.env`（密钥仅存后端内存不回显，检测容器内 localhost 风险）、直连或上传 `textures` 或“仅迁移数据库”，配套 `POST /api/v1/admin/textures/rescan` 与 `skinloom textures rescan` 对缺失后补传的材质做 SHA-256 与 PNG 尺寸校准（移除 `blobs.{size,width,height}` 与 `textures.size` 的 `Immutable` 约束，迁移零变更）。
- **Blessing Skin 只读桥接插件与面向 v2 插件化的演进**：Blessing Skin 端伴侣插件 `blessing-skin-export-bridge` 维护在独立仓库（不混入 SkinLoom 本体仓库），遵循 BS v6 插件规范，采用仅超级管理员可签发、SHA-256 哈希保存、默认短 TTL 过期、可选 IP/CIDR 白名单与写入冻结中间件以及防缓存头（`Cache-Control: private, no-store`）；SkinLoom 端 `BridgeClient` / `BridgeSourceReader` / `BridgeFileFetcher` 对非私有地址强制 TLS 1.2+，带幂等退避重试、截断与 `X-Texture-Sha256` 校验，并在 `manifest`（`protocolVersion: 1`、`capabilities`、`extensions`）与 `bs.sourceData.Extensions` 预留扩展点，以便后续在 SkinLoom v2 插件体系成熟后平滑演进为 v2 迁移插件并承接 BS 社区插件扩展数据。

## 备选方案

- **每次保存数据库配置后退出进程由守护进程重启**：容器环境下单次退出容易触发编排器误判 CrashLoopBackOff 或丢失实时进度展示，进程内原子热切换路由体验连续且无需外部重启策略。
- **在 Setup Mode 强制依赖 SQLite 存放状态**：违反冷启动磁盘零污染原则且增加后续库切换复杂度，文件化的 `setup.token` 与内存作业状态更轻且可清理。
