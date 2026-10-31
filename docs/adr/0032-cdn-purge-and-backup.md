# 0032 CDN 刷新跟随领域事件；backup / restore 命令

- 状态：已接受（2026-10-31）
- 相关：设计文档 02 F72、F78；04 §5.4；ADR 0021、0028、0029

## 背景
M4 第四部分（ADR 0029）是 CDN 刷新与备份：
- 角色资料（`/{name}.json` 等）和头像允许 CDN 缓存 `delivery.profile_cache_seconds` 秒（默认 60）。站长把这个值调大以减轻源站压力后，换了皮肤要等缓存过期才看得到。BS 靠 refresh-ali-cdn、refresh-qcloud-cdn 等插件解决。ADR 0028 的领域事件就是为它准备的。
- BS 没有备份功能。02 F78 要求 `backup` 子命令：SQLite 在线备份加材质文件。

## 决策
- **CDN 刷新由领域事件驱动**（`internal/cdn`）：
  - 订阅 `player.created`、`player.renamed`、`player.updated`、`player.deleted`。材质被删除、改为私有或移出衣柜时被脱下的角色，已经会发出 `player.updated`（ADR 0028），所以不用再单独订阅材质事件。
  - 每个角色刷新 6 个地址：`/{name}.json`、`/csl/…`、`/usm/…`、`/skin/{name}.png`、`/cape/{name}.png`、`/avatar/player/{name}`；改名时新旧名字都刷新。带查询参数的头像变体（尺寸）无法一一列出，等它们按缓存时间过期。材质文件按内容哈希命名，不需要刷新。
  - 刷新失败时返回错误，由任务队列按 1、5、15、60 分钟重试。
  - **只在配置了 CDN 时注册处理器**：没有处理器时事件根本不会写进任务队列（ADR 0028），不用 CDN 的站点没有任何额外开销。`settings.changed` 时重新判断，所以开关 CDN 不用重启。
- **服务商**：
  - Cloudflare：按 URL 清除，每次 30 个；
  - 阿里云 CDN：`RefreshObjectCaches`，RPC 签名 v1；
  - 腾讯云 CDN：`PurgeUrlsCache`，TC3-HMAC-SHA256，签名 content-type、host 与 x-tc-action 三个头；
  - Webhook：把 `{"urls", "event"}` POST 到指定地址，可选 HMAC-SHA256 签名头 `X-SkinLoom-Signature`，用来接入其它 CDN。

  两种签名都用官方文档里的示例做了测试：阿里云示例的签名结果，腾讯云示例的请求体哈希、规范请求哈希、派生密钥与最终签名（示例的 SecretKey 打了码，从派生密钥开始逐步核对）。阿里云 ESA 的接口不同，没有做；需要时用 Webhook 接入。
- **设置**：`cdn.provider`、`cdn.base_url`（留空用 `site.url`），以及各服务商的凭据。密钥类都是只写的 Secret 设置（ADR 0029）。
- **手动刷新**：`POST /api/v1/admin/cdn/purge {"players": [...]}`。没有配置 CDN 时返回 409，服务商拒绝时返回 502 并带上它的回答，也可以用来检查凭据。操作写审计。
- **`skinloom backup`**：
  - 写出 `<data>/backups/skinloom-<时间>.tar.gz`，也可以用 `--out` 指定目录，文件权限 0600。
  - 内容：清单 `manifest.json`（格式版本、SkinLoom 版本、数据库类型、迁移版本、材质数量），然后是 `config.toml`、`secrets/`、SQLite 数据库与材质文件。
  - SQLite 用 `VACUUM INTO` 复制，站点可以继续运行。打开数据库时**不执行迁移**，升级前做的备份保持原来的结构。
  - MySQL 和 PostgreSQL 不导出数据库，提示用 `mysqldump` / `pg_dump`：它们的原生工具更可靠，也支持增量与时间点恢复。
  - 材质通过存储层读取，本地与 S3 都能备份；`--no-textures` 跳过材质。`--keep N` 只保留最新 N 份。
- **`skinloom restore <archive>`**：
  - 只接受已知的条目名（`manifest.json`、`config.toml`、`database.sqlite`、`secrets/<名>`、`textures/<哈希>`），归档写不到数据目录以外。
  - 先检查清单的格式版本。目标已有 `config.toml` 或数据库时拒绝，除非加 `--force`。
  - 数据库写到恢复后的配置所指的位置，并删掉旧的 `-wal` / `-shm`。材质通过恢复后配置的存储写入，所以本地备份可以恢复到 S3，反过来也行。
  - 要求站点已停止。恢复后启动时会自动执行较新的迁移。
- **定时备份交给系统**：cron、systemd timer 或 Windows 计划任务调用 `skinloom backup --keep N`，文档给出示例；服务本身不内置调度。

## 影响
- 新设置 `cdn.*`；新接口 `purgeCDN`；新命令 `backup`、`restore`。没有新表与迁移。
- 备份文件含有密钥和（SQLite 时的）全部账号数据，需要和数据目录同样保管。
