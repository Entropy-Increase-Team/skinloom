---
title: Docker Compose
sidebar_position: 2
---

# Docker Compose（推荐）

仓库的 [`deploy/compose/`](https://github.com/Entropy-Increase-Team/skinloom/tree/main/deploy/compose) 里有三份现成的 compose 文件：

| 目录 | 容器 | 适合 |
|---|---|---|
| `minimal` | 1 个（前后端一体），SQLite | 个人站、中小站、NAS |
| `standard` | 后端、前端、MariaDB | 中大站，前端可多开 |
| `postgres` | 后端、前端、PostgreSQL | 同上，用 PostgreSQL |

所有容器都设置了 `restart: unless-stopped`：Docker 开机自启后，站点随之启动；崩溃后自动重启。

## 镜像源选择（Docker Hub / GHCR 二选一）

所有 Compose 模板**默认从 Docker Hub 拉取**（可直接受益于国内常见的 Docker Hub 镜像加速服务），同时 CI 会双推到 **GHCR**（内容完全一致）。如需切换为 GHCR，无需改 `compose.yaml`，只需在 `.env` 或启动命令前设置镜像变量：

- **`minimal`**：在同目录 `.env` 中写入（或命令前加）`SKINLOOM_IMAGE=ghcr.io/entropy-increase-team/skinloom`
- **`standard` / `postgres`**：取消 `.env` 中以下两行的注释：

```dotenv
SKINLOOM_SERVER_IMAGE=ghcr.io/entropy-increase-team/server
SKINLOOM_WEB_IMAGE=ghcr.io/entropy-increase-team/web
```

## 最小部署

```bash
mkdir skinloom && cd skinloom
curl -4 -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs -f skinloom               # 等待日志打印一次性安装链接（看到后按 Ctrl+C 退出查看）
```

打开 <http://服务器地址:17546>，按安装链接完成初始化。`minimal` 未预设外部数据库，向导中默认使用 SQLite，也可以按需切换为外部 MySQL / PostgreSQL。

## 前后端分离 + MariaDB / PostgreSQL

以 MariaDB（`standard`）为例；若使用 PostgreSQL，把下面两行 URL 里的 `standard` 改为 `postgres` 即可：

```bash
mkdir skinloom && cd skinloom
curl -4 -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/standard/compose.yaml
curl -4 -fsSL https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/standard/.env.example -o .env
sed -i "s/^DB_PASSWORD=.*/DB_PASSWORD=$(openssl rand -hex 24)/" .env
docker compose up -d
docker compose logs -f server                 # 等待数据库就绪并打印一次性安装链接
```

> **提示**：
> - `standard` 与 `postgres` 已在 Compose 中内置数据库容器并通过环境变量注入连接，打开 `/setup` 向导时数据库栏会自动显示为「环境变量托管（只读）」，无需重复填写。
> - 国内服务器若直连 `raw.githubusercontent.com` 较慢或超时，可将下载前缀替换为 `https://cdn.jsdelivr.net/gh/Entropy-Increase-Team/skinloom@main/deploy/compose/...`；上文命令的 `-4` 参数用于规避部分 VPS 半残 IPv6 导致的卡顿。
> - 如果 `docker compose up -d` 后未及时看到安装链接，可直接运行 `docker compose exec server skinloom setup-link` 重新生成。

后端是唯一入口，它把页面请求转给前端容器（`web.mode = external`）。前端无状态，可以多开：

```bash
docker compose up -d --scale web=2
```

## 可调的变量（写进 `.env` 或命令前）

| 变量 | 默认 | 说明 |
|---|---|---|
| `SKINLOOM_IMAGE` | `dnyo666/skinloom` | `minimal` 镜像地址；切到 GHCR 设为 `ghcr.io/entropy-increase-team/skinloom` |
| `SKINLOOM_SERVER_IMAGE` | `dnyo666/skinloom-server` | `standard` / `postgres` 后端镜像；切到 GHCR 设为 `ghcr.io/entropy-increase-team/server` |
| `SKINLOOM_WEB_IMAGE` | `dnyo666/skinloom-web` | `standard` / `postgres` 前端镜像；切到 GHCR 设为 `ghcr.io/entropy-increase-team/web` |
| `SKINLOOM_VERSION` | `latest` | 镜像版本：`1.1.3` 这样的正式版本、`latest`（最新发布）或 `edge`（main 分支最新构建） |
| `SKINLOOM_BIND` | `0.0.0.0` | 宿主机监听地址；若前方有本机 Nginx / Caddy / 宝塔 / 1Panel 反代，建议设为 `127.0.0.1` 避免公网绕过 HTTPS 直连 |
| `SKINLOOM_PORT` | `17546` | 宿主机端口 |
| `TZ` | `Asia/Shanghai` | 时区 |
| `DB_PASSWORD` | — | 数据库密码（`standard`、`postgres` 必填） |

其它配置用 `SKINLOOM_<段>__<键>` 环境变量加到服务的 `environment` 里，例如放在宿主机反向代理后面时的 `SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,::1,172.16.0.0/12`。

## 日常操作

```bash
docker compose pull && docker compose up -d           # 升级
docker compose logs -f                                # 看日志
docker compose exec skinloom skinloom setup-link      # 重新生成安装链接（minimal）
docker compose exec server skinloom setup-link        # 重新生成安装链接（standard / postgres）
```

## 备份与迁移

最小部署的全部数据都在 `skinloom_data` 卷里：

```bash
docker compose stop
docker run --rm -v skinloom_data:/data -v "$PWD":/backup alpine tar czf /backup/skinloom-data.tgz -C /data .
docker compose start
```

恢复时把压缩包解回同名卷即可。`standard`、`postgres` 还要备份数据库卷 `skinloom_db`（或用 `mariadb-dump` / `pg_dump`）。

## 从仓库源码自行构建

如需基于修改后的源码构建本地镜像，请叠加同目录下的 `compose.build.yaml`（生产 `compose.yaml` 本身不包含 `build:`，避免单文件部署拉图失败时误触发本地构建）：

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker compose -f deploy/compose/minimal/compose.yaml -f deploy/compose/minimal/compose.build.yaml up -d --build
```
