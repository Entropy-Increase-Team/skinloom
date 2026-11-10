# SkinLoom 设计系统

> 状态：草案（2026-11-09），随 [设计文档 07 前端重构方案](../docs/design/07-frontend-redesign.md) 一起评审。
>
> 本文是 SkinLoom 前端界面的唯一依据：颜色、字体、间距、图标、组件怎么选、页面怎么搭。写界面之前先查这里；要改规则，先改这里再改代码。
> 文中的数值是**默认主题**的值。站长的主题可以改 token 的值，但不能改 token 的含义（第 14 节）。

## 0. 方向

| | |
|---|---|
| 产品 | 自托管的 Minecraft 皮肤站：玩家上传、收藏皮肤并给角色换装，站长一键部署、管理和定制自己的站 |
| 性格 | 手作的、精确的、带一点玩心 |
| 世界 | 织机与像素。64×64 的皮肤贴图是一格一格"织"出来的；默认主题的颜色取自蓝染布：靛蓝为主色，金线作强调色 |
| 高光时刻 | ① 皮肤本身：3D 预览、2D 渲染、角色身上穿着的样子；② 后台概览的主图表 |
| 标志性细节 | **像素格**：皮肤与披风的预览一律放在淡淡的棋盘格底板上（像图像编辑器显示透明区域），别处不用 |

一句话：界面本身安静克制，颜色和细节留给皮肤。

## 1. 原则

1. **内容是主角，界面退后。** 导航、按钮、表格用中性色；有颜色、有质感的只有皮肤预览和少数关键数字。
2. **一屏一个重点。** 每屏只有一个主操作（实心主色按钮），其余操作用次要样式或收进菜单。
3. **同样的东西长一样。** 同一种列表的行高、卡片的内边距、图标的尺寸处处一致；能用组件就不手写样式。
4. **先服务端渲染，再增强。** 公开页没有 JavaScript 也能读；筛选、搜索用普通链接和 GET 表单，交互是在此基础上的增强。
5. **主题可改，含义不变。** 组件只用 token（`bg-primary`、`text-muted-foreground`），不写死颜色；token 的名字和含义是主题的公开契约。
6. **移动端是一等公民。** 每个页面都要在 375px 宽度下没有横向滚动、点得到、读得清。

## 2. 颜色

### 2.1 五种颜色

整个界面只用下面五种颜色，每种都有固定的用途：

| | 颜色 | 用途 | 不用于 |
|---|---|---|---|
| 1 | **中性色阶**（带极淡靛蓝的灰） | 背景、卡片、边框、三级文字 | — |
| 2 | **靛蓝**（主色 `primary`） | 主操作按钮、选中状态、链接、焦点环、进度、主图表 | 大面积背景、装饰 |
| 3 | **金线**（强调色 `highlight`） | 积分与签到、"热门"排名、需要提醒的地方（警告） | 按钮、文字、普通徽章 |
| 4 | **绿**（`success`） | 成功、正常、已通过 | 品牌、装饰 |
| 5 | **红**（`destructive`） | 错误、危险操作、驳回 | 收藏、点赞 |

- 警告（warning）复用金线色，信息提示（info）复用靛蓝，不再引入第六种颜色。
- 图表最多用靛蓝 + 金线两个色相，其余系列用中性色阶的不同深浅。
- **不用渐变**：不在背景、按钮、文字上用色相到色相的渐变，不用彩色光晕和彩色阴影。唯一允许的是同一颜色内的明暗过渡。

### 2.2 Token

名称沿用 shadcn/ui，另加 SkinLoom 的几个（标 ★）。下表是默认主题的值，均已核对对比度（见 2.4）。

**亮色**

| Token | 值 | 约合 | 用途 |
|---|---|---|---|
| `background` | `oklch(0.985 0.002 262)` | `#f9fafb` | 页面背景 |
| `foreground` | `oklch(0.21 0.014 262)` | `#15181f` | 一级文字 |
| `card` / `popover` | `oklch(1 0 0)` | `#ffffff` | 卡片、弹层 |
| `card-foreground` / `popover-foreground` | 同 `foreground` | | |
| `muted` | `oklch(0.965 0.005 262)` | `#f2f3f7` | 凸起面：悬停行、输入框底、预览底板 |
| `muted-foreground` | `oklch(0.45 0.016 262)` | `#50555e` | 二级文字：说明、表格次要列 |
| ★`faint-foreground` | `oklch(0.545 0.014 262)` | `#6c7079` | 三级文字：时间、计数、元信息 |
| `secondary` | 同 `muted` | | 次要按钮底色 |
| `secondary-foreground` | 同 `foreground` | | |
| `accent` | 同 `muted` | | **shadcn 含义**：菜单项、幽灵按钮的悬停底色。不是品牌强调色 |
| `accent-foreground` | 同 `foreground` | | |
| `primary` | `oklch(0.47 0.13 262)` | `#3057a3` | 靛蓝 |
| `primary-foreground` | `oklch(0.985 0.002 262)` | `#f9fafb` | 靛蓝上的文字 |
| ★`highlight` | `oklch(0.80 0.14 80)` | `#edb345` | 金线 |
| ★`highlight-foreground` | `oklch(0.30 0.06 60)` | `#43260a` | 金线上的文字 |
| ★`success` | `oklch(0.52 0.13 155)` | `#007e46` | 成功 |
| ★`success-foreground` | `oklch(0.985 0.002 262)` | | 成功色上的文字 |
| `destructive` | `oklch(0.55 0.20 27)` | `#cc2827` | 危险 |
| ★`destructive-foreground` | `oklch(0.985 0.002 262)` | | 危险色上的文字 |
| `border` | `oklch(0.21 0.014 262 / 10%)` | 约 `#e8e8e9` | 发丝线 |
| `input` | `oklch(0.21 0.014 262 / 14%)` | 约 `#dedfe0` | 输入框边框 |
| ★`overlay` | `oklch(0.21 0.014 262 / 30%)` | | 对话框、抽屉背后的遮罩 |
| `ring` | 同 `primary` | | 焦点环（用时 50% 透明） |
| `chart-1` | 同 `primary` | | 主系列 |
| `chart-2` | 同 `highlight` | | 需要突出的第二系列 |
| `chart-3`…`chart-5` | `foreground` 的 45% / 28% / 16% | | 其余系列 |
| `sidebar` 等 `sidebar-*` | `sidebar` = `background`，其余同对应的通用 token | | 用户中心、后台侧栏 |
| `radius` | `0.625rem` | 10px | 圆角基准（第 5 节） |

