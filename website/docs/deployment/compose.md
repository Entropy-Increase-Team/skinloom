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

## 最小部署

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # 一次性安装链接
```

打开 <http://服务器地址:17546>，按安装链接完成初始化。

## 前后端分离 + MariaDB / PostgreSQL

```bash
mkdir skinloom && cd skinloom
base=https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/standard   # PostgreSQL 用 …/postgres
curl -fsSLO $base/compose.yaml && curl -fsSL $base/.env.example -o .env
sed -i "s/^DB_PASSWORD=.*/DB_PASSWORD=$(openssl rand -hex 24)/" .env
docker compose up -d
docker compose logs server | grep setup
```

后端是唯一入口，它把页面请求转给前端容器（`web.mode = external`）。前端无状态，可以多开：

```bash
docker compose up -d --scale web=2
```

## 可调的变量（写进 `.env` 或命令前）

| 变量 | 默认 | 说明 |
|---|---|---|
| `SKINLOOM_VERSION` | `latest` | 镜像版本：`0.1.0` 这样的正式版本、`latest`（最新发布）或 `edge`（main 分支最新构建） |
| `SKINLOOM_PORT` | `17546` | 宿主机端口 |
| `TZ` | `Asia/Shanghai` | 时区 |
| `DB_PASSWORD` | — | 数据库密码（`standard`、`postgres` 必填） |

其它配置用 `SKINLOOM_<段>__<键>` 环境变量加到服务的 `environment` 里，例如放在反向代理后面时的 `SKINLOOM_SERVER__TRUSTED_PROXIES`。

## 日常操作

```bash
docker compose pull && docker compose up -d           # 升级
docker compose logs -f                                # 看日志
docker compose exec skinloom /app/skinloom setup-link # 重新生成安装链接（minimal）
docker compose exec server /usr/local/bin/skinloom setup-link   # 同上（standard / postgres）
```

## 备份与迁移

最小部署的全部数据都在 `skinloom_data` 卷里：

```bash
docker compose stop
docker run --rm -v skinloom_data:/data -v "$PWD":/backup alpine tar czf /backup/skinloom-data.tgz -C /data .
docker compose start
```

恢复时把压缩包解回同名卷即可。`standard`、`postgres` 还要备份数据库卷 `skinloom_db`（或用 `mariadb-dump` / `pg_dump`）。

## 还没有发布版本时：从源码构建

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker compose -f deploy/compose/minimal/compose.yaml up -d --build
```

compose 文件同时写了 `image` 与 `build`：能拉到镜像时直接用镜像，拉不到时用仓库源码构建。
