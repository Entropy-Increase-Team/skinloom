---
title: 架构
sidebar_position: 2
---

# 架构

## 一张图

```text
浏览器 / 游戏 / 启动器
        │  :17546（唯一对外端口）
        ▼
┌──────────────── skinloom（Go，一个程序）────────────────┐
│ gateway   后端路径 → httpapi；/_next/static → 磁盘；其余 → Next.js │
│ httpapi   /api/v1（Huma）· 游戏协议 · OAuth · BS 兼容接口 · /events │
│ modules   账号 · 材质 · 角色 · 积分 · 举报 · 通知 · 外置登录 ……  │
│ hook      领域事件（随事务写入任务队列，提交后投递）              │
│ platform  数据库 · 任务队列 · 存储 · 日志 · 发布订阅 ……         │
└──────────────────────────┬─────────────────────────────┘
        ▲ 服务端渲染时调用内部 API │
┌───────┴────────┐           ▼
│ Next.js 前端    │   SQLite / MySQL / PostgreSQL · 本地盘 / S3
│（子进程或独立部署）│
└────────────────┘
```

- **一个程序、一个端口**：`skinloom up` 启动后端，再拉起并看管 Next.js 子进程。大站可以把前端单独部署、多开（`web.mode = external`）。
- **模块化单体**：业务按模块分包，模块之间直接调用对方的 Service；没有微服务，没有消息中间件。
- **热加载**：站点设置、主题、配置文件改了立即生效。设置保存后递增配置版本号，各节点通过 `/events`（SSE）通知浏览器刷新。

## 目录

```text
cmd/skinloom/          main：只调用 internal/cli
internal/
  cli/                 命令行（cobra），每组命令一个文件
  app/                 装配：创建各个服务、启动 HTTP 与后台任务（core.go、run.go）
  config/              config.toml 与环境变量
  gateway/             唯一入口：按路径分给后端或前端
  launcher/            启动、看管 Next.js 子进程
  daemon/              注册为系统服务（systemd / launchd / Windows 服务）
  backup/              备份与恢复
  httpapi/             HTTP 层：把请求翻译成 Service 调用（doc.go 有文件索引）
  hook/                扩展总线：领域事件，将来也是插件的接入点
  modules/             业务模块，每个包拥有自己的表和规则
    account/           用户、会话、安装、注册登录、审计入口
    texture/           上传、皮肤库、衣柜、材质管理与文件清理
    player/  score/  report/  notify/  render/  theme/  settings/ ……
    oauth/  yggdrasil/  social/  cdn/  captcha/  mail/
  platform/            与业务无关的基础设施
    db/  dbtest/  migrate/  jobs/  storage/  pubsub/  logging/ ……
  importer/bs/         从 Blessing Skin 导入
  contract/            游戏接口的契约测试工具
  tools/               开发工具：migdiff（生成迁移）、benchseed（压测数据）
ent/schema/            数据模型（ent），生成代码在 ent/ 下
migrations/            SQLite、MySQL、PostgreSQL 各一套迁移（生成后提交）
api/openapi.json       由代码生成的 OpenAPI 文档
themes/                内置主题包
web/                   前端：apps/web（Next.js）、packages/ui、packages/api-client
website/               本文档站与开发日志
deploy/                Dockerfile、Compose、Kubernetes、Helm、打包脚本
docs/design/ docs/adr/ 设计文档与架构决策记录
```

依赖方向：`cli → app → httpapi → modules → platform`。`platform` 不认识任何业务；`modules` 之间可以互相调用，但不依赖 `httpapi`。

## 一个请求怎么走

以"收藏材质"为例：

1. `gateway` 看到 `/api/v1/me/closet` 属于后端，交给 `httpapi`。
2. 中间件解析客户端地址（只信任配置过的代理）、会话 Cookie，并检查 CSRF 头。
3. `httpapi/manage.go` 的处理函数校验参数，调用 `texture.Service.Favorite`。
4. `Favorite` 在一个事务里按固定顺序加锁（材质 → 用户），重新读取要依据的数据，写衣柜、扣积分、记积分流水，并把领域事件写进同一个事务。
5. 提交后，任务队列把事件投递给订阅者（如 CDN 刷新）。

## 写代码时要遵守的规则

这些规则都来自真实的 bug，详见 ADR：

| 规则 | 为什么 | ADR |
|---|---|---|
| 不变量写进数据库（唯一、外键、CHECK），积分和计数用原子更新 | 应用层检查挡不住并发 | 0019 |
| 事务**先加锁、再判断**：按"材质 → 用户（id 升序）→ 角色 → 其它"加锁，加锁后重新读取决定结果的数据 | 否则并发时会重复扣费或死锁 | 0026、0027 |
| 审计记录和领域事件写在同一个事务里 | 事务回滚时不会留下假记录或发出假事件 | 0028 |
| 事务一律用 `db.WithTx` | 统一提交、回滚，panic 时也会回滚、释放锁 | 0040 |
| 每个新事务都要有并发测试（`dbtest.ForEach`），在 PostgreSQL 上跑 | SQLite 单写者，很多竞态在它上面测不出来 | 0026 |
| 请求路径上不做运维动作（迁移、清缓存、写配置） | 请求可能并发、可能被取消 | 0003 |
| 后端保留路径（`/api/`、`/csl/` 等，见 `internal/gateway`）前端不得占用 | 网关按路径分流 | 0001 |
| 注释写"为什么"，不写"做了什么" | 代码已经说明了做了什么 | — |

## 深入阅读

- [设计文档 04：重构方案](/design/04-rewrite-plan)：整体设计与路线图
- [设计文档 05：插件与主题](/design/05-plugin-theme-standard)
- [架构决策记录](/adr/)
