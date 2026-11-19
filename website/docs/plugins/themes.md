---
title: 开发主题
sidebar_position: 4
---

# 开发主题

站长无需重新编译程序，就可以在 `<数据目录>/themes/<id>/` 下创建、调试并启用自己的主题（ADR 0043）。

## 快速开始

```bash
# 1. 生成继承默认主题的脚手架（默认写入 data/themes/my-theme/）
skinloom theme new my-theme --extends default

# 2. 校验清单、资源与文字对比度
skinloom theme check data/themes/my-theme

# 3. 通知运行中的服务重新扫描（或在后台「外观 → 主题」点「重新扫描」）
skinloom theme reload
```

以 `skinloom up --dev` 启动时，后端会自动监听 `data/themes/` 目录，修改 `plugin.json` 或 `theme.css` 保存即通过 SSE 刷新已打开的浏览器页面。本地主题的资源地址以内容哈希命名（`/themes-assets/<id>/<哈希>/<文件>`），修改文件无需手动改版本号即可生效。

也可以在「后台 → 外观 → 颜色」调整好配色后点「导出为主题」，下载打包好的 `.zip` 解压到 `data/themes/` 作为起点。

---

## 目录与清单

```text
data/themes/my-theme/
├── plugin.json       主题清单（必填）
├── theme.css         样式表（可选）
├── screenshot.webp   后台主题卡片预览图，建议 16:10（可选）
├── assets/           字体、背景图等静态资源（可选）
└── web/index.js      Level ⑤ 组件覆盖前端打包入口（可选，需签名包或超级管理员显式信任）
```

静态资源只提供普通文件，扩展名限定为 `css`、`woff2`、`woff`、`png`、`webp`、`avif`、`jpg`、`jpeg`、`gif`、`svg`（SVG 自动附带 `Content-Security-Policy: sandbox`）。

### 五层主题定制模型与 Level ⑤ 组件覆盖

SkinLoom 的主题能力从轻量到深度分为 5 层：

1. **Level ① 语义 Token（`theme.tokens`）**：调整亮暗色板、字体栈、圆角、阴影与 3D 预览背景，纯 JSON 声明。
2. **Level ② 布局变体与默认首页（`theme.layout` / `theme.home`）**：切换登录页、皮肤库、用户中心、材质详情页的版式以及默认首页区块。
3. **Level ③ 主题设置（`theme.settings`）**：声明供站长在后台可视化调节的配色、图片、开关与下拉选项。
4. **Level ④ 自由样式表（`theme.css`）**：通过无 Cascade Layer 的 CSS 和稳定挂钩类名深度定制界面细节。
5. **Level ⑤ 核心组件覆盖（`web: "web/index.js"` + `overrides`）**：通过 `bunx create-skinloom-plugin my-theme --template theme` 创建带有 `@skinloom/plugin-sdk/web` 的主题工程，可注册替换 `site.header`、`site.footer`、`texture.card`、`home.hero` 等核心宿主组件（包裹在独立 ErrorBoundary 中，出错自动回退宿主默认组件）。
   - **安全门控**：由已信任公钥签名的 `.slp` 主题包安装后可直接加载 `web/index.js`；放在 `data/themes/` 下的本地未签名主题默认只加载 CSS 与 Token，若包含 `web/index.js`，需要超级管理员在「后台 → 外观 → 主题」卡片上点「启用脚本」（写入 `theme.trusted_web`）后才会加载脚本。
   - **打包与分发**：主题既可以放在 `data/themes/<id>/` 本地使用，也可以用 `skinloom-plugin pack --key <私钥>` 打包为签名的 `.slp` 扩展包，在「后台 → 插件与主题」上传或发布到官方市场供所有站长在「外观 → 浏览主题市场」一键安装。

---

## Token 参考与对比度要求

组件只使用语义 token，主题可以修改值，但不要改变其含义：

