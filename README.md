<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">云织 SkinLoom</h1>

<p align="center">开源、自托管的 Minecraft 皮肤站</p>

<p align="center"><a href="https://skinloom.cn">文档</a> · <a href="https://github.com/Entropy-Increase-Team/skinloom/releases">下载</a> · <a href="CHANGELOG.md">更新日志</a> · <a href="README.en.md">English</a></p>

---

玩家在云织上传、分享皮肤和披风，给游戏角色换装；启动器和游戏通过 authlib-injector 外置登录或 CustomSkinLoader 等 Mod 显示这些皮肤。

云织以 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 为蓝本重新实现，对外协议保持兼容：老站可以一键迁移，玩家和服务器不用改配置。

> 1.0 正在收尾，目前是 0.x 预览版。

## 功能

- **玩家**：皮肤库、上传与收藏、衣柜、多角色、3D 预览、签到积分、举报、站内通知，GitHub / Microsoft / OpenID Connect 登录
- **游戏**：authlib-injector 外置登录、CustomSkinLoader、UniSkinMod、头像与预览图
- **站长**：后台管理与审计日志；设置、主题、首页在线修改，不用重启；SQLite / MySQL / PostgreSQL；本地或 S3 存储；CDN 刷新；备份恢复
- **开发者**：OAuth 2.0、个人访问令牌、与 Blessing Skin 相同的资源接口、REST API（OpenAPI 3.1）
- **部署**：Docker、Kubernetes / Helm，或 Windows / macOS / Linux 发行包（自带运行环境，可注册为系统服务）

## 快速开始

**Docker：**

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # 一次性安装链接
```

**发行包：** 从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载解压，Windows 双击 `start.bat`，macOS 双击 `start.command`，Linux 运行 `./start.sh`。

打开日志里的安装链接（`http://服务器地址:17546/setup?token=…`），设置站点名称和管理员账号即可。更多部署方式见[文档](https://skinloom.cn/docs/deployment/quick-start)。

## 文档

| 我是…… | 去看 |
|---|---|
| 玩家 | [玩家指南](https://skinloom.cn/docs/players/account)：账号、上传皮肤、角色、在游戏里使用 |
| 站长 | [站长指南](https://skinloom.cn/docs/deployment/quick-start)：部署、配置、后台、从 Blessing Skin 迁移 |
| 开发者 | [开发环境](https://skinloom.cn/docs/development/setup)、[架构](https://skinloom.cn/docs/development/architecture)、[接口](https://skinloom.cn/docs/development/api) |

设计取舍见[设计文档](docs/design/README.md)和[架构决策记录](docs/adr/README.md)。

## 参与开发

需要 Go 1.26+、Node.js 24 和 [bun](https://bun.sh)（或 pnpm / npm），不需要 Docker：

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
cd web && bun install && cd ..
go run ./cmd/skinloom up --dev     # http://localhost:17546
```

命令与约定见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请按 [SECURITY.md](SECURITY.md) 私下报告。

## 鸣谢

感谢 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 团队与社区多年的工作；部分翻译文本与渲染算法思路源自 Blessing Skin（MIT），版权声明见 [NOTICE](NOTICE)。也感谢 [skinview3d](https://github.com/bs-community/skinview3d)、[authlib-injector](https://github.com/yushijinhun/authlib-injector)、[CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader) 等项目。

## 许可

[MIT](LICENSE)。云织与 Mojang Studios、Microsoft 无关。
