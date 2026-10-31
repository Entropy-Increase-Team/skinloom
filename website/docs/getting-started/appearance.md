---
title: 外观与主题
sidebar_position: 9
---

# 外观与主题

站点的样子在后台的 **外观** 页（`/admin/appearance`）里调整，保存后立即对所有访客生效，不用重启。

## 主题

SkinLoom 自带两套主题：

| 主题 | 特点 |
|---|---|
| 云织默认主题（`default`） | 蓝色系；登录页是居中的卡片；皮肤库每行 5 个 |
| 织锦（`brocade`） | 朱红与金色；登录页分栏，旁边是品牌色面板；皮肤库更紧凑；首页带最新材质与站点数据 |

在"主题"一栏点"启用"即可切换。没有自定义首页时，首页也会换成新主题自带的。安装第三方主题要等到第二阶段的插件平台。

## 颜色

"颜色"一栏可以在主题之上调整亮色、暗色两组常用颜色和圆角。留空就用主题的值；值可以是任何 CSS 颜色（`#336699`、`oklch(0.6 0.2 300)`……），也可以用取色器。调整保存在 `theme.overrides`，对之后启用的其它主题同样生效；换主题后如果不合适，点"全部清除"。

值里不能有 `;`、花括号、尖括号或注释，这样的值保存时会被拒绝。

## 首页

首页由区块从上到下组成。在"首页"一栏可以添加、排序、删除区块，修改每个区块的属性：

| 区块 | 属性 |
|---|---|
| 大标题 `hero` | 标题、副标题、背景图、两个按钮的链接 |
| 公告 `announcement` | 显示 `site.announcement`，没有公告时不显示 |
| 功能介绍 `features` | — |
| 站点数据 `stats` | 标题；显示用户、角色、公开材质的数量 |
| 最新材质 `latest-textures`、热门材质 `popular-textures` | 标题、数量（4–24）、类型（皮肤或披风） |
| 文字 `markdown` | 内容（Markdown） |
| 图片 `image` | 图片地址、说明、链接 |
| 按钮组 `buttons` | 最多 6 个按钮，每个有文字与链接 |
| 启动器配置引导 `launcher-guide` | — |

文字类的属性可以按语言分别填写，空着的语言会用另一种语言的文字。链接只能是本站路径（`/skinlib`）或 `http(s)://` 地址。

保存后首页存在 `home.blocks` 设置里；点"改用主题自带的首页"会把它设回空，以后换主题时首页也跟着换。也可以用命令行：

```bash
skinloom settings set home.blocks '[{"type":"hero"},{"type":"latest-textures","props":{"limit":12}},{"type":"launcher-guide"}]'
skinloom settings set home.blocks null
```

## 站点标识与页脚

| 设置 | 说明 |
|---|---|
| Logo（`site.logo`） | 页头左上角的图片：图片地址或本站路径；留空使用主题自带的标志 |
| 浏览器图标（`site.favicon`） | 标签页上的图标 |
| 页脚文字（`site.copyright`） | Markdown，不能写 HTML；留空显示"© 年份 站点名" |
| 搜索关键词（`seo.keywords`） | 逗号分隔；页面描述使用站点简介 `site.description` |

## 自定义 CSS

`theme.custom_css` 会作为一个单独的样式表加载在主题样式之后，最多 64 KB。它对每个页面生效，所以**只有超级管理员**（以及管理员令牌和命令行）能修改，普通管理员看到的是只读。

主题留了几个类名方便定制，例如首页大标题 `.sl-hero`、分栏登录页旁的面板 `.sl-auth-panel`、Markdown 内容 `.sl-markdown`。颜色请优先用"颜色"一栏或 CSS 变量（`var(--primary)` 等），换主题时更不容易出问题。

自定义 JavaScript 暂不支持，会在加上内容安全策略（CSP）之后提供。

## 从 Blessing Skin 迁移

| BS 选项 | SkinLoom |
|---|---|
| 首页背景图 `home_pic_url` | 大标题区块的"背景图" |
| `favicon_url` | 浏览器图标 |
| 导航栏、侧栏颜色 | "颜色"一栏 |
| `copyright_text` | 页脚文字（改为 Markdown） |
| `custom_css` | 自定义 CSS |
| `custom_js` | 暂不支持 |
| 透明导航、隐藏介绍、固定背景 | 用区块编排与自定义 CSS 实现 |
| `meta_keywords` | 搜索关键词 |
| `meta_extras` | 不提供（任意 HTML 进入页面头部有安全风险） |

## 主题包（给主题作者）

主题和插件用同一种包格式：一个 `plugin.json`（`kind: "theme"`）加上样式表与图片。内置主题在源码的 `themes/` 目录下，可以照着写。清单里可以声明：

- `tokens`：`light`、`dark` 两组颜色 token（名称同 shadcn/ui：`background`、`primary`……），`fonts`，以及 3D 预览的 `viewer.background`；
- `css`：额外的样式表；
- `layout`：`auth` 为 `card` 或 `split`，`skinlib` 为 `comfortable` 或 `compact`；
- `home`：默认的首页区块。

加载时会检查清单：id 只能用 1–32 个小写字母、数字或横线；token 值不能含 `; { } < >` 或注释；首页区块必须是上表里的类型与属性。完整规范见[设计文档 05 §6](/design/05-plugin-theme-standard)。