**暗色**

| Token | 值 | 约合 |
|---|---|---|
| `background` | `oklch(0.165 0.008 262)` | `#0c0e12` |
| `card` / `popover` | `oklch(0.20 0.010 262)` | `#14161b` |
| `muted` / `secondary` / `accent` | `oklch(0.245 0.012 262)` | `#1d2126` |
| `foreground` | `oklch(0.95 0.004 262)` | `#edeef1` |
| `muted-foreground` | `oklch(0.75 0.014 262)` | `#a9aeb7` |
| ★`faint-foreground` | `oklch(0.64 0.014 262)` | `#878c95` |
| `primary` | `oklch(0.74 0.11 262)` | `#85abf0` |
| `primary-foreground` | `oklch(0.18 0.03 262)` | `#0a111f` |
| ★`highlight` | `oklch(0.82 0.13 82)` | `#eebb58` |
| ★`highlight-foreground` | `oklch(0.26 0.05 60)` | `#351e08` |
| ★`success` | `oklch(0.72 0.14 155)` | `#4ebe7d` |
| ★`success-foreground` / ★`destructive-foreground` | 同 `background` | （暗色下浅色填充配深色字） |
| `destructive` | `oklch(0.68 0.18 25)` | `#f3625d` |
| `border` | `oklch(0.95 0.004 262 / 10%)` | 约 `#292c30` |
| `input` | `oklch(0.95 0.004 262 / 14%)` | 约 `#323439` |
| ★`overlay` | `oklch(0 0 0 / 60%)` | |

**两种模式共用**

| Token | 默认值 | 用途 |
|---|---|---|
| ★`viewer.background` → `--sl-viewer-background` | `transparent`（显示像素格底板） | 3D 预览背景，任意 CSS 背景值 |
| ★`pixel-a` / ★`pixel-b` | `muted` 与 `card` | 像素格底板的两种格子颜色 |
| ★`shadow-popover` | 亮：`0 8px 24px oklch(0.21 0.014 262 / 8%), 0 1px 2px oklch(0.21 0.014 262 / 6%)`；暗：`0 16px 48px oklch(0 0 0 / 50%)` | 下拉、弹层 |
| ★`shadow-dialog` | 亮：`0 16px 48px oklch(0.21 0.014 262 / 12%)`；暗：同 `shadow-popover` | 对话框、抽屉 |
| `brand-gradient` | **保留但默认主题不再使用**（值等于 `primary`） | 兼容旧主题 |

### 2.3 用法

- **主色**每屏出现几次：主按钮、当前选中的导航或分段、链接、焦点环。卡片、标题、图标都不用主色。
- **金线**只出现在有含义的地方：积分数字旁的 `coins` 图标、签到连续天数、排行前三名的名次、警告提示的图标和左边线。
- **语义色**做底色时用 10–15% 透明度的淡底（`bg-destructive/10`）配同色文字；只有确认危险操作的按钮用实心红。
- **收藏**的选中状态用主色实心的 `heart`，不用红色。红色只表示"出错"和"危险"。
- **文字**只有三级：`foreground`（标题、正文）、`muted-foreground`（说明、次要信息）、`faint-foreground`（时间、计数、元信息）。不要用透明度临时调出第四级。
- **边框**是发丝线，深色模式靠表面的明暗层级表达深度，不靠阴影。
- 写死的颜色（`bg-white`、`text-slate-900`、`#fff`）不允许出现在组件里。皮肤预览图片本身除外。

### 2.4 对比度要求

每个主题都要满足以下要求；`theme.contrast.test.ts` 会对内置主题逐项检查，`skinloom theme check` 对站长主题给出警告。

| 组合 | 最低 | 默认主题（亮 / 暗） |
|---|---|---|
| `foreground` / `background` | 7:1 | 16.97 / 16.65 |
| `muted-foreground` / `card`、`muted` | 4.5:1 | 7.44、6.72 / 8.14、7.29 |
| `faint-foreground` / `card`、`background` | 4.5:1 | 4.65、4.75 / 5.38、5.73 |
| `primary-foreground` / `primary` | 4.5:1 | 6.65 / 8.12 |
| `primary` 作文字 / `card` | 4.5:1 | 6.95 / 7.81 |
| `highlight-foreground` / `highlight` | 4.5:1 | 7.31 / 8.90 |
| `success-foreground` / `success` | 4.5:1 | 4.95 / 8.24 |
| `destructive-foreground` / `destructive` | 4.5:1 | 5.37 / 6.16 |
| `ring` / `background`（非文字） | 3:1 | 6.65 / 8.31 |

## 3. 字体

### 3.1 字体家族

