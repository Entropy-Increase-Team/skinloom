# 设计文档

SkinLoom 立项阶段的分析与方案，记录的是动工前的设想。实现与文档不一致时，以 [ADR](../adr/README.md) 中更新的决策为准；当前的代码结构见 ADR 0040 与文档站的"架构"页。

| 文档 | 内容 |
|---|---|
| [01 Blessing Skin 架构拆解](01-bs-architecture.md) | BS 的运行模型、数据模型、核心流程、插件系统 |
| [02 功能清单与兼容契约](02-feature-contract.md) | 第一阶段验收基线：功能、协议格式、配置与数据迁移映射 |
| [03 BS 缺陷与性能问题](03-bs-defects.md) | 74 条问题与新系统的结构性对策 |
| [04 重构方案](04-rewrite-plan.md) | 选型、架构、热加载、部署、路线图 |
| [05 插件与主题开发体系](05-plugin-theme-standard.md) | 插件 API 1.0 规范草案 |
| [06 文档与日志体系](06-docs-and-logs.md) | 文档、更新日志、开发日志、ADR 的维护规则 |
| [07 前端重构方案](07-frontend-redesign.md) | 设计系统、组件库、页面重构与主题系统扩展（草案）；规则见 `web/design.md` |
| [08 安装与迁移引导方案](08-setup-wizard-plan.md) | Pre-Core 启动模式、交互式安装向导与 BS 只读桥接迁移方案 |
| [09 插件平台与主题系统实施方案](09-plugin-platform-plan.md) | v2.0 插件平台、JS 沙箱、SDK/CLI、组件覆盖、静态签名市场与首批官方插件完整开发顺序与执行方案 |
| [10 双轨插件架构与官方市场方案](10-dual-tier-plugin-and-market-plan.md) | 混合双轨插件架构（JS 轻量沙箱 + Go 原生子进程）、三方言隔离 SQL、多维环境筛选分包市场与严格分阶段任务清单 |
