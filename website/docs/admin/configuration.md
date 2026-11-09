---
title: 配置文件
sidebar_position: 1
---

# 配置文件

SkinLoom 有两类配置：

| | 管什么 | 在哪改 | 生效 |
|---|---|---|---|
| **配置文件** | 端口、数据库、存储、前端进程、日志 | `data/config.toml` 或环境变量 | 大多立即生效，少数需要重启 |
| **站点设置** | 站点名、注册、积分、主题等运营规则 | 后台"设置"页或命令行，见[站点设置](./settings.md) | 立即生效 |

本页讲配置文件。

## config.toml

首次启动时自动生成，每个键都有注释。标 `[cold]` 的键改了要重启，其它的保存后立即生效：

```toml
[server]
listen = ":17546"         # [cold] 对外端口
role = "all"              # [cold] all | api
trusted_proxies = []      # 允许设置 X-Forwarded-* 的反向代理（IP 或网段），见"反向代理与 HTTPS"

[database]
driver = "sqlite"         # [cold] sqlite | mysql | postgres
dsn = "skinloom.db"       # [cold]

[storage]
driver = "local"          # [cold] local | s3
path = "textures"         # [cold] 本地存储的目录，相对数据目录

[web]
mode = "managed"          # managed | dev | external | off，见下文
port = 17547              # 前端进程的内部端口；被占用时自动换一个
url = ""                  # mode = external 时的前端地址

[log]
level = "info"            # debug | info | warn | error
format = "text"           # text | json
file = ""                 # [cold] 同时写入这个日志文件，相对数据目录；系统服务默认 logs/skinloom.log
```

文件写错时站点继续用原来的配置并在日志里报错；拼错的键会告警。

## 环境变量

任何键都可以用环境变量覆盖：`SKINLOOM_<分组>__<键>`（两个下划线），列表用逗号分隔：

```bash
SKINLOOM_DATABASE__DRIVER=postgres
SKINLOOM_DATABASE__DSN=postgres://skinloom:密码@127.0.0.1:5432/skinloom?sslmode=disable
SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,10.0.0.0/8
```

另外 `SKINLOOM_DATA` 指定数据目录（默认 `./data`），`SKINLOOM_CONFIG` 指定配置文件。密码、密钥建议用环境变量传，不写进文件。

## 数据库

| 驱动 | `dsn` 示例 |
|---|---|
| `sqlite`（默认） | `skinloom.db`（相对数据目录） |
| `mysql`（含 MariaDB） | `skinloom:密码@tcp(127.0.0.1:3306)/skinloom` |
| `postgres` | `postgres://skinloom:密码@127.0.0.1:5432/skinloom?sslmode=disable` |

建一个空库即可，表在启动时自动创建和升级。小站用 SQLite 就够；需要多副本时用 MySQL 或 PostgreSQL。

## 对象存储（S3 兼容）

材质文件默认存在数据目录的 `textures/` 下，也可以放进 Amazon S3、阿里云 OSS、腾讯云 COS、七牛、Cloudflare R2、MinIO 等：

```toml
[storage]
driver = "s3"
endpoint = "oss-cn-hangzhou.aliyuncs.com"   # 主机名（和端口），不带 https://
bucket = "my-skins"
region = "cn-hangzhou"                       # 部分服务需要
access_key = "LTAI..."                       # 密钥用环境变量 SKINLOOM_STORAGE__SECRET_KEY 传
prefix = "textures/"                         # 桶内目录，可以留空
path_style = false                           # MinIO 等自建服务设为 true
public_url = "https://cdn.example.com"       # 可选：桶的公开地址或 CDN
```

- 启动时会检查桶，地址或密钥错了会直接报错。
- 设置了 `public_url` 时，材质文件直接由 CDN 发给玩家，不经过 SkinLoom。
- 从本地迁到对象存储：把 `textures/` 里的文件原样传到桶的 `prefix` 下，改配置后重启。

## 日志

在终端里运行时，日志是方便人看的格式，级别带颜色：

```text
16:03:29 INFO  [migrate] database migrated  applied=11 version=20261030040409
16:03:29 NOTE  SkinLoom is running  url=http://localhost:17546/ database=sqlite
16:03:29 WARN  unknown keys in config file (typo?); they are ignored  keys=[log.levle]
16:03:29 NOTE  first run: open this one-time link to create the administrator account  url=http://…/setup?token=…
```

| 级别 | 含义 |
|---|---|
| `ERROR`（红） | 出错了，需要处理 |
| `NOTE`（绿） | 必须看到的信息：站点地址、首次安装链接、正在关闭。只有 `log.level = error` 时才隐藏 |
| `WARN`（黄） | 值得注意，但站点照常运行，例如配置文件里拼错的键 |
| `INFO` | 正常运行的记录 |
| `DEBUG` | 排查问题用，`log.level = debug` 时才显示，包括每个请求 |

方括号里是输出这条日志的组件，`[web]` 是前端进程。在终端里启动时，日志前面还有一个 SKINLOOM 字符画横幅和版本号。

写进日志文件（`log.file`、系统服务的 `data/logs/skinloom.log`）以及 Docker、systemd 收集的日志，是带完整时间的 `key=value` 格式，方便 `grep` 和日志工具处理；`log.format = "json"` 则全部输出 JSON。

- 不要颜色：设置环境变量 `NO_COLOR=1`。
- 在 `docker compose logs` 这类非终端的地方也想要易读格式：设置 `FORCE_COLOR=1`。

## 前端运行方式（web.mode）

| 值 | 用于 |
|---|---|
| `managed` | 发行包和一体镜像的默认值：SkinLoom 自己启动并看管前端 |
| `external` | 前端单独部署（分离的容器、Kubernetes），页面请求转发到 `web.url` |
| `dev` | 开发：运行源码里的前端开发服务器 |
| `off` | 不要前端，只提供 API 和游戏接口 |