| 角色 | 字体 | 协议 | 加载 |
|---|---|---|---|
| 界面（英文、数字） | **Inter**（可变字体） | SIL OFL 1.1 | `@fontsource-variable/inter`，只取 latin 与 latin-ext 子集 |
| 界面（中文） | **思源黑体 / Noto Sans SC**（可变字体） | SIL OFL 1.1 | `@fontsource-variable/noto-sans-sc`，按 `unicode-range` 切片，页面用到哪些字只下载哪几片 |
| 代码、哈希、UUID、令牌 | **JetBrains Mono**（可变字体） | SIL OFL 1.1 | `@fontsource-variable/jetbrains-mono`，只取 latin 子集 |

```css
--sl-font-sans: 'Inter Variable', 'Noto Sans SC Variable', 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif;
--sl-font-mono: 'JetBrains Mono Variable', ui-monospace, 'SFMono-Regular', Consolas, monospace;
--sl-font-display: var(--sl-font-sans); /* 主题可换成标题专用字体，只用于首页大标题 */
```

- Inter 排在前面：英文和数字由 Inter 显示，中文落到思源黑体。两者的字重和 x-height 接近，混排时不跳。
- 字体文件随前端构建进 `/_next/static/media`，由 Go 网关直接提供，不依赖任何外部 CDN（国内访问 Google Fonts 不可靠），也满足 CSP 的 `font-src 'self'`。
- `font-display: swap`；中文切片不预加载。
- 不用 HarmonyOS Sans、MiSans、阿里巴巴普惠体：前两者的协议限制单独再分发或修改（网页切片就算修改），放进开源仓库和发行包有风险。站长可以在自己的主题里换用（第 14 节）。
- 发行包的 `NOTICE` 列出三种字体及其 OFL 协议。

### 3.2 字号

保留 Tailwind 的默认字号（`text-xs` 12、`text-sm` 14、`text-base` 16 ……），另加两个：

| 用途 | 字号 / 行高 | 字重 | Tailwind |
|---|---|---|---|
| 元信息、计数、徽章、表头 | 12 / 16 | 400–500 | `text-xs` |
| 说明文字、辅助提示、密集表格 | 13 / 20 | 400 | ★`text-caption` |
| **界面基准**：正文、导航、按钮、输入框 | 14 / 22 | 400；按钮、导航 500 | `text-sm` |
| 阅读文字：公告、Markdown、材质说明 | 15 / 26（1.75） | 400 | ★`text-body` |
| 卡片标题、对话框标题 | 16 / 24 | 500 | `text-base` |
| 区块标题（h2） | 18 / 28 | 500 | `text-lg` |
| 应用页标题（用户中心、后台） | 24 / 32 | 500 | `text-2xl` |
| 公开页标题（皮肤库、材质名） | 30 / 38 | 500 | `text-3xl` |
| 统计数字 | 28–30 / 1 | 500，等宽数字 | `text-3xl tabular-nums` |
| 首页大标题 | 桌面 48 / 56，手机 32 / 40 | 600 | `text-5xl`、`max-sm:text-3xl` |

- 字重只用 400、500，600 只给首页大标题。不用 700 以上：中文笔画多，粗体在小字号下糊成一团。
- 层级靠字号和颜色区分，不靠加粗。
- 中文正文行高不低于 1.6，阅读文字 1.75；英文为主的界面文字 1.5 左右。
- 字距：16px 以下为 0；40px 以上的标题可以收到 `-0.02em`。中文不加负字距。
- 不用斜体表示强调（中文没有真正的斜体）；不用全大写加宽字距的标签。

### 3.3 数字与代码

- 统计数字、表格里的数字、积分、存储用量都加 `tabular-nums`，并用 `Intl.NumberFormat` 加千分位。同一屏的数字格式一致。
- 哈希、UUID、令牌、命令、文件名用 `font-mono`，字号比周围小一级（`text-xs` 或 `text-caption`），过长时中间省略并提供复制按钮。
- 时间：列表里显示相对时间（"3 分钟前"），悬停显示完整时间；详情里显示日期（`dateStyle: 'medium'`）。

### 3.4 中英混排

- 中文与英文、数字之间加一个半角空格："上传 3 个材质"、"由 SkinLoom 驱动"。符号单位紧跟数字（64×64、30%），字母单位前加空格（3 KB）。
- 中文里用全角标点，引用界面元素或名称用「」：删除角色「Steve」？
- 可以用 CSS `text-autospace` 作渐进增强，但文案本身仍要手动加空格。

## 4. 间距与尺寸

一律在 **4px 网格**上（Tailwind 的 1 = 4px）。禁止 `mt-[13px]` 这类零散值。

| 用途 | 值 |
|---|---|
| 控件内部 | 4–12 |
| 同一组内的项目（列表行之间） | 4–8 |
| 组与组之间（标签与列表、卡片之间） | 16–24 |
| 页面区块之间 | 应用页 32；公开页 64–96 |
| 卡片内边距 | 默认 20（`p-5`），紧凑 16，大卡片 24 |
| 页面边距 | 手机 16，平板 24，桌面 32 |
| 顶栏高度 | 56 |
| 侧栏宽度 | 248（收起后 56） |
| 内容最大宽度 | 公开页 1200；应用页 1440；阅读文字 720；单列表单 640 |

**控件高度**

| 尺寸 | 高度 | 用在 |
|---|---|---|
| `sm` | 32 | 表格行内操作、筛选栏、密集工具条 |
| `default` | 36 | 绝大多数按钮和输入框 |
| `lg` | 40 | 首页主按钮、登录注册表单、手机上的主操作 |
| 触屏 | ≥ 40 | `pointer-coarse:` 变体把 `default` 提到 40 |

