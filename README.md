<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">云织 SkinLoom</h1>

<p align="center">开源、自托管的 Minecraft 皮肤站 · Go 后端 + Next.js 前端 · MIT</p>

<p align="center"><a href="https://skinloom.cn">官网 skinloom.cn</a> · <a href="README.en.md">English</a> · <a href="docs/design/README.md">设计文档</a> · <a href="docs/adr/README.md">架构决策</a> · <a href="CHANGELOG.md">更新日志</a></p>

---

> **状态：开发中（M0 地基）。** 尚不能用于生产。路线图见 [设计文档 04 §13](docs/design/04-rewrite-plan.md#13-路线图)。

SkinLoom 是以 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 为蓝本重新设计实现的皮肤站：上传、分享皮肤与披风，管理游戏角色，并为 CustomSkinLoader、authlib-injector（外置登录）等客户端提供皮肤与认证服务。目标是在保持对 Blessing Skin 外部协议兼容（老站可迁移、游戏端无需改配置）的同时，从数据模型、运行模型到插件体系全面重做。

## 特性（规划）

- **一键运行**：Windows / macOS / Linux 发行包解压后双击即可启动（内置 Node 运行时）；也提供 Docker、Docker Compose、Helm（k3s / k8s）。
- **热加载**：站点设置、主题、配置文件、插件安装/升级/卸载均无需重启，已打开的页面秒级刷新。
- **兼容 Blessing Skin**：CSL / USM / Legacy / Yggdrasil 协议与 OAuth API 兼容，提供只读导入工具，玩家 UUID、密码、皮肤链接不变。
- **统一的插件标准**：TypeScript 一门语言、一个 SDK、一个 CLI，后端在沙箱中运行，前端可扩展页面、插槽与首页区块，主题同一标准。
- **多数据库**：SQLite（零依赖）、MySQL / MariaDB、PostgreSQL。

## 快速开始

### 发行包（推荐给站长）

下载对应平台的压缩包并解压：Windows 双击 `start.bat`，macOS 双击 `start.command`，Linux 运行 `./start.sh`，然后打开 <http://localhost:8080>。数据保存在同目录的 `data/`。

### 从源码运行（开发）

需要 Go 1.26+、Node.js 20.9+（推荐 22）、pnpm 10。

```bash
cd web && pnpm install && cd ..
go run ./cmd/skinloom up --dev
```

`up --dev` 会启动后端并在 `./web` 里运行 `pnpm dev`，统一从 <http://localhost:8080> 访问。更多命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。

### Docker

```bash
docker compose -f deploy/compose/minimal/compose.yaml up -d
```

## 文档

- [设计文档](docs/design/README.md)：Blessing Skin 拆解、兼容契约、缺陷清单、重构方案、插件与主题规范、文档体系
- [架构决策记录](docs/adr/README.md)
- 文档站源码：[`website/`](website/)（含开发日志）

## 鸣谢

- **[Blessing Skin Server](https://github.com/bs-community/blessing-skin-server)**：SkinLoom 的设计参考与致敬对象。感谢 Blessing Skin 团队与社区多年的工作；部分翻译文本与渲染算法思路源自 Blessing Skin（MIT），版权声明保留在 [NOTICE](NOTICE)。
- [skinview3d](https://github.com/bs-community/skinview3d)、[authlib-injector](https://github.com/yushijinhun/authlib-injector)、[CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader)，以及 Go 与 Node.js 生态中的众多开源项目。

## 许可

[MIT](LICENSE)。SkinLoom 与 Mojang Studios、Microsoft 无关。
