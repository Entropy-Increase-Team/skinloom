---
title: 插件与主题
sidebar_position: 1
---

# 插件与主题

插件平台计划在 2.0 开放。本页说明规划中的标准，以及现在已经可以用的部分。完整规范草案见[设计文档 05](/design/05-plugin-theme-standard)。

## 规划中的标准

- **一份清单**：`plugin.json`，带 JSON Schema，编辑器可以自动补全。
- **一门语言**：TypeScript，后端和前端都用它。
- **一个 SDK**：`@skinloom/plugin-sdk`。
- **一个 CLI**：`npm create skinloom-plugin` 起步，开发时保存即热替换。

插件后端运行在宿主内嵌的 JS 沙箱里（限制内存和执行时间），只能使用宿主授予的能力；前端可以加页面、往插槽里放组件、提供首页区块。主题是同一标准下 `kind: "theme"` 的包。

## 现在能用的

| 部分 | 状态 |
|---|---|
| 主题包与本地主题 | 可用。内置 `default`、`brocade`，支持在 `<数据目录>/themes/` 放置本地主题、热加载、`extends` 继承与主题设置（见[开发主题](./themes.md)） |
| 领域事件 | 用户、角色、材质、衣柜、举报共 15 个事件，内置模块（如 CDN 刷新）已经在用 |
| 首页区块 | 可用。区块目录见 `GET /api/v1/admin/blocks` |
| 前端插槽 | 页面已留好位置，等插件运行时上线 |
| JS 沙箱、SDK、CLI、插件市场 | 2.0 |

## 写一个主题

运行 `skinloom theme new my-theme --extends default` 即可在 `<数据目录>/themes/my-theme/` 生成主题脚手架，再用 `skinloom theme check` 校验、`skinloom theme reload` 热加载，无需重新编译。完整指南、Token 参考、挂钩清单与示例见[开发主题](./themes.md)。

## 从 Blessing Skin 迁移插件

BS 插件是 PHP，无法直接运行。常用的官方插件（外置登录、第三方登录、S3 存储、CDN 刷新等）SkinLoom 已经内置。设计文档 05 §4.4 给出了 BS 过滤器、事件与 SkinLoom Hook 的对照表。