列表行高：紧凑 36、默认 44、带缩略图 64。同一列表只用一种。

## 5. 圆角、边框与阴影

圆角由 `--radius`（默认 10px）推导，主题只需改这一个值：

| Tailwind | 计算 | 默认 | 用在 |
|---|---|---|---|
| `rounded-sm` | `radius × 0.6` | 6 | 徽章、键帽、头像 |
| `rounded-md` | `radius × 0.8` | 8 | 按钮、输入框、菜单项 |
| `rounded-lg` | `radius` | 10 | 弹层、下拉、提示框 |
| `rounded-xl` | `radius × 1.4` | 14 | 卡片 |
| `rounded-2xl` | `radius × 1.8` | 18 | 对话框、抽屉、大面板 |

- **嵌套圆角**：内层 = 外层 − 两者之间的间距，最小 4。卡片（14）里留 4px 边的预览图圆角是 10。
- 不是所有东西都是胶囊形：只有分段控件的选中块、计数气泡用全圆角。
- **头像是方的**：Minecraft 头像是像素方块，一律 `rounded-sm`，不裁成圆形。
- 卡片默认只有边框、没有阴影；可点击的卡片悬停时边框加深并换成 `muted` 底，不上浮、不放大。
- 阴影只给浮在页面上的东西：下拉与弹层用 `shadow-popover`，对话框与抽屉用 `shadow-dialog`。

## 6. 动效

```css
--ease-out: cubic-bezier(0.22, 1, 0.36, 1);
--duration-fast: 150ms;  /* 悬停、按下、开关 */
--duration-base: 200ms;  /* 菜单、弹层、提示 */
--duration-slow: 320ms;  /* 抽屉、对话框、页面进入 */
```

- 只过渡 `color`、`background-color`、`border-color`、`opacity`、`transform`，写明属性（`transition-colors`），不用 `transition-all`。
- 按下缩到 0.98；弹层从触发点方向淡入并位移 4–8px；数字变化可以补间。
- 页面加载最多一次进入动画（主内容淡入上移 8px）。不做循环动画（加载指示除外），不做视差，卡片悬停不放大。
- 3D 预览默认慢速自转和走路动画；`prefers-reduced-motion: reduce` 时都停掉，只能手动拖动。
- 不引入 motion / framer-motion：CSS 过渡加 shadcn 自带的 `tw-animate-css` 就够了。

## 7. 图标

只用 **lucide**（`lucide-react`）。

- 尺寸：行内与按钮 16（`size-4`），导航 16，空状态 24。同一处只用一种尺寸。
- 线宽：全站 1.75（全局 CSS `.lucide { stroke-width: 1.75 }`，插件里的 lucide 图标也跟着变）。1.75 与 14px 思源黑体常规体的笔画粗细最接近。
- 颜色跟随所在文字的颜色（`currentColor`），导航里是 `muted-foreground`，选中是 `foreground`。只有提示里的状态图标用语义色，积分的 `coins` 用金线色。
- 图标与文字间距 8（`gap-2`），同一列表里图标占位宽度一致，文字左对齐在一条线上。
- 不加只为装饰的图标：卡片标题、区块标题、分类标签前面都不放图标。皮肤类型（经典、纤细、披风）用文字，不硬找图标。
- 纯图标按钮必须有 `aria-label`，并配 Tooltip。

### 7.1 概念与图标

同一个概念全站只用同一个图标：

| 概念 | 图标 | | 概念 | 图标 |
|---|---|---|---|---|
| 首页 | `house` | | 皮肤库 | `layout-grid` |
| 衣柜 | `shirt` | | 收藏 / 收藏数 | `heart` |
| 角色（游戏角色） | `person-standing` | | 用户（账号） | `users-round` |
| 上传 | `upload` | | 下载 | `download` |
| 概览 / 仪表盘 | `layout-dashboard` | | 通知 | `bell` |
| 公告 | `megaphone` | | 举报 | `flag` |
| 积分 | `coins` | | 签到 | `calendar-check` |
| 存储用量 | `hard-drive` | | 个人资料 | `user-cog` |
| 登录设备 | `monitor-smartphone` | | 第三方账号 | `link-2` |
| 外置登录 / 游戏内使用 | `gamepad-2` | | 开发者（OAuth 应用） | `code-xml` |
| 访问令牌 | `key-round` | | 审计日志 | `scroll-text` |
| 外观 | `palette` | | 翻译 | `languages` |
| 设置 | `settings` | | 系统状态 | `activity` |
| CDN | `cloud` | | 备份 | `database-backup` |
| 3D 预览 | `rotate-3d` | | 2D 预览 | `image` |
| 公开 | `globe` | | 私密 | `lock` |
| 编辑 | `pencil` | | 删除 | `trash-2` |
| 复制 / 已复制 | `copy` → `check` | | 更多操作 | `ellipsis` |
| 搜索 | `search` | | 筛选 | `list-filter` |
| 排序 | `arrow-up-down` | | 刷新 | `refresh-cw` |
| 亮色 / 暗色 / 跟随系统 | `sun` / `moon` / `monitor` | | 语言 | `languages` |
| 登录 / 退出 / 注册 | `log-in` / `log-out` / `user-plus` | | 菜单（手机） | `menu` |
| 外部链接 | `external-link` | | 邮箱验证 | `mail-check` |
| 成功 / 错误 / 警告 / 信息 | `circle-check` / `circle-x` / `triangle-alert` / `info` | | 加载中 | `loader-circle`（旋转） |

