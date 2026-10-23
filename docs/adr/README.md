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
