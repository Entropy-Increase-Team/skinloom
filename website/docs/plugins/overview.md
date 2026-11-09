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
| 主题包 | 可用。内置的 `themes/default`、`themes/brocade` 就是按标准写的 |
| 领域事件 | 用户、角色、材质、衣柜、举报共 15 个事件，内置模块（如 CDN 刷新）已经在用 |
| 首页区块 | 可用。区块目录见 `GET /api/v1/admin/blocks` |
| 前端插槽 | 页面已留好位置，等插件运行时上线 |
| JS 沙箱、SDK、CLI、插件市场 | 2.0 |

## 写一个主题

主题包是一个目录：

```text
my-theme/
  plugin.json     清单
  theme.css       可选：额外的样式表
  *.png / *.svg   可选：图片
```

`plugin.json` 里可以声明：

- `tokens`：`light`、`dark` 两组颜色（名称同 shadcn/ui：`background`、`primary`……）、`fonts`，以及 3D 预览的背景；
- `css`：额外的样式表；
- `layout`：登录页 `card` 或 `split`，皮肤库 `comfortable` 或 `compact`；
- `home`：默认的首页区块。

加载时会校验：id 只能用 1–32 个小写字母、数字或横线；颜色值不能含 `; { } < >` 或注释；首页区块必须是已知的类型与属性。照着 `themes/` 下的内置主题写最快。目前主题需要随程序一起构建，安装第三方主题要等插件平台。

## 从 Blessing Skin 迁移插件

BS 插件是 PHP，无法直接运行。常用的官方插件（外置登录、第三方登录、S3 存储、CDN 刷新等）SkinLoom 已经内置。设计文档 05 §4.4 给出了 BS 过滤器、事件与 SkinLoom Hook 的对照表。
