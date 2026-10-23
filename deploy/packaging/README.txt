云织 SkinLoom
==============

快速开始
  Windows : 双击 start.bat
  macOS   : 双击 start.command（首次被拦截时：右键 → 打开）
  Linux   : ./start.sh

  浏览器打开 http://localhost:8080 。数据库、配置与上传文件都保存在同目录的 data 文件夹，
  备份或迁移时复制整个 data 文件夹即可。配置文件 data/config.toml 修改后大部分立即生效。

目录说明
  skinloom(.exe)  后端程序（也负责启动前端）
  web/            前端程序（Next.js）
  runtime/node/   随包附带的 Node.js 运行时
  data/           首次启动后生成：数据库、配置、密钥、上传文件

官网与文档：https://skinloom.cn
源码：https://github.com/Entropy-Increase-Team/skinloom
鸣谢：Blessing Skin（https://github.com/bs-community/blessing-skin-server）

Quick start
  Windows : double-click start.bat
  macOS   : double-click start.command (if blocked the first time: right-click → Open)
  Linux   : ./start.sh

  Open http://localhost:8080. The database, config and uploads live in the "data" folder
  next to this file; copy that folder to back up or move the site. Most changes to
  data/config.toml apply without a restart.

Website and docs: https://skinloom.cn · Source: https://github.com/Entropy-Increase-Team/skinloom
License: MIT (see LICENSE and NOTICE). Bundled Node.js: see runtime/node/LICENSE.
