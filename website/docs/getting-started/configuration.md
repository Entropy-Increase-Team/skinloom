---
title: 配置
sidebar_position: 2
---

# 配置

SkinLoom 有两类配置：

| 类别 | 放在哪 | 改了之后 |
|---|---|---|
| **静态配置**：端口、数据库、前端进程、日志 | `data/config.toml` 或环境变量 | 大部分键立即生效（热键），少数需要重启（冷键） |
| **站点设置**：站点名、主题、首页布局、各类规则 | 数据库，通过后台 / API / 命令行修改 | 全部立即生效，已打开的页面几秒内自动刷新 |

## config.toml

首次启动时自动生成，每个键都有注释标明 `[hot]` 或 `[cold]`：

```toml
[server]
listen = ":8080"          # [cold] 唯一需要开放的端口
role = "all"              # [cold] all | api
trusted_proxies = []      # [hot]  允许设置 X-Forwarded-* 的代理（IP 或网段），例如 ["127.0.0.1", "10.0.0.0/8"]

[database]
driver = "sqlite"         # [cold] sqlite | mysql | postgres
dsn = "skinloom.db"       # [cold]

[web]
mode = "managed"          # [hot]  managed | dev | external | off
port = 3000               # [hot]  前端进程的内部端口；被占用或设为 0 时自动选择空闲端口
url = ""                  # [hot]  mode = external 时的前端地址
node = ""                 # [hot]  指定 node 可执行文件

[log]
level = "info"            # [hot]  debug | info | warn | error
format = "text"           # [hot]  text | json
```

- 编辑器用"原子保存"（先写临时文件再替换）也没关系，监听的是所在目录。
- 文件写错时，SkinLoom 会保留正在运行的配置并在日志里报错，不会把站点弄挂。
- 改了冷键会在日志中提示"需要重启才能生效"。
- 拼错的键（例如 `lisen`、`[databse]`）会在启动和热加载时告警，不会被悄悄忽略。

### 数据库连接串

| 驱动 | `dsn` 示例 |
|---|---|
| sqlite | `skinloom.db`（相对 `data/`） |
| mysql / mariadb | `user:pass@tcp(127.0.0.1:3306)/skinloom` |
| postgres | `postgres://user:pass@127.0.0.1:5432/skinloom?sslmode=disable` |

### 环境变量

任意键都可以用环境变量覆盖，格式 `SKINLOOM_<分组>__<键>`（双下划线分隔层级）：

```bash
SKINLOOM_DATABASE__DRIVER=postgres
SKINLOOM_DATABASE__DSN=postgres://...
SKINLOOM_SERVER__LISTEN=:9000
```

另有 `SKINLOOM_DATA`（数据目录，默认 `./data`）与 `SKINLOOM_CONFIG`（配置文件路径）。

### web.mode

| 值 | 用途 |
|---|---|
| `managed` | 发行包默认：由 SkinLoom 启动并监管 `web/` 里的前端 |
| `dev` | 源码开发：在 `./web` 运行 `pnpm dev` |
| `external` | 前端单独部署（容器、k8s），SkinLoom 把页面请求转发到 `url` |
| `off` | 只提供 API 与游戏接口 |

## 站点设置

```bash
skinloom settings get                                   # 列出全部设置（* 表示改过）
skinloom settings set site.name '{"zh-CN":"我的皮肤站","en":"My Skins"}'
skinloom settings set theme.overrides '{"light":{"primary":"oklch(0.6 0.2 300)"}}'
skinloom settings set site.name null                    # 恢复默认
```

运行中的站点会在 2 秒内感知命令行或其它节点做的修改。也可以调用 `PATCH /api/v1/admin/settings`（M1 前使用 `skinloom admin token` 输出的临时令牌）。

M0 可用的设置：`site.name`、`site.description`、`site.url`、`site.announcement`、`theme.active`、`theme.overrides`、`home.blocks`。
