---
slug: m1-setup-and-login
authors: [maintainers]
title: 第 4 期 · M1 起步：安装向导、登录与会话
date: 2026-10-23
tags: [devlog, M1, security]
---

站点第一次有了"主人"：首次启动打印一次性安装链接，创建超级管理员；之后用账号登录，管理后台不再依赖临时令牌。

<!-- truncate -->

## 本期完成

- 数据库：`users`、`sessions`、`auth_tokens`、`audit_logs` 四张表，三种数据库各一份迁移（`20261023095021_accounts`）。
- 账号服务 `internal/account`：argon2id 密码哈希（并发受限）、Blessing Skin 全部九种密码算法的验证与登录后自动升级、会话（库里只存令牌哈希）、按 IP 与按账号的失败限流、审计日志。
- 首次安装：`skinloom up` 在站点未安装时打印 `/setup?token=…`，`--open` 直接打开它；`skinloom setup-link` 随时再生成一个。完成安装在一个事务里做完，`system_state` 的 `setup.completed` 行保证并发安装只成功一次，并且之后永不重开。
- API：`GET/POST /api/v1/setup`、`POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`；管理 API 接受管理员会话，设置修改写 `updated_by` 与审计。
- CSRF：带会话 Cookie 的写请求必须带 `X-SkinLoom-Request` 头且 `Origin` 是本站；前端的 `apiFetch` 自动加头，插件前端以后也走它。
- 前端：`/setup` 向导、`/auth/login` 登录页（支持 `?next=`，只接受站内路径）、页头的用户菜单与退出、未安装时的全站提示。新增 `@skinloom/ui` 的 `Input`、`Label`。
- 设计记录：[ADR 0012 账号、会话与首次安装](https://github.com/Entropy-Increase-Team/skinloom/blob/main/docs/adr/0012-accounts-sessions-setup.md)。

## 本地实测（Windows，`skinloom up --dev`，全新数据目录）

- 两个迁移依次应用，控制台打印安装链接。
- 浏览器里：确认密码不一致被前端拦下；填写正确后跳回首页、已登录为"超级管理员"，站点名称即时变为向导里填写的名字，未安装提示消失。
- 再次打开同一安装链接 → 跳走（已安装）。会话 Cookie 对页面脚本不可见（`HttpOnly`）。
- 退出 → 页头恢复"登录"；错误密码显示统一提示；大写邮箱 + 正确密码登录成功并按 `?next=/about` 跳转；`?next=https://evil.example/` 被改回首页。
- 带会话调用管理 API：带 `X-SkinLoom-Request` 头 200，不带 403。
- 服务端与 Next.js 日志全程无警告、无错误。

## 测试

- Go：密码（argon2id、九种 BS 算法按 PHP 源码公式独立计算对照、拒绝超大 argon2 参数、排队时尊重取消）、限流（窗口、内存回收、IPv6 /64）、服务（安装、两个令牌并发安装只出一个超管、登录、记住我、过期、封禁、限流、旧哈希升级）、HTTP（CSRF 头与 Origin、Bearer 脚本不受影响、非管理员 403、HTTPS 下 `Secure`、429 + `Retry-After`）。账号服务测试在本地同时跑了 SQLite 与 PostgreSQL 18。
- 前端：`safeNext`（开放重定向、回环）、错误映射、`apiFetch` 自动加 CSRF 头。

## 踩坑与决策

- **用本机 PostgreSQL 的二进制起临时集群生成迁移**：本机装了 PostgreSQL 18 但需要密码；用它的 `initdb` 在临时目录建一个 trust 认证、监听 55432 的独立集群，不碰原有实例。顺带验证了 M0 手写的 PostgreSQL 初始迁移与 Atlas 生成结果语义一致。注意：`pg_ctl start` 的输出不能接管道（postgres 会继承句柄，命令永不返回），停掉父任务又会把 postmaster 留在半死状态（新连接报 0xC0000142），最后改为直接以后台任务运行 `postgres.exe`。
- **MySQL 迁移仍为手写**：Atlas 的 replay 模式比较的是"回放迁移后的库结构"，不是 SQL 文本，所以手写只需类型、默认值、索引名、外键名一致（名字取自 ent 生成的 `migrate/schema.go`）。CI 的漂移检查会在 MySQL 8.4 上核对。
- **会话按需查询**：每次页面渲染都会请求 bootstrap，如果中间件一律解析会话，就是每页一次多余查询；改为处理函数第一次需要当前用户时才查。
- **路线图对齐**：M0 开发日志曾把 Helm Chart 与 `service install` 列为 M0 收尾，但设计文档 04 §13 把它们放在 M4（部署与运维）。按路线图执行，M0 收尾只保留安装向导（本期完成）。

## 问题与风险

- MySQL 迁移未在本地验证，以 CI 的漂移检查和集成测试为准。
- 失败限流在每个副本内独立计数，多副本时限额等比放宽；全局限流需要 Redis（计划在 M4 的多副本部署中处理）。
- 依赖 `golang.org/x/crypto` 带有 GO-2026-5932（`openpgp` 子包已废弃且不安全），SkinLoom 不导入该子包，govulncheck 判定不受影响。

## 下期计划

M1 继续：注册（`auth.*` 设置、按 IP 注册上限、用角色名注册）、验证码 Provider（图形 / reCAPTCHA / hCaptcha / Turnstile，失败 3 次后要求）、邮件队列与 SMTP 设置、邮箱验证与找回密码（一次性令牌、重置后吊销全部会话）、个人资料（改密/改邮箱吊销其它会话、注销软删除）、管理后台的用户管理。