### 7.2 菜单图标注册表

后端和插件下发的菜单项用字符串指定图标（`MenuItem.icon`，lucide 的短横线名称）。`@skinloom/ui` 维护一张**允许的图标表**（上表全部图标，按需增加），把名称映射到组件；不在表里的名称不显示图标，但保留占位，让文字仍然对齐。不做"按名称动态导入全部 lucide"，否则整个图标库会进入首屏。

## 8. 标志性细节：像素格

所有皮肤、披风的预览都放在**像素格底板**上：

- 8px 一格的棋盘格，两种格子颜色是 `--pixel-a`（默认 `muted`）和 `--pixel-b`（默认 `card`），对比很弱，像图像编辑器显示透明区域。它是有功能的：皮肤的透明部分（第二层、披风镂空）一眼可见。
- 2D 预览图加 `image-rendering: pixelated`，按整数倍缩放，保证像素锐利。
- 3D 预览的画布是透明的，底下同样是像素格；主题可以用 `viewer.background` 换成别的背景。
- 只用在这里：材质卡片、材质详情、上传预览、角色卡片、材质选择器。页面背景、按钮、空状态都不用像素格。
- 组件：`PixelBackdrop`（第 10.3 节）。主题可以通过 `.sl-pixel-backdrop` 重新设计。

### 8.1 皮肤的两种视图

| 视图 | 样子 | 来源 | 用在 |
|---|---|---|---|
| **双面** | 正面与背面并排的平面 2D 图，没有透视和光影 | 后端 `/preview/{tid}`（`render.SkinPreview`，36:32） | 皮肤库与首页的材质卡片、衣柜、角色卡片、材质选择器、举报队列缩略图 |
| **单面** | 一个立体的角色，可以行走、旋转 | 前端 skinview3d（`SkinViewer`） | 首页展示区、材质详情、上传预览 |

- 列表里一律用双面：一眼看到正反两面，不用点进去；同一网格里的人物高度一致。
- 需要"活起来"的地方用单面：首页展示区行走轮播，材质详情可以拖动旋转。
- 披风只有一种视图：后端 `CapePreview` 画的外侧（从背后看到的一面）。
- 头像（`/avatar`）是头部正面，属于第三种用途，不算视图。

## 9. 布局

### 9.1 断点

沿用 Tailwind：`sm` 640、`md` 768、`lg` 1024、`xl` 1280。

| 宽度 | 行为 |
|---|---|
| < 768 | 顶栏导航收进 `menu` 按钮（Sheet）；侧栏收进 Sheet；对话框变成底部抽屉；表格变成卡片列表 |
| 768–1023 | 顶栏完整显示；侧栏默认收起为图标栏 |
| ≥ 1024 | 侧栏展开 |

### 9.2 四种外壳

**公开区**（首页、皮肤库、材质详情、关于）：顶栏 + 内容 + 页脚。

```
┌ 顶栏 56 ───────────────────────────────────────────────────────────┐
│ [标志] 站名    皮肤库  关于  (插件菜单)        [搜索] [🔔] [头像 ▾]   │  已登录
│ [标志] 站名    皮肤库  关于                    [语言/主题 ▾] 登录 [注册] │  未登录
└────────────────────────────────────────────────────────────────────┘
```

- 已登录时，个人入口（概览、角色、衣柜、通知、资料、开发者、后台、语言与主题、退出）收进头像下拉菜单。顶栏只留通知铃铛和头像。
- 未登录时，"登录"是文字链接，"注册"是唯一的实心按钮。
- 语言和亮暗模式合并成一个设置下拉（`languages` 图标）。
- 手机：标志 + 站名 + 右侧 `menu` 按钮；菜单是从右侧滑出的 Sheet，里面是导航、账号入口和语言主题。

**用户中心**（`/user/*`）与**后台**（`/admin/*`）：共用顶栏 + 左侧栏 + 内容。

```
┌ 顶栏（同上）──────────────────────────────────────────────────────┐
├──────────────┬───────────────────────────────────────────────────┤
│ 概览          │  页面标题                              [主操作]    │
│              │  说明文字                                          │
│ 我的          │  ─────────────────────────────────────────────── │
│   角色        │                                                   │
│   衣柜        │  内容（流式宽度，最大 1440）                         │
│   上传材质    │                                                   │
│ 账号          │                                                   │
│   资料与安全  │                                                   │
│   通知        │                                                   │
│   我的举报    │                                                   │
│ 开发者        │                                                   │
│   应用与令牌  │                                                   │
└──────────────┴───────────────────────────────────────────────────┘
```

- 侧栏用 shadcn `Sidebar`：导航行高 32，行间距 2，分组标题上方 24、下方 8；图标一列对齐。
- 后台分组：概览；内容（用户、角色、举报、审计日志）；站点（外观、翻译、设置）；系统（系统状态）。
- 插件通过 `menus.user`、`menus.admin` 加入的菜单排在对应分组后面。
- 主题可以用布局变体 `user: tabs` 把用户中心改成顶部标签页（第 14 节）。

**认证页**（登录、注册、找回密码、首次安装）：保留 `card`（居中卡片）和 `split`（左侧面板 + 右侧表单）两种变体。`split` 的面板 `.sl-auth-panel` 默认是 `muted` 底 + 像素格 + 站内几个真实皮肤的 2D 渲染。

### 9.3 页面骨架

每个应用页都从 `PageHeader` 开始：标题（`text-2xl`）、一行说明（`muted-foreground`）、右侧至多一个主操作和一个次要操作。标题直接写页面名，比如"角色""衣柜"，不写"欢迎回来"之类的问候。

