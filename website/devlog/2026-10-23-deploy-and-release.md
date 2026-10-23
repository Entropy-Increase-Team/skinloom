---
slug: deploy-and-release
authors: [maintainers]
title: 第 5 期 · M1 审查修复、端口、部署清单与发布
date: 2026-10-23
tags: [devlog, M1, deployment, release]
---

负责人的五条反馈：发布只有工作流、没有 Release；README 只有一种 Docker 方式、不教 k3s/k8s、不教二进制怎么常驻；默认 8080 必然撞端口；M1 代码要完整审查与回归测试；文档站在哪里。

<!-- truncate -->

## M1 代码审查（先写失败测试，再修）

| 问题 | 后果 | 回归测试 |
|---|---|---|
| ent 的 `MaxLen` 按字节计，昵称校验按字符 | 32 个汉字的昵称安装时报 500 | `TestUnicodeNickname` |
| 安装接口先算密码哈希再看令牌 | 公开接口可被用来消耗 CPU/内存，安装后依旧 | `TestSetupRefusesBeforeHashing`（占满哈希并发槽，请求必须不排队就被拒绝） |
| 任一地址 10 次失败即锁定账号 | 一个攻击者就能让超级管理员一直进不来 | `TestLoginLockoutIsPerClient`（含 10 个地址的分布式场景） |
| 客户端地址可能超过 45 字符 | 带接口名的 IPv6 登录报 500 | `TestLongClientAddress` |
| 导入的 argon2 参数上限 1 GiB / 255 线程 | 一条坏数据让每次登录都很昂贵 | `TestDecodePHCRejectsHostileParameters` |
| 再次登录不吊销旧会话 | 切换账号后旧会话仍有效 | `TestLoginReplacesPreviousSession` |
| 环境变量无法设置列表配置 | 容器、集群里配不了可信代理 | `TestListFromEnvironment` |

另外：安装后保存站点设置改用不随请求取消的上下文；旧密码升级的哈希挪出事务；安装向导字段错误本地化。所有新测试先在旧代码上失败，再在修复后通过；账号服务测试同时在 SQLite 与 PostgreSQL 上运行。

## 端口

默认端口改为 **17546**（1 + 九宫格上的 S-K-I-N），前端内部端口 17547。第一版用的是四位数 7546，负责人指出四位数端口大多已被占用（7546 就登记给了 Cisco Fabric Services），于是改成五位数。五位数也不能随便挑：Linux 的临时端口从 32768 开始、Windows 的动态端口从 49152 开始（Hyper-V/WSL 会在这一段保留整块端口）、Kubernetes NodePort 是 30000–32767，所以只在 10000–29999 里选，并远离 Minecraft 的 25565。IANA 在 17540–17549 没有任何登记；改之前也核对过 Windows 的保留端口段。

## 部署

- **Docker Compose**（首选）：`minimal` / `standard`（MariaDB）/ `postgres` 三份，镜像版本、宿主机端口、时区可调。
- **Kubernetes / k3s**：Kustomize 清单 `all-in-one` 与 `split`，`kubectl kustomize` 渲染后用 kubeconform 严格校验，全部 15 个资源通过；CI 新增"Deployment files"任务持续校验清单与 compose 文件。
- **二进制常驻**：发行包附带 systemd、launchd、Windows 计划任务三种配置。PowerShell 脚本保持纯 ASCII：Windows PowerShell 5.1 会按系统代码页（中文系统是 GBK）读取无 BOM 的脚本。
- **`skinloom healthcheck`**：distroless 后端镜像没有 shell 和 curl，也能声明健康检查。

## 发布与文档站

此前从未推送过 `v*` 标签，所以没有 Release；即使推送了，工作流也只会生成草稿。现在：标签即发布（带 `-` 的为预发布），附六个平台的发行包、仅后端的单文件程序与校验和；镜像有 `x.y.z`、`x.y`、`latest`、`edge` 四类标签。文档站新增 GitHub Pages 工作流，需要管理员在仓库设置里把 Pages 来源选为 "GitHub Actions" 一次。

## 本地验证

Go 全部测试（含 PostgreSQL）、前端类型检查与单测、文档站在根路径与 `/skinloom/` 子路径下各构建一次、Kubernetes 清单校验、compose 文件静态解析、`healthcheck` 命令对真实启动的服务。按约定没有在本地构建或运行容器。
