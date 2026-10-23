---
title: Docker
sidebar_position: 3
---

# Docker（docker run）

## 镜像

| 镜像 | 内容 | 默认命令 |
|---|---|---|
| `ghcr.io/entropy-increase-team/skinloom` | 前后端一体（与发行包相同的布局），以 `node` 用户运行 | `skinloom up` |
| `ghcr.io/entropy-increase-team/server` | 仅后端，distroless、非 root | `skinloom serve`（只提供 API）；加 `up` 参数则同时作为入口反代前端 |
| `ghcr.io/entropy-increase-team/web` | 仅前端（Next.js），监听 17547 | `node apps/web/server.js` |

均为 amd64 / arm64 多架构，自带 `HEALTHCHECK`。标签：`x.y.z`、`x.y` 为正式版本，`latest` 为最新发布，`edge` 为 main 分支最新构建。

## 一个容器

```bash
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  ghcr.io/entropy-increase-team/skinloom:latest
docker logs skinloom 2>&1 | grep setup              # 一次性安装链接
docker exec skinloom /app/skinloom setup-link       # 或重新生成一个
```

## 使用外部数据库

```bash
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  -e SKINLOOM_DATABASE__DRIVER=mysql \
  -e SKINLOOM_DATABASE__DSN='skinloom:密码@tcp(数据库地址:3306)/skinloom' \
  ghcr.io/entropy-increase-team/skinloom:latest
```

PostgreSQL：`SKINLOOM_DATABASE__DRIVER=postgres`、`SKINLOOM_DATABASE__DSN='postgres://skinloom:密码@数据库地址:5432/skinloom?sslmode=disable'`。数据库需要提前建好（空库即可，表由 SkinLoom 自动创建）。

## 前后端分开

```bash
docker network create skinloom
docker run -d --name skinloom-web --network skinloom --restart unless-stopped \
  -e SKINLOOM_API_INTERNAL=http://skinloom-server:17546 \
  ghcr.io/entropy-increase-team/web:latest
docker run -d --name skinloom-server --network skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  -e SKINLOOM_WEB__MODE=external -e SKINLOOM_WEB__URL=http://skinloom-web:17547 \
  ghcr.io/entropy-increase-team/server:latest up
```

`server` 是唯一对外的入口：API 与游戏协议自己处理，页面请求转给 `web`；`web` 渲染页面时通过 `SKINLOOM_API_INTERNAL` 回调后端。

## 自己构建镜像

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker build -f deploy/docker/all-in-one.Dockerfile -t skinloom/skinloom .   # 或 server / web
```
