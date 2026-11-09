# 0042 前端设计系统与组件库

- 状态：已接受（2026-11-09）
- 相关：设计文档 07（前端重构方案）；`web/design.md`（设计规则）；ADR 0004（渲染策略）、0030（外观与 token）；设计文档 05 §5.5（`@skinloom/ui`）

## 背景

1.0 的前端功能齐全，但界面是 shadcn 默认样式的拼装：`@skinloom/ui` 只有 5 个组件，14 处 `window.confirm`、14 处原生 `<select>`，同一串输入框样式在 6 个文件里各写一份；数据请求都是 `useState` + `useEffect`；中文落到各平台的系统字体；手机宽度下顶栏溢出。设计文档 05 承诺给插件的组件库大部分不存在。设计文档 07 的审查与 Pencil 高保真原型（首页、用户中心、后台概览）已评审通过。

## 决策

- **设计规则写在 `web/design.md`**，是界面的唯一依据；改规则先改它。设计文档 07 记录理由与实施阶段（P0–P5）。
- **颜色**：中性色阶 + 靛蓝主色（`primary`）+ 金线强调色（新 token `highlight`）+ 成功绿 + 危险红，共五种；警告复用金线，信息复用靛蓝。所有文字组合对比度不低于 4.5:1，内置主题由测试检查。
  - `accent` 保持 shadcn 的含义（菜单、幽灵按钮的悬停底色），品牌强调色是 `highlight`。
  - 新增 token：`faint-foreground`（三级文字）、`highlight`、`success`、`warning`、`*-foreground`、`pixel-a/b`（像素格底板）、`shadow-popover`、`shadow-dialog`、`chart-1..5`、`sidebar-*`；字体增加 `mono`、`display`。
  - `brand-gradient` 保留为兼容 token，默认主题不再用色相渐变。
- **字体**：Inter（英文与数字）+ 思源黑体 Noto Sans SC（中文）+ JetBrains Mono（代码、哈希、UUID），全部 SIL OFL 1.1，用 Fontsource 的可变字体包从 npm 安装，随前端构建进 `/_next/static/media`，由网关直接提供。中文按 `unicode-range` 切片，页面用到哪些字只下载哪几片。
- **组件**：基础组件全部采用 shadcn/ui，底层 Base UI（shadcn 自 2026 年 7 月的默认），风格 `nova`，用 shadcn 的 monorepo 方式安装到 `packages/ui`；皮肤站自己的组件（材质卡片、双面/单面预览、展示区轮播、确认对话框等）放在 `packages/ui/src/components/skinloom/`。两者都通过 `@skinloom/ui` 导出给插件。
- **token 与基础样式移到 `packages/ui/src/styles/globals.css`**，应用从 `@skinloom/ui/globals.css` 引入；将来插件用 Tailwind 时引入同一份定义。
- **图标**：只用 lucide，线宽全站 1.75（全局 CSS），同一概念只用同一个图标（design.md §7.1）。后端与插件下发的菜单图标是 lucide 名称字符串，由 `@skinloom/ui` 的允许列表映射成组件，不在列表里的不显示，不按名称动态导入整个图标库。后端皮肤库菜单的图标从 `shirt` 改为 `layout-grid`（`shirt` 是衣柜）。
- **数据层**：引入 TanStack Query 5。orval 在原有的 fetch 函数之外再生成一份 Query 钩子（`src/gen/query.ts`），同样走 `apiFetch`（CSRF 头、超时、`ApiError`）。服务端组件继续用 fetch 函数取首屏数据，作为 `initialData` 交给客户端；收到 `config.changed` 时除了 `router.refresh()` 还使全部查询失效。
- **皮肤的两种视图**：列表一律用双面视图（后端 `/preview/{tid}`，正反面并排的 2D 图）；首页展示区、材质详情、上传预览用单面视图（skinview3d 的 3D 预览）。
- **只用 token**：组件代码里不用 Tailwind 调色板颜色（`slate-*`、`white`、`black` 等），`bun run test` 里的样式检查拒绝这类类名。
- **不引入**：motion（CSS 过渡与 `tw-animate-css` 够用）、Storybook（改为后台内置的组件总览页）、表单库（等 2.0 设计插件 SDK 的 `Form` 时再定）。

## 备选方案

- **shadcn 的 Radix 底层**：现有代码用 Radix 的 `Slot`（`asChild`），但这些地方在页面重写时都要改；Radix 维护放缓，shadcn 已经默认 Base UI。
- **React Aria（BoardUI 的底层）**：与 shadcn 生态脱节，组件要自己从头写样式。
- **HarmonyOS Sans、MiSans**：协议限制单独再分发或修改（网页切片算修改），放进 MIT 仓库与发行包有风险。站长可以在自己的主题里换用。
- **系统字体**：零下载，但 Windows 上的微软雅黑与 macOS 的苹方观感差别大，界面在各平台不一致。
- **参考组件库直接复制**：beUI（MIT）可以改写后使用并在 NOTICE 注明；BoardUI 免费部分是 MIT，但底层是 React Aria，只借结构；LeiBox（MIT + Commons Clause）与 bencho（付费）只借鉴想法，不复制代码。

## 影响

- 发行包与镜像增加 114 个字体文件、共 4.9 MB（P0 实测）。首次访问时只下载页面用到的切片：首页共 15 个文件、842 KB，其中思源黑体 13 片约 750 KB；之后由浏览器长期缓存。`NOTICE` 列出三种字体的协议。P5 再评估是否改用静态字重或常用字子集。
- 内置主题的 token 换成新的一套；第三方主题（目前还没有）沿用 shadcn 名称即可继续工作，`highlight` 等新 token 不写时取默认值。
- `@skinloom/ui` 的公开导出会在 P1 大幅增加；插件 API 在 2.0 冻结前允许调整。
- 后端 bootstrap 中皮肤库菜单的 `icon` 从 `shirt` 变为 `layout-grid`。
