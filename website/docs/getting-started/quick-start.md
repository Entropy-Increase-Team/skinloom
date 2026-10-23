---
title: 快速开始
sidebar_position: 1
---

# 快速开始

## 方式一：Docker Compose（推荐）

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # 一次性安装链接
```

一个容器同时运行前后端，SQLite 与上传文件保存在 `skinloom_data` 卷里，开机随 Docker 自启。MariaDB / PostgreSQL、前后端分开、Kubernetes 等见 [部署](../deployment/overview.md)。

## 方式二：发行包

1. 从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载对应平台的压缩包：`skinloom-<版本>-windows-amd64.zip`、`…-darwin-arm64.tar.gz`、`…-linux-amd64.tar.gz` 等。
2. 解压后启动：
   - **Windows**：双击 `start.bat`
   - **macOS**：双击 `start.command`（首次被系统拦截时，右键 → 打开；或执行 `xattr -dr com.apple.quarantine <解压目录>`）
   - **Linux**：`./start.sh`
3. 浏览器打开 <http://localhost:17546>。

发行包自带 Node.js 运行时，不需要另外安装任何东西。数据库（默认 SQLite）、配置、密钥与上传的文件都保存在同目录的 `data/`；**备份或迁移站点时复制整个 `data/` 即可**。启动脚本是前台运行的，要开机自启、后台常驻，见 [发行包与后台服务](../deployment/binary.md)。

## 方式三：从源码运行

需要 Go 1.26+、Node.js 24 与 [bun](https://bun.sh)（没有 bun 也可以用 pnpm 或 npm）：

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom
cd skinloom/web && bun install && cd ..   # 或 pnpm install / npm install
go run ./cmd/skinloom up --dev
```

## 启动之后

- 默认端口是 **17546**（唯一需要开放的端口）。
- 首次启动会生成 `data/config.toml`（带注释的默认配置）和 `data/secrets/`。
- 日志里会打印一条**一次性安装链接**（`http://localhost:17546/setup?token=…`，发行包与 `--open` 会自动打开）。打开它，填写站点名称、站点地址和第一个管理员账号；完成后链接立即失效，之后在 `/auth/login` 登录。只有能看到控制台或日志的人才能完成安装，公网部署也不会被别人抢先。
- 链接找不到或过期（24 小时）了：重启一次，或运行 `skinloom setup-link` 生成新链接；容器里用 `docker compose exec skinloom /app/skinloom setup-link`。
- 脚本调用管理 API 时，可以用 `skinloom admin token` 输出的令牌（`Authorization: Bearer …`）。
- API 文档：<http://localhost:17546/api/docs>。
