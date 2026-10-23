---
title: 部署方式
sidebar_position: 1
---

# 部署方式

| 方式 | 适合 | 数据库 | 页面 |
|---|---|---|---|
| Docker Compose **推荐** | 绝大多数站点、NAS | SQLite / MariaDB / PostgreSQL | [Docker Compose](compose.md) |
| docker run | 已有 Docker 环境、想自己编排 | 任意 | [Docker](docker.md) |
| Kubernetes / k3s | 集群、需要横向扩容 | SQLite / PostgreSQL | [Kubernetes / k3s](kubernetes.md) |
| 发行包（二进制） | Windows / macOS 桌面、没有 Docker 的 Linux | 任意 | [发行包与后台服务](binary.md) |
| 反向代理 / HTTPS | 以上任一方式放在 Nginx、Caddy、宝塔、1Panel 后面 | — | [反向代理与 HTTPS](reverse-proxy.md) |

## 共同点

- **端口**：默认 **17546**（1 + 手机九宫格上的 S-K-I-N），是唯一需要对外开放的端口；前端进程的内部端口是 17547。四位数端口大多已被常见软件占用，所以用五位数；它也不落在系统临时端口（Linux 32768 起、Windows 49152 起）与 Kubernetes NodePort（30000–32767）范围内。改端口：`[server] listen = ":8000"` 或环境变量 `SKINLOOM_SERVER__LISTEN=:8000`。
- **首次安装**：首次启动会在日志里打印一次性安装链接 `http://<地址>:17546/setup?token=…`。打开它设置站点名称与地址、创建管理员账号。链接 24 小时有效、用后作废；找不到时运行 `skinloom setup-link` 再生成一个。只有能看到日志的人才能完成安装。
- **数据**：数据库（SQLite 时）、配置、密钥、上传文件都在数据目录（容器里是 `/data`，发行包里是 `data/`）。备份这个目录即可；使用外部数据库时另行备份数据库。
- **升级**：替换程序或镜像后重启。启动时自动执行数据库迁移（带锁，多副本同时启动也安全）；使用 SQLite 时先把数据库快照到 `data/backups/`。
- **配置**：所有 `config.toml` 的键都能用环境变量覆盖，格式 `SKINLOOM_<段>__<键>`，例如 `SKINLOOM_DATABASE__DSN`；列表用逗号分隔，例如 `SKINLOOM_SERVER__TRUSTED_PROXIES=10.0.0.0/8,127.0.0.1`。详见 [配置](../getting-started/configuration.md)。

## 路径归属

下列路径由后端处理，其余页面由前端渲染。一般把整站反代到 17546 即可；大站也可以在 Ingress 里按下表把页面请求直接分给前端服务，少一跳。

```text
/api/*  /oauth/*  /.well-known/*  /events  /healthz  /readyz
/{角色名}.json  /csl/*  /usm/*  /skin/*  /cape/*  /textures/*  /raw/*
/avatar/*  /preview/*  /plugins-assets/*  /themes-assets/*
```

## 健康检查

- `GET /healthz`：进程存活
- `GET /readyz`：数据库可用
- `skinloom healthcheck`：请求本机的 `/readyz`，成功退出码为 0。镜像的 `HEALTHCHECK` 用的就是它（后端镜像是 distroless，没有 shell 和 curl）。
