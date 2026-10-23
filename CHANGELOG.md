# 更新日志 / Changelog

本文件记录所有对用户、站长与插件开发者可见的变化。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.1 发布后由 release-please 根据 Conventional Commits 自动维护；人写的发布说明见文档站"发布说明"。

## [Unreleased]

### 新增（M1 账号 · 第一部分）
- **首次安装向导**：首次启动时控制台打印一次性安装链接（`/setup?token=…`，24 小时有效，`--open` 直接打开）。在向导里设置站点名称、站点地址并创建超级管理员，完成后自动登录、链接作废。`skinloom setup-link` 可随时生成新链接（容器内同样可用）。
- **登录与会话**：`/auth/login` 页面与 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`；"记住我"保持 30 天，否则关闭浏览器即失效。页头显示当前用户与退出按钮。
- 管理 API 接受管理员会话（`admin.token` 保留给脚本使用）；设置修改记录修改人并写审计日志。
- 从 Blessing Skin 导入的密码哈希（BCRYPT、ARGON2I、MD5、SHA256、SHA512 及其 SALTED2* 变体）可以直接登录，首次登录后自动升级为 argon2id。
- 数据库新增 `users`、`sessions`、`auth_tokens`、`audit_logs` 表（迁移 `20261023095021_accounts`）。
- 站点 bootstrap 增加 `needsSetup`，未完成安装时所有页面顶部提示如何找到安装链接。

### 安全（M1 账号 · 第一部分）
- 安装向导只能凭控制台打印的一次性令牌进入，完成后永久关闭（修 Blessing Skin S11）；并发的两次安装只会有一次成功。
- 密码使用 argon2id，并发哈希数受限；长度规则统一为 8–128 个字符（S15）。
- 会话令牌在数据库中只存 SHA-256；Cookie 为 `HttpOnly`、`SameSite=Lax`，HTTPS 下加 `Secure`；被封禁用户的会话立即失效。
- 登录失败一律返回"邮箱或密码不正确"，未注册邮箱与错误密码耗时一致（S05）；失败次数按"客户端地址""地址 + 账号""账号"三层限制，超出返回 429（S01）。单个攻击者无法把真正的用户锁在门外。
- 带会话 Cookie 的写请求必须带 `X-SkinLoom-Request` 头，且 `Origin` 必须是本站（CSRF 防护）；登录后跳转只接受站内路径（防开放重定向）。

### 变更（部署与发布）
- **默认端口改为 17546**（前端内部端口 17547），避开 8080 / 3000 以及常用的四位数端口，也不落在系统临时端口与 Kubernetes NodePort 范围内。已有站点的 `config.toml` 保留原端口。见 ADR 0013。
- Docker Compose 成为推荐部署方式：`minimal`（一体镜像 + SQLite）、`standard`（前后端分离 + MariaDB）、新增 `postgres`；可用 `SKINLOOM_VERSION`、`SKINLOOM_PORT`、`TZ` 调整，拉不到镜像时自动用源码构建。
- 新增 Kubernetes / k3s 清单（Kustomize）：`deploy/kubernetes/all-in-one`（单 Pod + SQLite）与 `deploy/kubernetes/split`（后端 + 可扩容前端 + PostgreSQL），非 root、带探针，k3s 开箱即用。
- 发行包附带后台服务配置 `service/`：systemd 单元（带沙箱选项）、macOS launchd、Windows 计划任务安装脚本。
- 新增 `skinloom healthcheck` 命令；三个镜像都声明了 `HEALTHCHECK`。
- 发布工作流：推送 `v*` 标签直接发布 Release（带 `-` 的为预发布），附仅后端的单文件程序；镜像增加 `latest`（最新发布）与 `edge`（main 分支）标签；Go 改为原生交叉编译，镜像构建更快。
- 文档站通过 GitHub Pages 发布（`docs.yml`），自动适配自定义域名或 `/skinloom/` 子路径。
- README 重写：部署方式一览，Compose、docker run、Kubernetes / k3s、发行包后台常驻、反向代理与 HTTPS、端口说明；文档站新增对应的部署页面。

### 修复（M1 账号审查）
- 32 个汉字的昵称会让安装失败（数据库层按字节计长度，校验按字符）。
- 公开的安装接口在检查令牌之前就计算密码哈希，可被用来消耗 CPU 与内存，安装完成后依旧如此；现在先做廉价检查。
- 任一地址连续 10 次登录失败会把该账号对所有人锁定 15 分钟，攻击者可借此让超级管理员一直无法登录；改为三层计数（见上）。
- 客户端地址超过 45 个字符（带接口名的 IPv6）或超长 User-Agent 会让登录失败。
- 导入的 argon2 哈希最多可要求 1 GiB 内存、255 个线程；上限降为 256 MiB、16 个线程。
- 在同一浏览器再次登录（切换账号）后，旧会话在数据库里仍然有效。
- 安装完成后保存站点名称与地址时，若浏览器已断开会丢失设置。
- 旧密码升级为 argon2id 时在事务内计算哈希，延长了事务时间。
- 安装向导里的字段错误改为当前语言显示。
- 列表型配置（如 `trusted_proxies`）无法通过环境变量设置：现在用逗号分隔。

### 变更（2026-10-23 晚）
- 前端包管理器改为 **bun**（仓库只提交 `bun.lock`）；没有 bun 时可用 pnpm 或 npm 安装和运行全部脚本。`skinloom up --dev` 按 bun → pnpm → npm 自动选择，新增热加载配置 `web.package_manager`。见 ADR 0011。
- Node.js 统一为 24：发行包内置 Node 24.22.1，Docker 镜像基于 `node:24-alpine`，CI 使用 Node 24。
- GitHub Actions 升级到 Node 24 版本（checkout / setup-go / setup-node v7，docker v4–v7，action-gh-release v3）。

### 修复（2026-10-23 晚）
- CI "Generated files are up to date" 失败：迁移生成工具 `migdiff` 没有注册 MySQL / PostgreSQL 的 `database/sql` 驱动，漂移检查在这两种数据库上无法运行。

### 新增（M0 地基）
- `skinloom` 命令行：`up`（后端 + 前端 + 网关，单端口）、`serve`、`migrate [status]`、`settings get|set`、`admin token`、`openapi`、`version`。
- 启动器：监管 Next.js 子进程（`web.mode = managed | dev | external | off`），崩溃退避重启，日志合并，Windows 下终止整个进程树。
- 网关：后端保留路径、`/_next/static` 由后端直接提供（`immutable` 缓存）、其余页面反代前端；前端未就绪时显示占位页。
- 数据库：SQLite / MySQL / MariaDB / PostgreSQL；ent 建模、Atlas 生成迁移、goose 执行（带锁），SQLite 迁移前自动快照。
- 运行时设置：类型化定义、校验（逐字段错误）、`null` 重置为默认、配置版本号；跨进程 2 秒内生效。
- 热加载：设置/主题变化经 SSE（`/events`）推送到已打开的页面；配置文件热键即时生效（兼容编辑器原子保存），冷键提示重启。
- Hook 总线：Guard / Filter / Event，按贡献者原子替换，带失败策略与超时。
- 主题：主题包标准（`plugin.json`，`kind: theme`），内置默认主题；token 覆盖（`theme.overrides`）、首页区块编排（`home.blocks`）。
- REST API：`/api/v1/site/bootstrap`、`/api/v1/admin/settings`、`/api/v1/admin/themes`；OpenAPI 3.1（`/api/docs`）；前端客户端由 orval 生成。
- 前端：Next.js 16 App Router，主题 token 注入、明暗模式、中英双语、首页区块（hero、公告、特性、启动器配置）、关于页（鸣谢 Blessing Skin）。
- 发行：`deploy/packaging/package.sh` 构建含 Node 运行时的发行包与启动脚本；Docker（server / web / all-in-one）与 Compose 示例。

### 安全
- 可信代理：`server.trusted_proxies` 生效（此前只写在配置模板里）。只有来自可信代理的 `X-Forwarded-For/-Proto/-Host` 才被采信，客户端 IP 取最右侧的非可信地址；其余来源一律以连接对端为准。支持热加载。
- 网关先规范化路径再路由（`/a/../api`、`//api` 308 跳转到规范路径），无法再绕过路径归属。
- 所有响应默认带 `X-Content-Type-Options`、`Referrer-Policy`、`X-Frame-Options`（不覆盖前端自己设置的值）。
- 主题资源与静态文件只提供普通文件，不再列出目录。
- 首页区块中的链接与背景图只接受站内路径和 http(s) 地址（拒绝 `javascript:`、`data:`、`//host` 等）。
- 发行包打包时按 nodejs.org 官方 `SHASUMS256.txt` 校验下载的 Node 运行时。
- 管理员令牌文件改为原子创建：服务端与命令行同时首次启动时不再生成两个不同的令牌。
- `/events` 同时连接数上限（默认 5000），超出返回 503。
- 文档站构建依赖升级到已修复版本（serialize-javascript、uuid、tinypool、postcss-selector-parser）；`braces` 上游暂无修复版本，仅用于构建期匹配仓库内文件，记录为已知风险。

