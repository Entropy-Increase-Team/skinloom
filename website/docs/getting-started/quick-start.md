---
title: 快速开始
sidebar_position: 1
---

# 快速开始

## 方式一：发行包（推荐）

1. 下载对应平台的压缩包：`skinloom-<版本>-windows-amd64.zip`、`…-darwin-arm64.tar.gz`、`…-linux-amd64.tar.gz` 等。
2. 解压后启动：
   - **Windows**：双击 `start.bat`
   - **macOS**：双击 `start.command`（首次被系统拦截时，右键 → 打开；或执行 `xattr -dr com.apple.quarantine <解压目录>`）
   - **Linux**：`./start.sh`
3. 浏览器打开 <http://localhost:8080>。

发行包里已经带了 Node.js 运行时，不需要另外安装任何东西。数据库（默认 SQLite）、配置、密钥与上传的文件都保存在同目录的 `data/` 文件夹；**备份或迁移站点时复制整个 `data/` 即可**。

发行包目录结构：

```text
skinloom(.exe)   后端程序，同时负责启动前端
web/             前端程序（Next.js）
runtime/node/    随包附带的 Node.js
start.*          启动脚本（等同于 skinloom up --open）
data/            首次启动后生成
```

## 方式二：Docker

```bash
docker compose -f deploy/compose/minimal/compose.yaml up -d
```

单容器同时运行前后端，数据保存在名为 `data` 的卷里。需要 MySQL/MariaDB、前后端分开部署的生产形态见 [部署](../deployment/overview.md)。

## 方式三：从源码运行

需要 Go 1.26+、Node.js 20.9+（推荐 22）与 pnpm 10：

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom
cd skinloom/web && pnpm install && cd ..
go run ./cmd/skinloom up --dev
```

## 启动之后

- 首次启动会生成 `data/config.toml`（带注释的默认配置）和 `data/secrets/`。
- 在账号系统（M1）上线之前，管理 API 使用临时令牌：`skinloom admin token`。
- API 文档：<http://localhost:8080/api/docs>。
