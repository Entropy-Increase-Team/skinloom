# 架构决策记录（ADR）

格式与规则见 [设计文档 06 §6](https://github.com/Entropy-Increase-Team/skinloom/blob/main/docs/design/06-docs-and-logs.md#6-架构决策记录adr)。编号只增不改；被取代的记录保留并标注。

| 编号 | 标题 | 状态 |
|---|---|---|
| [0001](0001-separate-frontend-with-launcher.md) | 前后端分离，Go 启动器一键运行 | 已接受 |
| [0002](0002-go-modular-monolith.md) | Go 模块化单体：chi + Huma，不用 Kratos | 已接受 |
| [0003](0003-ent-atlas-goose-migrations.md) | ent 建模，Atlas 生成迁移，goose 执行 | 已接受 |
| [0004](0004-nextjs-rendering-strategy.md) | Next.js 渲染策略：公开页 SSR，不依赖 ISR，不用 Module Federation | 已接受 |
| [0005](0005-typescript-plugin-standard.md) | 插件标准：TypeScript + JS 沙箱（QuickJS-NG/WASM） | 已接受 |
| [0006](0006-hot-reload-model.md) | 热加载模型：配置版本号 + 原子替换 + SSE | 已接受 |
| [0007](0007-no-sql-for-plugins.md) | 插件存储不开放 SQL | 已接受 |
| [0008](0008-project-name.md) | 项目命名：SkinLoom · 云织 | 已接受 |
| [0009](0009-release-bundles.md) | 发行包由脚本构建，前端依赖扁平化 | 已接受 |
| [0010](0010-trusted-proxies-and-shutdown.md) | 可信代理、路径规范化与快速关闭 | 已接受 |
| [0011](0011-bun-and-node-24.md) | 包管理器 bun（回退 pnpm / npm），Node.js 24 | 已接受 |
| [0012](0012-accounts-sessions-setup.md) | 账号、会话与首次安装 | 已接受 |
| [0013](0013-ports-deployment-release.md) | 默认端口、部署清单与发布渠道 | 已接受 |
| [0014](0014-registration-and-captcha.md) | 注册与验证码 | 已接受 |
| [0015](0015-jobs-and-mail.md) | 后台任务队列与邮件 | 已接受 |
| [0016](0016-profile-and-user-management.md) | 个人资料、设备管理、注销与后台用户管理 | 已接受 |
| [0017](0017-textures-storage-and-score.md) | 材质、文件存储与积分流水 | 已接受 |
| [0018](0018-players-and-game-apis.md) | 角色、UUID 与游戏皮肤接口 | 已接受 |
| [0019](0019-closet-texture-edit-and-sign-in.md) | 收藏、材质编辑与删除、签到与用户中心 | 已接受 |
| [0020](0020-reports-and-admin-tools.md) | 举报、后台概览与后台角色、衣柜管理 | 已接受 |
| [0021](0021-renderer-notifications-and-s3.md) | 渲染器、站内通知与 S3 兼容存储 | 已接受 |
| [0022](0022-yggdrasil.md) | Yggdrasil 外置登录（authlib-injector） | 已接受 |
| [0023](0023-oauth-provider-and-resource-api.md) | OAuth 2.0 提供方、个人访问令牌与 Blessing Skin 兼容资源 API | 已接受 |
| [0024](0024-bs-admin-resource-api.md) | 兼容 Blessing Skin 的管理类资源 API | 已接受 |
| [0025](0025-social-login.md) | 社交登录（GitHub、Microsoft、OpenID Connect） | 已接受 |
| [0026](0026-transaction-lock-order.md) | 事务的加锁顺序（防死锁） | 已接受 |