### 修复
- 关闭服务时，已打开的页面（SSE 连接）会让关闭等满 15 秒；浏览器预连接但未发请求的连接会再拖 5 秒。现在关闭在毫秒级完成。
- 前端进程崩溃后，若其子进程仍占用输出管道，监管者会一直卡住、不再重启前端；现在 3 秒内识别并重启。
- `web.port` 被其它程序占用时，网关会把公网请求转发给那个程序；现在自动改用空闲端口并告警（`web.port = 0` 表示总是自动选择）。
- Windows：`skinloom` 被强制结束时前端子进程会残留并占住端口；现在前端放在作业对象里随主进程退出。Linux 使用父进程退出信号。
- 设置在并发重载时可能短暂回退到旧版本。
- 缺少管理员令牌时返回 401（此前是 422）。
- 前端在服务端请求后端时设 5 秒超时；后端返回非 JSON（如代理错误页）时给出明确错误。
- 已打开的页面改为整个生命周期只保持一条 SSE 连接，配置变化后在 0–1.5 秒内随机刷新，避免大站所有页面同时重渲染。
- Docker：`server` 镜像的 `/data` 归属非 root 用户（此前新卷不可写导致首次启动失败）；镜像构建会带上前端的 `.npmrc`。
- 仓库强制 LF 换行（`.gitattributes`），避免在 Windows 上打出的发行包里 `start.sh` / `start.command` 是 CRLF 而无法运行。

