---
title: 快速开始
sidebar_position: 0
---

# 快速开始

五分钟搭起一个能用的皮肤站。正式上线前再看[部署方式](./overview.md)与[反向代理与 HTTPS](./reverse-proxy.md)。

## 1. 启动

**有 Docker**（推荐）：

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
```

**没有 Docker**：从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载对应系统的压缩包，解压后运行：

- Windows：双击 `start.bat`
- macOS：双击 `start.command`（被系统拦截时右键 → 打开）
- Linux：`./start.sh`

发行包自带运行环境，不需要另外安装任何东西。

## 2. 完成安装

首次启动时，日志里会打印一条**一次性安装链接**。Windows 和 macOS 的发行包会自动在浏览器里打开它；Docker 和 Linux 在日志里找：

```bash
docker compose logs skinloom | grep setup      # Docker；发行包看启动窗口
```

打开 `http://服务器地址:17546/setup?token=…`，填写站点名称、站点地址和管理员账号。链接 24 小时内有效，用过即作废；找不到时运行 `skinloom setup-link` 重新生成（Docker 里是 `docker compose exec skinloom /app/skinloom setup-link`）。

只有能看到日志的人才能完成安装，所以公网部署也不会被别人抢先。

## 3. 接下来

| 要做的事 | 去看 |
|---|---|
| 绑定域名、开启 HTTPS | [反向代理与 HTTPS](./reverse-proxy.md) |
| 配置邮件（验证邮箱、找回密码需要） | [注册、登录与邮件](../admin/accounts.md) |
| 调整积分、上传规则、角色名规则 | [站点设置](../admin/settings.md) |
| 换主题、编辑首页 | [外观与主题](../admin/appearance.md) |
| 开机自启、后台常驻 | [发行包与系统服务](./binary.md) 或 [Docker Compose](./compose.md) |
| 从 Blessing Skin 搬过来 | [从 Blessing Skin 迁移](./from-blessing-skin.md) |

## 记住这三件事

- **端口**：只需要开放 **17546**。
- **数据**：数据库、配置、密钥、上传的文件都在数据目录（Docker 里是 `/data` 卷，发行包里是 `data/`）。**备份就是备份这个目录**。
- **升级**：换新版本后重启即可，数据库会自动迁移。
