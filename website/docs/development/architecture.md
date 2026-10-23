---
title: 架构概览
sidebar_position: 2
---

# 架构概览

```text
                    :17546（唯一对外端口）
浏览器 / 游戏 / 启动器 ──▶ skinloom（Go）
                     ├─ 后端保留路径 → API · 游戏协议 · SSE
                     ├─ /_next/static、public → 直接读前端产物
                     └─ 其它页面 → Next.js（子进程或独立服务）
                                      │ 服务端渲染时调用内部 API
                                      ▼
                     领域服务 ─ Hook 总线 ─ 设置 / 主题 / 插件（热替换注册表）
                                      │
                     SQLite / MySQL / MariaDB / PostgreSQL · 本地盘 / S3 · Redis（可选）
```

## 关键机制

- **启动器**（`internal/launcher`）：`skinloom up` 拉起并监管 Next.js，崩溃自动重启，日志合并输出。
- **网关**（`internal/gateway`）：路径归属表决定请求交给谁。
- **设置**（`internal/settings`）：类型化定义 + 数据库存储 + 原子替换的快照；每次修改递增 `config_version`。
- **热加载**：配置版本号 → 本机广播 + 其它节点轮询 → `/events` SSE → 页面 `router.refresh()`。见 [ADR 0006](/adr/0006-hot-reload-model)。
- **Hook 总线**（`internal/hook`）：Guard / Filter / Event，按贡献者原子替换；内置模块和未来的插件都通过它扩展。
- **主题**（`internal/theme`、`themes/`）：主题包标准，token 由前端根布局注入为 CSS 变量。

## 深入阅读

- [设计文档 04：重构方案](/design/04-rewrite-plan)
- [设计文档 05：插件与主题开发体系](/design/05-plugin-theme-standard)
- [架构决策记录](/adr/)