| Token | 用途 |
|---|---|
| `background` / `foreground` | 页面底色与一级正文 |
| `card` / `card-foreground` | 卡片底色与文字 |
| `popover` / `popover-foreground` | 下拉菜单、浮层底色与文字 |
| `muted` / `muted-foreground` | 弱化底板（输入框、表头）与二级说明文字 |
| `faint-foreground` | 三级元信息文字（时间、计数、哈希） |
| `secondary` / `secondary-foreground` | 次要按钮底色与文字 |
| `accent` / `accent-foreground` | 菜单项、幽灵按钮的悬停底色（shadcn 约定，非品牌强调色） |
| `primary` / `primary-foreground` | 主操作按钮、当前选中项、链接、焦点环 |
| `highlight` / `highlight-foreground` | 品牌强调色：积分、签到、排行、警告（`warning` 未填写时默认同 `highlight`） |
| `success` / `success-foreground` | 成功状态 |
| `destructive` / `destructive-foreground` | 危险操作与错误提示 |
| `border` / `input` / `ring` / `overlay` | 边框、输入框边线、焦点环、对话框遮罩 |
| `chart-1` … `chart-5` | 后台图表序列颜色 |
| `sidebar`、`sidebar-foreground`、`sidebar-primary`、`sidebar-accent`、`sidebar-border`、`sidebar-ring` | 用户中心与后台侧栏 |
| `pixel-a` / `pixel-b` | 皮肤与披风预览背后的 8px 棋盘格双色 |
| `shadow-popover` / `shadow-dialog` | 浮层与对话框阴影 |
| `radius` | 圆角基准（写在 `light` 中，亮暗共用） |
| `fonts.sans` / `fonts.mono` / `fonts.display` | 正文、等宽、大标题字体栈 |
| `viewer.background` | 3D 预览器背景（CSS 背景值或 `assets/...` 相对路径） |

### 对比度要求

`skinloom theme check` 会按 WCAG 2.1 检查亮色与暗色（当 `colorScheme` 包含该模式时）的对比度，不达标时给出警告：

| 文字 / 背景组合 | 最低对比度 |
|---|---|
| `foreground` / `background` | 7:1 |
| `muted-foreground` / `card`、`muted` | 4.5:1 |
| `faint-foreground` / `card`、`background` | 4.5:1 |
| `primary-foreground` / `primary` | 4.5:1 |
| `primary` / `card` | 4.5:1 |
| `highlight-foreground` / `highlight` | 4.5:1 |
| `success-foreground` / `success` | 4.5:1 |
| `destructive-foreground` / `destructive` | 4.5:1 |
| `ring` / `background` | 3:1 |

访问 `/admin/appearance/preview`（组件总览页）可以在当前主题的亮暗模式下一次核对全部基础组件与业务组件。

---

## 样式优先级与挂钩清单

### 优先级约定（Cascade Layers）

宿主全部样式都放在 Tailwind 的 `@layer`（`theme`、`base`、`components`、`utilities`）中；主题的 `theme.css` 不在任何 layer 内，因此**无需写 `!important` 即可自然覆盖宿主工具类**。如果希望某条规则保持较低优先级（允许组件工具类覆盖），可以把它包进 `@layer components { ... }`。

### 稳定挂钩（Hooks）

以下类名与属性作为版本化的公共契约维护：

| 挂钩 | 位置 |
|---|---|
| `html[data-sl-theme="<id>"]` | 根元素：当前启用的主题 ID |
| `html[data-sl-setting-<名称>="<值>"]` | 根元素：主题设置项的当前值 |
| `[data-sl-area]`（`public`、`user`、`admin`、`auth`） | 各区域外框 |
| `.sl-header`、`.sl-footer` | 公开页与应用的顶栏、页脚 |
| `.sl-sidebar` | 用户中心与后台导航（侧栏或顶栏标签） |
| `.sl-page-header` | 页面标题区 |
| `.sl-hero`、`.sl-block-<类型>` | 首页大标题（`banner` 模式）与各类型区块容器 |
| `.sl-auth-panel` | 分栏登录页（`auth: "split"`）的展示面板 |
| `.sl-texture-card`、`.sl-player-card` | 材质卡片、角色卡片 |
| `.sl-pixel-backdrop`、`.sl-viewer` | 像素棋盘格底板、3D 皮肤预览器 |
| `.sl-markdown` | 服务端渲染的 Markdown 正文 |
| `[data-slot="<名称>"]` | 基础组件根元素（`button`、`card`、`input` 等） |

---

## 布局变体与首页区块

在 `theme.layout` 中可按区域选择布局：

| 区域 | 可选值（第一项为默认） | 说明 |
|---|---|---|
| `auth` | `card` \| `split` | 居中单卡片，或桌面端左面板 + 右表单分栏 |
| `skinlib` | `comfortable` \| `compact` | 皮肤库网格密度（每行最多 5 张或 8 张） |
| `user` | `sidebar` \| `tabs` | 用户中心使用左侧可折叠侧栏，或顶栏下的水平标签栏 |
| `texture` | `split` \| `stacked` | 材质详情页左右分栏，或上下叠放 |

在 `theme.home` 中可声明默认首页区块（站长未自定义首页时使用）。`hero` 区块支持：

- `layout`: `split`（默认，左文右 3D 走秀）、`centered`（居中纯文字）、`banner`（背景图或 `--brand-gradient` 横幅）
- `showcase`: `latest`（最新公开皮肤）、`popular`（最热公开皮肤）、`none`（隐藏展示区）
- `background`: 横幅背景图（支持主题内相对路径 `assets/...`）

---

## 主题设置与继承

### 继承（`extends`）

