---
title: 发行包与后台服务
sidebar_position: 5
---

# 发行包与后台服务

从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载对应平台的压缩包：

| 文件 | 内容 |
|---|---|
| `skinloom-<版本>-<系统>-<架构>.zip / .tar.gz` | 完整发行包：`skinloom` 程序、前端、便携 Node.js、启动脚本 |
| `skinloom-server-<版本>-<系统>-<架构>` | 仅后端的单文件程序（前端另行部署时使用） |
| `checksums.txt` | 以上文件的 SHA-256 |

完整发行包不需要另外安装任何东西。目录结构：

```text
skinloom(.exe)   后端程序，同时负责启动前端
web/             前端程序（Next.js）
runtime/node/    便携 Node.js
start.*          前台启动脚本
data/            首次启动后生成：数据库、配置、密钥、上传文件、日志
```

## 前台运行

Windows 双击 `start.bat`，macOS 双击 `start.command`（首次被系统拦截时右键 → 打开，或执行 `xattr -dr com.apple.quarantine <解压目录>`），Linux 运行 `./start.sh`。然后打开 <http://localhost:17546>。关掉窗口就会停止，适合试用。

## 后台常驻：注册为系统服务

`skinloom service install` 把程序注册为系统服务：开机启动、无需登录、崩溃后自动重启。Linux 用 systemd，macOS 用 launchd，Windows 是真正的 Windows 服务。日志和首次启动的安装链接写到 `data/logs/skinloom.log`。

### Linux

以解压到 `/opt/skinloom` 为例：

```bash
sudo useradd --system --shell /usr/sbin/nologin skinloom   # 服务以这个用户运行
sudo mkdir -p /opt/skinloom
sudo tar xzf skinloom-*-linux-amd64.tar.gz -C /opt/skinloom --strip-components=1
cd /opt/skinloom
sudo ./skinloom service install      # 也会把 data/ 交给 skinloom 用户
sudo ./skinloom service start
sudo grep setup data/logs/skinloom.log   # 一次性安装链接
```

生成的单元名为 `SkinLoom`，开启了 systemd 的沙箱选项：系统目录只读，只能写 `data/` 与前端的渲染缓存目录 `web/apps/web/.next`。建议放在 `/opt` 这类目录：放在 `/home`、`/root` 下也能运行，但家目录对服务只能是只读，而不是完全隐藏。也可以用 `systemctl status SkinLoom`、`journalctl -u SkinLoom`。想以别的用户运行，加 `--user 用户名`；`--user ""` 表示以 root 运行（不推荐）。

### macOS

```bash
cd ~/skinloom    # 解压目录
sudo ./skinloom service install
sudo ./skinloom service start
sudo grep setup data/logs/skinloom.log
```

注册为 `/Library/LaunchDaemons` 下的系统守护进程，开机即运行，不需要登录。

### Windows

在**管理员**终端（PowerShell 或命令提示符）里进入解压目录：

```powershell
.\skinloom.exe service install
.\skinloom.exe service start
Select-String setup .\data\logs\skinloom.log
```

服务名为 `SkinLoom`，在"服务"管理工具里也能看到。它以 LOCAL SERVICE（低权限的内置账户）运行，安装时只给它 `data\` 与前端缓存目录的写权限；设置为延迟自动启动，失败后 5 秒重启。

### 常用命令

Linux、macOS 加 `sudo`，Windows 在管理员终端里运行：

```bash
sudo ./skinloom service status      # running / stopped / not installed
sudo ./skinloom service stop
sudo ./skinloom service restart
sudo ./skinloom service uninstall   # 只删除服务，data/ 保留
```

请在解压目录里执行这些命令：数据目录默认是**当前目录**下的 `data/`。在别处执行，或数据放在别处时，安装和后续命令都加上 `--data <目录>`。

## 升级

`sudo ./skinloom service stop` → 用新版本的文件覆盖解压目录（保留 `data/`）→ `sudo ./skinloom service start`。数据库迁移在启动时自动执行。升级前可以先 [备份](./backup-and-cdn.md)。

Linux 上服务以 `skinloom` 用户运行，覆盖文件后把它要写的目录交还给它，再启动：

```bash
sudo chown -R skinloom: data web/apps/web/.next
```