### 9.4 移动端

- 375px 下没有横向滚动；长串（哈希、邮箱、URL）用 `break-all` 或中间省略。
- 触控目标不小于 40×40；行内的小按钮在触屏上扩大点击区域。
- 底部固定操作条（材质详情的"收藏"、设置页的"保存"）留出 `env(safe-area-inset-bottom)`。
- 宽表格先改成卡片列表；确实需要表格时只允许表格自身横向滚动，第一列固定。
- 对话框用响应式写法：`md` 以上是 `Dialog`，以下是 `Drawer`（底部抽屉）。
- 筛选栏在手机上：分类改为可横向滚动的分段控件，排序和搜索收进"筛选"抽屉。
- 3D 预览在手机上高度 60vh，支持单指旋转，不劫持页面滚动（竖向滑动仍然滚动页面）。
- 首页展示区在手机上压缩到约 224px 高：只留皮肤本身和上下少量空间，缩略图换成圆点。

### 9.5 首页展示区（行走 + 轮播）

首页的高光时刻：站内最新的公开皮肤轮流"走"出来。

| | 规则 |
|---|---|
| 当前皮肤 | skinview3d 单面视图，播放行走动画（速度 0.6），朝向固定在正面偏左 30°，不自转；拖动可旋转，松手 400ms 回到默认朝向 |
| 相邻皮肤 | 前一个、后一个静止站立，透明度 28%，比当前小约 40%；与当前皮肤在同一个 3D 画布里（场景中加额外的 PlayerObject），只占一个 WebGL 上下文 |
| 轮播 | 每 6 秒切换，最多 5 个；当前皮肤向左淡出并位移 24px，下一个从右侧淡入，`--duration-slow`，`--ease-out`；名称和上传者同步淡入 |
| 进度 | 当前缩略图描主色边，下方 3px 进度条随 6 秒填满；手机上当前圆点拉长为 16px |
| 交互 | 点缩略图跳转，左右按钮切换；悬停或键盘焦点在展示区内时暂停；暂停按钮切换播放；点角色进入材质详情 |
| 减少动效 | 不行走、不自动轮播，只显示静止姿势，仍可手动切换 |
| 性能 | 滚出视口或页面不可见时停止渲染；设备像素比最多 2；首屏先显示服务端渲染的 2D 图，3D 就绪后淡入替换 |
| 空站 | 没有公开皮肤时，hero 用 `centered` 版式，不显示展示区 |

## 10. 组件

### 10.1 分层

| 层 | 位置 | 内容 | 谁能用 |
|---|---|---|---|
| 基础组件 | `packages/ui/src/components/ui/` | shadcn/ui（Base UI 版），按本文调整样式 | 宿主与插件（`@skinloom/ui` 导出） |
| 业务组件 | `packages/ui/src/components/skinloom/` | 皮肤站特有的组件：材质卡片、预览、选择器 | 宿主与插件 |
| 页面组件 | `apps/web/src/components/` | 只在某个页面用的组合 | 只有宿主 |

`@skinloom/ui` 导出的每个组件都是插件 API：新增随意，改名和删除走插件 API 的弃用流程。

### 10.2 基础组件

来自 shadcn/ui，改动只限于本文规定的尺寸、圆角、颜色；行为与无障碍保持原样。

| 类别 | 组件 |
|---|---|
| 操作 | `Button`、`ButtonGroup`、`Toggle`、`ToggleGroup`（作分段控件） |
| 表单 | `Field`（含 Label、Description、Error）、`Input`、`InputGroup`、`Textarea`、`Select`、`Combobox`、`Checkbox`、`RadioGroup`、`Switch`、`Slider` |
| 弹层 | `Dialog`、`AlertDialog`、`Sheet`、`Drawer`、`Popover`、`DropdownMenu`、`Tooltip`、`Command` |
| 反馈 | `Alert`、`Toast`（sonner）、`Progress`、`Skeleton`、`Spinner`、`Empty` |
| 展示 | `Card`、`Badge`、`Avatar`、`Item`、`Kbd`、`Separator`、`Table`、`Accordion`、`Collapsible`、`ScrollArea` |
| 导航 | `Sidebar`、`Tabs`、`Breadcrumb`、`Pagination` |
| 图表 | `Chart`（Recharts，只在后台按需加载） |

**怎么选**

| 场景 | 用 | 不用 |
|---|---|---|
| 一个页面的主操作 | `Button`（default，实心靛蓝），一屏一个 | 一排实心按钮 |
| 次要操作 | `Button variant="outline"` 或 `secondary` | |
| 工具栏、表格行内的操作 | `Button variant="ghost" size="sm"`；超过 2 个收进 `DropdownMenu`（`ellipsis`） | 一行五个图标按钮 |
| 不可撤销的操作（删除、注销） | `AlertDialog`：标题是问句，正文说后果，确认按钮写动作（"删除"），实心红 | `window.confirm`、写"确定" |
| 特别严重的操作（注销账号、删除用户） | `AlertDialog` + 输入名称确认 | |
| 编辑一条记录 | 字段少用 `Dialog`；字段多或要对照列表用 `Sheet`（右侧） | 在表格行里展开表单 |
| 2–5 个互斥选项且常切换（分类、排序、视图） | `ToggleGroup`（分段控件） | 下拉 |
| 选项多或不常切换 | `Select`；可搜索时用 `Combobox` | 原生 `<select>` |
| 开关一个设置，立即生效 | `Switch` | 复选框 |
| 表单里勾选同意、多选 | `Checkbox` | |
| 操作结果（已保存、已复制） | `Toast`，3 秒后消失 | 页面里一行字 |
| 需要停留阅读的错误、警告 | 表单顶部或区块内的 `Alert` | Toast |
| 字段校验错误 | `Field` 的 Error，紧贴字段 | Toast |
| 首次加载 | `Skeleton`，形状同最终内容 | 整页转圈 |
| 按钮提交中 | 按钮内 `Spinner` + 禁用，文字不变 | 改成"加载中…" |
| 没有数据 | `Empty`：一句说明 + 一个操作 | 空白 |
| 简单的只读列表 | `Table` | |
| 可排序、筛选、分页、批量操作 | `DataTable`（`Table` + TanStack Table） | |
| 键盘快捷键 | `Kbd` | |

