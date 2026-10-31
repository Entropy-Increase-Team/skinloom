---
title: 备份、恢复与 CDN
sidebar_position: 7
---

# 备份、恢复与 CDN

## 备份

```bash
skinloom backup                 # 写到 data/backups/skinloom-<时间>.tar.gz
skinloom backup --keep 7        # 只保留最新 7 份
skinloom backup --out /mnt/backup --no-textures
```

备份文件包含：

| 内容 | 说明 |
|---|---|
| 数据库 | 仅 SQLite：用 `VACUUM INTO` 复制，站点不用停 |
| `config.toml` | 静态配置 |
| `secrets/` | 管理员令牌等密钥 |
| 材质文件 | 通过存储读取，本地目录与 S3 都可以；用 `--no-textures` 跳过（例如材质在 S3、桶本身有备份时） |

**MySQL / PostgreSQL 的数据库不在备份里**，请用各自的工具导出，例如：

```bash
pg_dump -Fc -d skinloom > skinloom.dump
mysqldump --single-transaction skinloom > skinloom.sql
```

备份文件含有密钥和全部账号数据（SQLite 时），权限是 0600，请和数据目录一样妥善保管。

### 定时备份

用系统自带的定时任务调用即可。Linux（cron，每天 4 点）：

```bash
# crontab -u skinloom -e：以服务用户运行，备份文件和 data/backups 都归它
0 4 * * * /opt/skinloom/skinloom backup --data /opt/skinloom/data --keep 14
```

systemd timer、macOS launchd、Windows 计划任务同理：运行 `skinloom backup --data <数据目录> --keep N`。

## 恢复

先**停止站点**，再恢复到数据目录：

```bash
skinloom restore data/backups/skinloom-20261031-040000.tar.gz --data /opt/skinloom/data
```

以系统服务运行时（Linux），用服务用户恢复，恢复出的文件才归它：`sudo -u skinloom ./skinloom restore …`；已经用 root 恢复了，就再执行 `sudo chown -R skinloom: data`。

- 目标目录已经有站点（有 `config.toml`、密钥或数据库）时会拒绝，确认要覆盖时加 `--force`。所有检查都在写入任何文件之前完成，被拒绝的恢复不会改动目标目录。
- 材质写入恢复后配置的存储，所以可以把本地备份恢复到 S3，反过来也行：先恢复，再改 `config.toml` 的 `[storage]`，或者在恢复前准备好目标配置。
- 恢复后照常启动，较新版本的迁移会自动执行。
- MySQL / PostgreSQL 先用 `pg_restore` / `mysql` 导入数据库，再运行 `skinloom restore` 恢复其余部分。

## CDN 刷新

站点放在 CDN 后面时，角色资料（`/{角色名}.json` 等）和头像可以被缓存 `delivery.profile_cache_seconds` 秒。为了让玩家换皮肤后马上生效，在后台 **设置 → CDN 刷新** 选择服务商：

| 服务商 | 需要 |
|---|---|
| Cloudflare | 区域 ID、带"缓存清除"权限的 API 令牌 |
| 阿里云 CDN | 授权了 `cdn:RefreshObjectCaches` 的 RAM 用户的 AccessKey |
| 腾讯云 CDN | 授权了 `cdn:PurgeUrlsCache` 的子账号的 SecretId / SecretKey |
| Webhook | 一个接收 POST 的地址，可以对接其它 CDN |

之后角色创建、改名、换装或被删除时，SkinLoom 会刷新这个角色的 6 个地址（CustomSkinLoader、UniSkinMod、旧版 PNG 与头像）。刷新失败会自动重试。材质文件按内容命名，永远不需要刷新。

"CDN 上的站点地址"留空时使用站点地址 `site.url`。

Webhook 收到的请求：

```json
{"urls": ["https://skin.example.com/Steve.json", "…"], "event": "player.updated"}
```

设置了签名密钥时，请求带 `X-SkinLoom-Signature: sha256=<请求正文的 HMAC-SHA256>`。

手动刷新某些角色（也可以用来检查凭据是否正确）：

```bash
curl -X POST -H "Authorization: Bearer $(skinloom admin token)" -H "Content-Type: application/json" \
  -d '{"players": ["Steve"]}' http://127.0.0.1:17546/api/v1/admin/cdn/purge
```
