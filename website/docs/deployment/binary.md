---
title: 发行包与后台服务
sidebar_position: 5
---

# 发行包与后台服务

从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载对应平台的压缩包：

| 文件 | 内容 |
|---|---|
| `skinloom-<版本>-<系统>-<架构>.zip / .tar.gz` | 完整发行包：`skinloom` 程序、前端、便携 Node.js、启动脚本、`service/` 后台服务配置 |
| `skinloom-server-<版本>-<系统>-<架构>` | 仅后端的单文件程序（前端另行部署时使用） |
| `checksums.txt` | 以上文件的 SHA-256 |

完整发行包不需要另外安装任何东西。目录结构：

```text
skinloom(.exe)   后端程序，同时负责启动前端
web/             前端程序（Next.js）
runtime/node/    便携 Node.js
service/         后台服务配置（systemd / launchd / 任务计划程序）
start.*          前台启动脚本
data/            首次启动后生成：数据库、配置、密钥、上传文件
```

## 前台运行

Windows 双击 `start.bat`，macOS 双击 `start.command`（首次被系统拦截时右键 → 打开，或执行 `xattr -dr com.apple.quarantine <解压目录>`），Linux 运行 `./start.sh`。然后打开 <http://localhost:17546>。关掉窗口就会停止，适合试用。

## 后台常驻

### Linux：systemd

以解压到 `/opt/skinloom` 为例：

```bash
sudo mkdir -p /opt/skinloom
sudo tar xzf skinloom-*-linux-amd64.tar.gz -C /opt/skinloom --strip-components=1
sudo useradd --system --home /opt/skinloom --shell /usr/sbin/nologin skinloom
sudo chown -R skinloom:skinloom /opt/skinloom
sudo cp /opt/skinloom/service/skinloom.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now skinloom
journalctl -u skinloom | grep setup        # 一次性安装链接
```

常用命令：`systemctl status skinloom`、`systemctl restart skinloom`、`journalctl -u skinloom -f`。

单元文件以专用用户运行，并开启了 systemd 的沙箱选项：系统目录只读，只能写 `data/` 与前端的渲染缓存目录 `web/apps/web/.next`。解压到别处时，把文件里的 `/opt/skinloom` 全部替换掉。数据库在本机时，取消 `After=… mariadb.service postgresql.service` 一行的注释。

### macOS：launchd

把 `service/cn.skinloom.plist` 里的 `/Users/you/skinloom` 全部换成你的解压目录，然后：

```bash
cp service/cn.skinloom.plist ~/Library/LaunchAgents/
launchctl load -w ~/Library/LaunchAgents/cn.skinloom.plist
grep setup data/skinloom.log
```

登录后自动启动、崩溃后自动重启，日志在 `data/skinloom.log`。停止：`launchctl unload -w ~/Library/LaunchAgents/cn.skinloom.plist`。需要无人登录也运行时，放到 `/Library/LaunchDaemons` 并加上 `UserName` 键。

### Windows：任务计划程序

在**管理员** PowerShell 里进入解压目录：

```powershell
powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1
Select-String setup .\data\skinloom.log
```

脚本注册名为 `SkinLoom` 的计划任务：开机启动（无需登录）、以 LOCAL SERVICE 身份运行、失败后每分钟重试，并只给 `data\` 与前端缓存目录写权限。日志写到 `data\skinloom.log`。卸载：

```powershell
powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1 -Uninstall
```

## 升级

停止服务 → 用新版本的文件覆盖解压目录（保留 `data/`）→ 启动服务。数据库迁移在启动时自动执行。

`skinloom service install`（一条命令注册为系统服务）计划在 M4 提供。
