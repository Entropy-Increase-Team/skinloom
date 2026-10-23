---
title: 部署形态
sidebar_position: 1
---

# 部署形态

| 形态 | 适合 | 说明 |
|---|---|---|
| 发行包（Windows / macOS / Linux） | 个人站、小站 | 解压即用，自带 Node 运行时，见 [快速开始](../getting-started/quick-start.md) |
| Docker 单容器 | 小站、NAS | `deploy/compose/minimal`：一个容器跑前后端，SQLite |
| Docker Compose 分离部署 | 中站 | `deploy/compose/standard`：后端、前端、MariaDB、Valkey 各自一个容器 |
| Kubernetes / k3s（Helm） | 大站 | M4 提供 Helm Chart：后端按角色拆分扩容，前端独立扩容 |

## 路径归属

SkinLoom 只需要对外开放一个端口（默认 8080）。下列路径由后端处理，其余页面由前端渲染：

```text
/api/*  /oauth/*  /.well-known/*  /events  /healthz  /readyz
/{角色名}.json  /csl/*  /usm/*  /skin/*  /cape/*  /textures/*  /raw/*
/avatar/*  /preview/*  /plugins-assets/*  /themes-assets/*
```

在 Nginx、Caddy 或 Ingress 后面部署时，把整个站点反代到 SkinLoom 的端口即可；大站也可以按上表把页面请求直接分给前端服务，减少一跳。

## 放在反向代理 / CDN 后面

把代理的地址加入 `server.trusted_proxies`（IP 或网段），SkinLoom 才会采信它传来的 `X-Forwarded-For`、`X-Forwarded-Proto`、`X-Forwarded-Host`：

```toml
[server]
trusted_proxies = ["127.0.0.1", "10.0.0.0/8"]
```

不配置时（默认）一律以直连的对端为准：这是安全的默认值，但站点会认为协议是 http、客户端 IP 是代理的地址。同时建议设置 `site.url`（`skinloom settings set site.url '"https://skin.example.com"'`）。

## 健康检查

- `GET /healthz`：进程存活
- `GET /readyz`：数据库可用

## 升级

替换程序（或镜像）后重启。启动时会自动执行数据库迁移（带锁，多副本同时启动也安全）；使用 SQLite 时会先把数据库快照到 `data/backups/`。
