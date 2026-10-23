# 更新日志 / Changelog

本文件记录所有对用户、站长与插件开发者可见的变化。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.1 发布后由 release-please 根据 Conventional Commits 自动维护；人写的发布说明见文档站"发布说明"。

## [Unreleased]

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
