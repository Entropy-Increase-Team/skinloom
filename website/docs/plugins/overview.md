---
title: 插件开发概览
sidebar_position: 1
---

# 插件开发概览

插件平台将在第二阶段（v2.0）开放。本页说明规划中的标准与当前已经就位的部分；完整规范草案见 [设计文档 05：插件与主题开发体系](/design/05-plugin-theme-standard)。

## 一个标准

- **一份清单**：`plugin.json`（带 JSON Schema，编辑器自动补全）
- **一门语言**：TypeScript，后端与前端都用它
- **一个 SDK**：`@skinloom/plugin-sdk`（server / web / testing 三个入口）
- **一个 CLI**：`npm create skinloom-plugin` 起步，`skinloom-plugin dev` 保存即热替换

插件后端运行在宿主内嵌的 JS 沙箱里（有内存与超时限制），只能通过 `ctx` 使用宿主授予的能力：Hook、接口、定时任务、KV 与文档存储、外部请求、用户 / 角色 / 材质 / 积分、登录方式、验证码、CDN 刷新……前端可以加页面、往命名插槽里放组件、提供首页区块与设置页。主题是同一标准下 `kind: "theme"` 的包。

## 现在已经可用的部分（M0）

| 部分 | 状态 |
|---|---|
| Hook 总线（Guard / Filter / Event，按贡献者原子替换） | 事件已就位：用户、角色、材质、衣柜、举报共 15 个领域事件，经任务队列投递（ADR 0028）；Guard 与 Filter 随插件运行时提供 |
| 主题包标准 | 已就位：两套内置主题（`themes/default`、`themes/brocade`）按标准编写，见[外观与主题](../getting-started/appearance.md) |
| 主题 token 覆盖、首页区块编排 | 已就位：后台外观页；区块目录 `GET /api/v1/admin/blocks`，插件区块将用同一套属性定义 |
| 前端插槽 | 页面已声明插槽位置（如 `site.footer`），挂载插件组件要等运行时上线 |
| 设计系统 `@skinloom/ui` | 已有 Button、Card、Badge，将随功能扩充 |
| JS 沙箱、SDK、CLI、插件包、市场 | 第二阶段 |

## 从 Blessing Skin 迁移插件

BS 插件是同进程 PHP，无法直接运行。规范里给出了 BS 过滤器/事件与 SkinLoom Hook 的对照表（设计文档 05 §4.4），常用的官方插件会以内置模块或示例插件的形式提供。
