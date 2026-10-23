---
slug: m0-foundation
authors: [maintainers]
title: 第 2 期 · M0 地基：一键启动与全链路热加载跑通
date: 2026-10-23
tags: [devlog, M0]
---

项目负责人确认了命名（SkinLoom · 云织）与剩余选型（chi + Huma、ent + Atlas、OAuth2 进 v1、与熵增体系只通过 OIDC 对接），仓库建立，M0 开工。

<!-- truncate -->

## 本期完成

- **后端骨架**：`skinloom` 命令行（`up` / `serve` / `migrate` / `settings` / `admin token` / `openapi` / `version`）；SQLite / MySQL / PostgreSQL 连接；ent 建模，Atlas 生成迁移、goose 执行（带锁、SQLite 迁移前快照）。
- **运行时设置**：类型化定义、逐字段校验、`null` 重置、配置版本号；命令行和其它节点的修改 2 秒内被感知。
- **Hook 总线**：Guard / Filter / Event，按贡献者原子替换，失败策略、超时、panic 隔离，单测覆盖。
- **网关 + 启动器**：单端口；后端保留路径、静态资源由 Go 直出（`immutable`）、页面反代；`web.mode` 四种模式；崩溃退避重启；Windows 下按进程树终止。
- **前端**：Next.js 16 + React 19 + Tailwind v4 + shadcn 风格组件；主题 token 注入、明暗模式、中英双语（语言由后端判定）、首页区块（hero / 公告 / 特性 / 启动器配置）、关于页（鸣谢 Blessing Skin）；后端不可达时降级渲染。
- **主题包标准落地**：默认主题即一个 `kind: "theme"` 的包。
- **发行**：`package.sh` 构建含便携 Node 的发行包与启动脚本；Docker（server / web / all-in-one）与 Compose。
- **工程**：CI（三平台测试、MySQL/PostgreSQL 集成、生成物漂移检查、前端与文档站构建）、发布工作流、Renovate、文档站（本站）、ADR 0001–0009。

## 本地实测（Windows）

| 项目 | 结果 |
|---|---|
| `skinloom up --dev` 一条命令起前后端 | 通过，Next.js dev 约 1 秒就绪 |
| 后台 API 改站点名、主题色、公告 | 已打开的页面约 1–2 秒内自动更新，无整页刷新 |
| 命令行改设置（另一个进程） | 运行中的服务 2 秒内感知并推送 |
| 改配置文件：日志级别 / 前端端口 / 数据库 | 分别为即时生效 / 前端子进程在新端口重启 / 只提示需要重启 |
| Windows 发行包（managed 模式） | 双击脚本等价命令运行通过；zip 49 MB，解压 133 MB |
| Docker 镜像 | 按约定未在本地构建运行，由 CI 发布流程覆盖 |

## 踩坑与决策

- **koanf 自带的文件监听在文件被替换后永久失效**（编辑器原子保存、`sed -i` 都会替换文件）→ 改为监听目录 + 去抖，写了回归测试。见 [ADR 0006](/adr/0006-hot-reload-model)。
- **Huma 不读嵌入结构体上的参数标签**，导致管理接口的鉴权头为空 → 在每个输入结构上直接声明。见 [ADR 0002](/adr/0002-go-modular-monolith)。
- **pnpm 的符号链接布局与发行包不兼容**：Windows 上无法复制符号链接，解引用后 Node 又找不到依赖（`@swc/helpers`）→ 前端工作区改用 `node-linker=hoisted`，standalone 输出零符号链接。见 [ADR 0009](/adr/0009-release-bundles)。
- **发行包改用脚本而非 GoReleaser**：每个目标要附带不同平台的 Node 运行时，脚本更直观。见 [ADR 0009](/adr/0009-release-bundles)。
- ent CLI 的依赖与新版 `uax29` 冲突 → 改用 `entc` 程序化生成。

## 问题与风险

- MySQL / PostgreSQL 的初始迁移为手写，需等 CI 的漂移检查与集成测试首次跑通确认。
- macOS / Windows 发行包尚未签名；正式发布前需要处理公证与代码签名。
- Windows 上若后端进程被强制结束，Node 子进程可能残留（尚未使用 Job Object 绑定生命周期）。

## 下期计划

M0 收尾：Helm Chart 草稿、`skinloom service install`（Windows 服务 / systemd / launchd）、首次运行安装向导与 setup token；随后进入 M1（账号、会话、验证码、邮件、旧密码算法、限流、审计）。