通过 `"extends": "default"` 可以只写与父主题不同的部分：

- `tokens` 按分组（`light`、`dark`、`fonts`、`viewer`）逐项合并，子主题覆盖父主题；
- `css` 先加载父主题样式表，再加载子主题样式表；
- `layout`、`settings` 按键合并，`home` 以子主题为准（未写则沿用父主题）；
- 只允许一层继承，避免循环依赖。

### 主题设置（`theme.settings`）

主题可以在 `plugin.json` 中声明供站长在「后台 → 外观 → 主题设置」调节的选项，支持 5 种类型：`color`、`image`、`choice`、`text`、`boolean`。

- 带 `"token"` 的 `color` / `text` 设置项会直接写入对应的亮暗 token（位于主题默认值之上、`theme.overrides` 之下）；
- 其余设置项同时注入为 CSS 变量 `--sl-setting-<名称>` 和根元素属性 `html[data-sl-setting-<名称>="<值>"]`。

---

## 字体与授权提醒

如果在 `assets/` 中放入自定义字体并在 `theme.css` 中通过 `@font-face` 引用：

1. 优先使用 `woff2` 格式并按 `unicode-range` 切片，避免首屏下载整包数 MB 的中文字体；
2. **请务必核对字体许可证**：免费商用字体（如部分厂商系统字体）未必允许从自建服务器再分发网页字体文件。分发主题包时建议选用 SIL Open Font License 1.1（如 Inter、思源黑体、JetBrains Mono）等明确允许 Web 再分发的字体，或在文档中说明由站长自行放置字体文件。

---

## 完整示例

### 示例一：只改配色的轻量主题（继承 `default`）

```json
{
  "$schema": "https://skinloom.cn/schema/plugin.v1.json",
  "id": "forest",
  "kind": "theme",
  "version": "1.0.0",
  "name": { "zh-CN": "松林", "en": "Forest" },
  "description": { "zh-CN": "冷杉绿配色的轻量主题", "en": "A calm fir-green palette extending the default theme" },
  "license": "MIT",
  "apiVersion": 1,
  "extends": "default",
  "theme": {
    "tokens": {
      "light": {
        "primary": "oklch(0.46 0.12 155)",
        "primary-foreground": "oklch(0.985 0.002 155)",
        "ring": "oklch(0.46 0.12 155)"
      },
      "dark": {
        "primary": "oklch(0.73 0.12 155)",
        "primary-foreground": "oklch(0.17 0.02 155)",
        "ring": "oklch(0.73 0.12 155)"
      }
    }
  }
}
```

### 示例二：调整布局、首页与主题设置的完整主题

```json
{
  "$schema": "https://skinloom.cn/schema/plugin.v1.json",
  "id": "atelier",
  "kind": "theme",
  "version": "1.0.0",
  "name": { "zh-CN": "工坊", "en": "Atelier" },
  "description": { "zh-CN": "分栏登录、顶部导航的用户中心与可调密度的皮肤站主题", "en": "Split sign-in, top-tab user center and configurable settings" },
  "license": "MIT",
  "apiVersion": 1,
  "extends": "default",
  "screenshot": "screenshot.webp",
  "theme": {
    "colorScheme": "both",
    "tokens": {
      "light": {
        "primary": "oklch(0.48 0.14 35)",
        "primary-foreground": "oklch(0.985 0.004 75)",
        "radius": "0.5rem"
      },
      "dark": {
        "primary": "oklch(0.74 0.13 40)",
        "primary-foreground": "oklch(0.18 0.015 40)"
      },
      "viewer": {
        "background": "url(assets/studio.webp) center / cover"
      }
    },
    "css": "theme.css",
    "layout": {
      "auth": "split",
      "skinlib": "compact",
      "user": "tabs",
      "texture": "stacked"
    },
    "home": [
      {
        "type": "hero",
        "props": {
          "layout": "split",
          "showcase": "popular",
          "primaryAction": "/skinlib",
          "secondaryAction": "/auth/register"
        }
      },
      { "type": "popular-textures", "props": { "limit": 8 } },
      { "type": "stats" },
      { "type": "launcher-guide" }
    ],
    "settings": {
      "accent": {
        "type": "color",
        "label": { "zh-CN": "主色", "en": "Primary color" },
        "token": "primary"
      },
      "card-border": {
        "type": "choice",
        "label": { "zh-CN": "卡片边框", "en": "Card border" },
        "options": ["subtle", "strong"],
        "default": "subtle"
      }
    }
  }
}
```

配套的 `theme.css`：

```css
html[data-sl-setting-card-border="strong"] .sl-texture-card,
html[data-sl-setting-card-border="strong"] .sl-player-card {
  border-color: color-mix(in oklab, var(--foreground) 24%, transparent);
}
```
