云织 SkinLoom
==============

快速开始
  Windows : 双击 start.bat
  macOS   : 双击 start.command（首次被拦截时：右键 → 打开）
  Linux   : ./start.sh

  浏览器打开 http://localhost:17546 。数据库、配置与上传文件都保存在同目录的 data 文件夹，
  备份或迁移时复制整个 data 文件夹即可。配置文件 data/config.toml 修改后大部分立即生效。
  首次启动时，日志里有一条一次性安装链接（/setup?token=…），用它创建管理员账号。

后台常驻（开机自启、崩溃自动重启）
  service/ 目录里有本平台的配置：Linux 用 skinloom.service（systemd），macOS 用
  cn.skinloom.plist（launchd），Windows 用 install-task.ps1（任务计划程序）。
  文件开头写明了用法，详见 https://skinloom.cn/docs/deployment/binary

目录说明
  skinloom(.exe)  后端程序（也负责启动前端）
  web/            前端程序（Next.js）
  runtime/node/   随包附带的 Node.js 运行时
  service/        后台服务配置
  data/           首次启动后生成：数据库、配置、密钥、上传文件

官网与文档：https://skinloom.cn
源码：https://github.com/Entropy-Increase-Team/skinloom
鸣谢：Blessing Skin（https://github.com/bs-community/blessing-skin-server）

Quick start
  Windows : double-click start.bat
  macOS   : double-click start.command (if blocked the first time: right-click → Open)
  Linux   : ./start.sh

  Open http://localhost:17546. The database, config and uploads live in the "data" folder
  next to this file; copy that folder to back up or move the site. Most changes to
  data/config.toml apply without a restart.
  On first start the log shows a one-time setup link (/setup?token=…) to create the
  administrator account. To run in the background (start at boot, restart on crash),
  use the file in service/ for your platform; usage is at the top of each file.

Website and docs: https://skinloom.cn · Source: https://github.com/Entropy-Increase-Team/skinloom
License: MIT (see LICENSE and NOTICE). Bundled Node.js: see runtime/node/LICENSE.
