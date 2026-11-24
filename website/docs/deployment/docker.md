---
title: Docker
sidebar_position: 3
---

# Docker（docker run）

## 镜像（Docker Hub / GHCR 二选一）

CI 每次发布都会同步构建并推送到 **Docker Hub** 与 **GHCR**，两者内容完全一致，均为 amd64 / arm64 多架构且自带 `HEALTHCHECK`：

- **Docker Hub（默认推荐）**：国内服务器可直接使用常见的 Docker Hub 镜像加速器，拉取更顺畅。
- **GHCR（GitHub Container Registry）**：适合海外服务器或已有 GitHub 镜像加速的环境。

| 角色 | Docker Hub（默认） | GHCR（备用） | 内容 | 默认命令 |
|---|---|---|---|---|
| 一体镜像 | `dnyo666/skinloom` | `ghcr.io/entropy-increase-team/skinloom` | 前后端一体（与发行包相同的布局），以 `node` 用户运行 | `skinloom up` |
| 仅后端 | `dnyo666/skinloom-server` | `ghcr.io/entropy-increase-team/server` | 仅后端，distroless、非 root | `skinloom serve`（只提供 API）；加 `up` 参数则同时作为入口反代前端 |
| 仅前端 | `dnyo666/skinloom-web` | `ghcr.io/entropy-increase-team/web` | 仅前端（Next.js），监听 17547 | `node apps/web/server.js` |

标签：`x.y.z`、`x.y` 为正式版本，`latest` 为最新发布，`edge` 为 main 分支最新构建。下方示例默认使用 Docker Hub，如需改用 GHCR，将镜像名按上表替换即可。

## 一个容器

```bash
# 默认：Docker Hub（改用 GHCR 请替换为 ghcr.io/entropy-increase-team/skinloom:latest）
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  dnyo666/skinloom:latest
docker logs skinloom 2>&1 | grep setup              # 一次性安装链接
docker exec skinloom skinloom setup-link            # 或重新生成一个
```

## 使用外部数据库

```bash
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  -e SKINLOOM_DATABASE__DRIVER=mysql \
  -e SKINLOOM_DATABASE__DSN='skinloom:密码@tcp(数据库地址:3306)/skinloom' \
  dnyo666/skinloom:latest
```

PostgreSQL：`SKINLOOM_DATABASE__DRIVER=postgres`、`SKINLOOM_DATABASE__DSN='postgres://skinloom:密码@数据库地址:5432/skinloom?sslmode=disable'`。数据库需要提前建好（空库即可，表由 SkinLoom 自动创建）。

## 前后端分开

```bash
# 默认：Docker Hub（改用 GHCR 请分别替换为 ghcr.io/entropy-increase-team/web:latest 与 server:latest）
docker network create skinloom
docker run -d --name skinloom-web --network skinloom --restart unless-stopped \
  -e SKINLOOM_API_INTERNAL=http://skinloom-server:17546 \
  dnyo666/skinloom-web:latest
docker run -d --name skinloom-server --network skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  -e SKINLOOM_WEB__MODE=external -e SKINLOOM_WEB__URL=http://skinloom-web:17547 \
  dnyo666/skinloom-server:latest up
```

`server` 是唯一对外的入口：API 与游戏协议自己处理，页面请求转给 `web`；`web` 渲染页面时通过 `SKINLOOM_API_INTERNAL` 回调后端。

## 自己构建镜像

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker build -f deploy/docker/all-in-one.Dockerfile -t skinloom/skinloom .   # 或 server / web
```