**Button 变体**：`default`（主色）、`secondary`、`outline`、`ghost`、`destructive`（实心红，只用于确认危险操作）、`destructive-soft`（淡红，列表里的删除）、`link`；尺寸 `xs`、`sm`、`default`、`lg`、`icon`、`icon-sm`、`icon-lg`。加载状态 `loading` 属性：显示 `Spinner` 并禁用。

**看起来像按钮的链接**用 `ButtonLink`（`<ButtonLink render={<Link href="/skinlib" />}>`），不要用 `Button` 渲染链接：`Button` 会给元素加上 `role="button"`，屏幕阅读器就读不出它是链接。弹层的触发器则相反，用 `render={<Button variant="outline" />}` 把按钮交给触发器。

### 10.3 业务组件

| 组件 | 说明 |
|---|---|
| `PixelBackdrop` | 像素格底板（第 8 节），包住任何皮肤预览 |
| `TexturePreview` | 双面视图（后端 `/preview/{tid}`，8.1），整数倍缩放，`pixelated`；按高度对齐，同一网格里人物一样高 |
| `SkinViewer` | 单面视图：3D 预览（skinview3d，懒加载）。工具条：2D/3D 切换、动画开关、重置视角；尊重减少动效 |
| `SkinCarousel` | 首页展示区：当前皮肤行走，前后两个静止并半透明，自动轮播 + 头像缩略图与进度条（手机上是圆点）。规则见 9.5 |
| `TextureCard` | 皮肤库卡片：预览 + 名称 + 上传者头像与昵称 + 收藏数；私密时显示 `lock` 徽章。主题挂钩 `.sl-texture-card`，两种密度 |
| `TextureGrid` | 卡片网格，游标分页，滚到底部自动加载并保留"加载更多"按钮 |
| `TexturePicker` | 从衣柜里选材质的对话框：分类分段 + 搜索 + 网格，单选 |
| `PlayerCard` | 角色：名称、UUID（等宽、可复制）、身上的皮肤和披风预览、操作菜单 |
| `PlayerPicker` | 选择角色（下拉或对话框） |
| `UserAvatar` | 用户头像（后端 `/avatar`），方形 `rounded-sm`，尺寸 20/24/32/48 |
| `PageHeader` | 页面标题、说明和操作区（9.3） |
| `StatCard` | 统计卡：标签、数字（`tabular-nums`）、可选的变化值，变化值必须写明对比对象（"比上周多 12"） |
| `UsageMeter` | 用量条（存储、积分上限）：`Progress` + 数字，接近上限时转金线色，超过时转红 |
| `CopyField` | 只读的值 + 复制按钮（`copy` → `check`），用于 UUID、令牌、启动器地址 |
| `CodeBlock` | 多行命令或配置，带复制按钮 |
| `ConfirmProvider` / `useConfirm()` | 以 Promise 形式调用的 `AlertDialog`，替换所有 `window.confirm`；也是插件 SDK 里 `confirm()` 的实现 |
| `Dropzone` | 拖放或点击上传，显示文件预览和错误 |
| `SaveBar` | 设置类页面有未保存的修改时，在底部浮出"有 3 项未保存 · 撤销 · 保存" |
| `StatusDot` | 状态小圆点（正常绿、注意金、故障红、未知灰）+ 文字，用于系统状态、举报状态 |
| `RelativeTime` | 相对时间，悬停显示完整时间 |
| `MenuIcon` | 按名称渲染允许的菜单图标（7.2） |
| `UiLabelsProvider` | 共享组件自己显示的文字（复制、取消、未保存……），宿主按访客的语言提供一次（消息里的 `ui` 命名空间），插件不用再翻译 |
| `Markdown` | 服务端渲染好的 HTML 的容器（`.sl-markdown`），排版用 `text-body` |

### 10.4 常用组合

**表单**

- 字段竖排，标签在上，`Field` 之间间距 16，分组之间 32 并用小标题分开。
- 必填不加星号，选填在标签后加"（选填）"。
- 提交按钮放在表单末尾左对齐（设置类页面用 `SaveBar`）；只有一个主按钮。
- 后端返回的 RFC 9457 错误：字段错误映射到对应 `Field`，其余放表单顶部的 `Alert`。

**列表页**

`PageHeader` → 筛选栏（分段控件 + 搜索 + 排序）→ 内容（网格或表格）→ 分页或加载更多。筛选条件写进 URL，可以分享、刷新不丢。

**批量操作**

选中行后，表格上方的工具条换成"已选 3 项 · 操作 · 取消选择"。

**加载、错误与空**

每个异步区块都有三种状态：加载（Skeleton）、错误（一句说明 + "重试"按钮）、空（`Empty`）。不能只处理成功的情况。

## 11. 文案