### 变更
- 源码仓库为 [Entropy-Increase-Team/skinloom](https://github.com/Entropy-Increase-Team/skinloom)，Go 模块路径改为 `github.com/Entropy-Increase-Team/skinloom`；官方域名 **skinloom.cn**（官网、文档站、插件清单 JSON Schema）；容器镜像为 `ghcr.io/entropy-increase-team/{skinloom,server,web}`。
- 移除未实现的配置键 `server.public_url`；站点公开地址统一使用运行时设置 `site.url`。
- 配置文件中的未知键（拼写错误）会在启动和热加载时告警；`web.port`、`log.format`、`server.trusted_proxies` 增加校验。
- MySQL 时间列改为 `DATETIME(6)`（此前为 `TIMESTAMP`，2038 年溢出且受会话时区影响）。
- 日志：组件日志器缓存已构建的处理器，每条日志从 8 次内存分配降为 0 次（约 1050 → 700 ns）。
- 数据目录权限改为 0750。

### 测试
- 新增端到端测试（真实端口启动、SSE、经管理 API 热更新、带打开连接的快速关闭）、启动器监管测试（以测试程序自身作为假前端，含"孙进程占用管道"场景）、网关/代理头/路径规范化、主题资源、密钥并发、配置校验、发布订阅关闭、日志热切换与基准；前端新增 vitest（URL 过滤、主题样式注入过滤、请求超时与错误处理）。
- CI 增加 `go test -race`（Linux）、staticcheck、govulncheck、前端单测与依赖审计。

### 说明
- 管理 API 在 M1（账号与角色）之前使用 `data/secrets/admin.token` 临时令牌。