- 用"你"称呼用户；语气平实，像在说明，不推销。
- 不用感叹号；按钮、标签、标题、提示末尾不加句号，只有多句的段落保留句号。
- 按钮写动作："上传材质""保存""删除角色"，不写"确定""提交"。
- 确认框：标题问句"删除角色「Steve」？"，正文写后果"删除后，使用该角色的启动器将无法显示皮肤，退还 10 积分。"
- 错误信息说清楚发生了什么、怎么办："名称已被占用，换一个试试"，不写"操作失败"。
- 空状态一句话 + 一个操作："衣柜里还没有材质 · 去皮肤库看看"。
- 占位符短："搜索"，不写"请输入关键词进行搜索…"。
- 数字的变化写明对比对象："比上周多 12 个"。
- 首页文案用站点自己的话（站名、站点描述），不写"开启你的创作之旅"这类空话。
- 不加 "New"、"Beta" 之类的徽章，除非功能确实如此。

## 12. 无障碍

- 所有交互元素可用键盘操作，焦点环清晰（`ring` 3px，50% 透明）。
- 对比度满足 2.4。
- 纯图标按钮有 `aria-label`；装饰图标 `aria-hidden`。
- 3D 预览有 `role="img"` 和描述（"「鲨鲨」的 3D 预览"），并提供 2D 预览作为替代。
- 状态变化（已保存、加载失败）通过 Toast 的 `aria-live` 播报。
- 尊重 `prefers-reduced-motion` 与 `prefers-color-scheme`。

## 13. 写组件时的约定

- 只用 token 类名（`bg-card`、`text-muted-foreground`、`border-border`），不用 Tailwind 调色板颜色（`slate-*`、`white` 等）。`bun run test` 中的样式检查会拒绝这类类名。
- 每个组件根元素带 `data-slot="<名称>"`（shadcn 约定），主题可以用它定位。
- 尺寸、间距只用 4 的倍数。
- 组件不读全局状态；数据通过属性传入，或使用 TanStack Query 的钩子（见方案文档 §5）。
- 客户端组件只在需要交互的叶子节点上加 `'use client'`。
- 组件里需要显示的固定文字从 `useUiLabels()` 取，不写死中文或英文。
- 宿主根部依次包着 `CSPProvider`（把页面的 CSP nonce 交给 Base UI 的内联脚本）、`UiLabelsProvider`、`TooltipProvider`、`ConfirmProvider` 和 `Toaster`；插件不用自己再包。
- 基础组件（`components/ui/`）保持 shadcn 的代码风格，方便用 `shadcn add --diff` 对照上游；调整只改尺寸、圆角、颜色和动效，并在提交说明里写明。

## 14. 给主题作者

主题能改的东西（详细规范见文档站"开发主题"）：

1. **Token**：第 2 节的全部 token，以及字体 `fonts.sans`、`fonts.mono`、`fonts.display` 和 `viewer.*`。名称和含义固定，值随意（但要满足 2.4 的对比度）。
2. **CSS**：主题的 `theme.css` 不放进任何 cascade layer，所以总是盖过宿主的 Tailwind 工具类，不需要 `!important`。想让自己的规则优先级更低，就包进 `@layer components`。
3. **挂钩**：稳定的类名和属性，版本化维护：

   | 挂钩 | 位置 |
   |---|---|
   | `html[data-sl-theme]`、`html[data-sl-area]`（`public`、`user`、`admin`、`auth`） | 根元素：当前主题、当前区域 |
   | `.sl-header`、`.sl-footer` | 顶栏、页脚 |
   | `.sl-sidebar` | 用户中心与后台的侧栏 |
   | `.sl-page-header` | 页面标题区 |
   | `.sl-hero`、`.sl-block-<类型>` | 首页区块 |
   | `.sl-auth-panel` | 分栏登录页的面板 |
   | `.sl-texture-card`、`.sl-pixel-backdrop`、`.sl-viewer` | 材质卡片、像素格底板、3D 预览 |
   | `.sl-markdown` | 服务端渲染的 Markdown |
   | `[data-slot="…"]` | 每个基础组件（shadcn 约定） |

4. **布局变体**：`auth`（card、split）、`skinlib`（comfortable、compact）、`user`（sidebar、tabs）、`texture`（split、stacked）。
5. **首页区块**：区块的组合与属性，以及 hero 的版式（split、centered、banner）。
6. **字体**：主题可以把字体文件放在自己的目录里，在 `theme.css` 里用 `@font-face` 引用相对路径，再在 `fonts.sans` 里引用。授权由主题作者负责。

主题**不能**改：组件的结构和行为（第二阶段的组件覆盖再开放）、不能带 JavaScript（自定义 JS 只有超级管理员能在设置里加）。

## 15. 自查清单

提交界面改动前过一遍：

- [ ] 只用了 token 颜色；一屏最多一个实心主色按钮
- [ ] 间距都是 4 的倍数；同类元素尺寸一致
- [ ] 图标来自 lucide，尺寸、线宽、颜色符合第 7 节，同一概念用同一图标
- [ ] 字重不超过 500（首页大标题除外）；数字用 `tabular-nums`
- [ ] 文案无感叹号、无句尾句号、中英文间有空格、按钮写动作
- [ ] 加载、错误、空三种状态都有
- [ ] 375px 下无横向滚动，触控目标 ≥ 40
- [ ] 亮色、暗色、织锦主题下都检查过
- [ ] 键盘可操作，焦点可见；纯图标按钮有 `aria-label`
- [ ] 没有 `window.confirm`、原生 `<select>`、手写的输入框样式

---

本文第 0 节的方向与第 2、5、6 节的部分取值参考了 unslop（Damir Sokolovsky，[@heydamir](https://x.com/heydamir)，CC BY 4.0）的方法。
