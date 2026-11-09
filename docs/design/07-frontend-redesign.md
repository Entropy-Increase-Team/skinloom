# 07 · 前端重构方案：设计系统、组件与主题

> 状态：草案（2026-11-09）。评审通过后拆成 ADR（§11），实现按 §9 的阶段推进。
> 设计规则本身（颜色、字体、间距、图标、组件怎么选）写在 [`web/design.md`](https://github.com/Entropy-Increase-Team/skinloom/blob/main/web/design.md)，本文讲为什么这样定、要改哪些东西、按什么顺序改。
> 高保真原型（Pencil）：首页（桌面与手机）、用户中心概览（亮色与暗色）、后台概览、组件与动效说明，已于 2026-11-09 评审通过。原型里的皮肤图是 AI 生成的示意，数字和用户名是编的。

## 0. 背景与目标

1.0 的后端已经完整，前端是"能用"的状态：功能齐全，但界面是 shadcn 默认样式的拼装，组件库只有 5 个组件，交互控件大量手写，手机上顶栏会溢出。主题标准（设计文档 05 §6）承诺的东西有一半没有落地，站长也没有办法放自己的主题。

这次重构的目标：

1. **一套设计系统**：颜色不超过五种，可商用的中英文字体，统一的间距、圆角、图标和动效，写进 `web/design.md`。
2. **完整的组件库**：基础组件全部采用 shadcn/ui，补齐皮肤站自己的业务组件，并作为插件 API（`@skinloom/ui`）的一部分。
3. **重做所有页面**：首页、皮肤库、用户中心、后台都按设计系统重排，移动端可用。
4. **主题系统可用**：站长可以在 `data/themes/` 放自己的主题并热加载；token、布局变体、主题设置都比现在丰富；有文档和脚手架。
5. **数据层**：引入 TanStack Query，统一加载、错误、缓存和失效。

不在本次范围：插件运行时与组件覆盖（05 §6.1 ⑤，2.0）、主题市场。

## 1. 现状审查

按 unslop 的检查表和设计文档 05 的承诺逐项看过（代码 + 本地运行截图）。

### 1.1 视觉

| 问题 | 位置 |
|---|---|
| 首页 hero 是蓝到青的色相渐变 + 斜线纹理，左对齐堆叠，没有任何能代表皮肤站的视觉 | `blocks/hero.tsx`、`themes/default/theme.css` |
| hero 按钮写死 `bg-white text-slate-900`，主题改不了 | `blocks/hero.tsx` |
| "功能介绍"是四张等宽卡片，每张一个图标 + 标题 + 一句话，模板感强 | `blocks/features.tsx` |
| 用户概览以"欢迎，xxx"作标题；五张等宽统计卡，每张都带图标 | `app/user/page.tsx` |
| 材质卡片悬停时预览放大 105% 并加阴影 | `texture/texture-card.tsx` |
| `Button` 用 `transition-all` | `packages/ui/src/components/button.tsx` |
| 页面标题 `font-bold`、卡片标题 `font-semibold`，层级靠加粗 | 多处 |
| 页面宽度在 `max-w-md`…`max-w-6xl` 之间混用，没有统一的骨架 | 多处 |
| 中文落到系统字体，Windows 上是微软雅黑，与英文的 `ui-sans-serif` 不协调 | `globals.css` |

### 1.2 结构

| 问题 | 数量 / 位置 |
|---|---|
| `@skinloom/ui` 只有 Button、Card、Badge、Input、Label | 5 个；05 §5.5 承诺约 15 个 |
| `window.confirm` | 14 处 |
| 原生 `<select>` | 14 处；同一串 `selectClass` 在 6 个文件里各写一份 |
| 原生 `<textarea>`、复选框各自手写样式 | 7 处、6 处 |
| 操作结果用页面里的一行 `role="status"` 文字，没有 Toast | 10 处 |
| 数据请求都是 `useState` + `useEffect`，加载与失败状态各管各的 | 44 个客户端组件中的大部分 |
| 手机宽度下顶栏溢出（375px 宽，页面实际 434px），没有移动端菜单 | `site-header.tsx` |
| 用户入口（概览、角色、衣柜、通知、头像、后台、退出）全部横排在顶栏 | `auth/user-menu.tsx` |
| 用户中心没有导航；后台导航是一排文字链接 | `admin/admin-nav.tsx` |
| 后端下发的 `MenuItem.icon` 前端没有渲染 | `site-header.tsx` |

### 1.3 主题系统

| 问题 | 位置 |
|---|---|
| 主题只能编进程序（`//go:embed all:default all:brocade`），站长放不了自己的主题 | `themes/embed.go`、`internal/app/core.go` |
| 布局变体只有 `auth`、`skinlib` 两项，`site`、`user` 没有实现 | `internal/modules/theme/theme.go` |
| 主题设置项（05 §6.2 `settings`）、`overrides`、`screenshot` 没有实现 | 同上 |
| 主题可依赖的类名只有 `sl-hero`、`sl-auth-panel`、`sl-markdown`、`sl-footer-text`，没有文档化的清单 | 前端 |
| 主题 CSS 与宿主 Tailwind 的优先级没有约定 | `layout.tsx` |
| token 缺少成功、警告、三级文字、阴影等，主题想改也没有入口 | `globals.css` |

## 2. 关键决定

| 决定 | 选择 | 理由 | 放弃的备选 |
|---|---|---|---|
| 基础组件 | **shadcn/ui，Base UI 底层**（`shadcn init --base base`，monorepo 模式） | shadcn 自 2026 年 7 月起默认使用 Base UI；代码在仓库里，可以按设计系统改；token 名与现有主题标准一致 | Radix 底层（维护放缓，现有代码的 `asChild` 反正要全部重写）；React Aria（BoardUI 用，但与 shadcn 生态脱节） |
| 视觉基调 | 以 shadcn 的 `nova` 风格（紧凑）初始化，再按 `design.md` 调整尺寸、圆角、颜色 | 后台和用户中心信息密度高 | `maia`（圆润宽松，不适合后台） |
| 图标 | **lucide**，统一尺寸与线宽，概念与图标一一对应，菜单图标走注册表 | 已在用；shadcn 默认；覆盖全面 | — |
| 数据层 | **TanStack Query 5**；orval 同时生成 fetch 函数（给服务端组件）与 Query 钩子（给客户端） | 05 §5.1 已把它列为插件共享依赖；统一缓存、失效、重试 | SWR；继续手写 |
| 表格 | **TanStack Table** + shadcn `Table`（即 shadcn 的 DataTable 做法） | 后台用户、角色、审计、翻译四张表都要排序、筛选、分页 | 手写 |
| Toast | **sonner**（shadcn 的 Toast 方案） | — | — |
| 图表 | shadcn `Chart`（Recharts），**只在后台按需加载** | 后台概览需要一张好看的主图表；公开页不受影响 | 继续手写 SVG（127 行，难以扩展） |
| 动效 | CSS 过渡 + `tw-animate-css`，**不引入 motion** | 设计系统只需要短促的过渡；少一个大依赖 | motion（beUI 等参考库都用它） |
| 字体 | **Inter + 思源黑体（Noto Sans SC）+ JetBrains Mono**，全部 OFL，从 npm 安装、自托管 | 可商用、可随开源发行包分发；国内不依赖 Google Fonts；切片按需加载 | HarmonyOS Sans、MiSans（协议限制再分发或修改）；系统字体（各平台观感不一） |
| 主色 | **靛蓝**（蓝染布）+ 强调色**金线** | 呼应"云织"，与织锦主题的朱红金色明显区分 | 草地绿（与成功色冲突）；保留天蓝（辨识度低） |
| 表单 | shadcn `Field` 系列 + 原生表单 + Query 的 mutation；**暂不引入表单库** | 现有表单以 FormData 为主，够用；插件的 `Form`（05 §5.5）等 2.0 设计 SDK 时再定 | react-hook-form + zod（插件 API 未定时先不绑定） |
| 组件展示 | 后台内置的**组件总览页** `/admin/appearance/preview`，不引入 Storybook | 主题作者在自己站上就能看到所有组件在当前主题下的样子 | Storybook（多一套构建，站长用不上） |

## 3. 参考组件库怎么用

用户给的四个参考，许可与用法不同，必须分开对待：

| 参考 | 许可 | 技术 | 本项目怎么用 |
|---|---|---|---|
| [beUI](https://github.com/starc007/ui-components) | MIT | Tailwind 4 + motion，shadcn registry `@beui` | **可以改写后使用**，在 `NOTICE` 注明来源。去掉 motion 依赖，改成本项目的 token 和动效 |
| [BoardUI](https://www.boardui.com/zh-hans) | 免费部分 MIT，Pro 付费 | React Aria + Tailwind 4 + Remix Icon | **参考结构与交互**，用 shadcn 组件重新实现（底层不同，不能直接搬）；只看免费部分 |
| [LeiBox](https://github.com/leishifu666/LeiBox) | MIT + Commons Clause，部分设计来自第三方 | Base UI 版 shadcn + motion/gsap/three | **只借鉴想法，不复制代码**：Commons Clause 禁止再分发组件本身，放进 MIT 仓库有风险 |
| [bencho](https://bencho.dev/) | 付费（源码需 Pro） | React | **只借鉴交互想法** |

参考库的风格大多是弹性动画、玻璃、果冻、粒子，**这些都不采用**。我们借的是功能和交互模式：

| 借鉴 | 来自 | 用在 |
|---|---|---|
| 复制字段（复制后图标变成对勾） | beUI | `CopyField`：UUID、令牌、启动器地址 |
| 带状态的按钮（提交中 → 完成） | beUI | `Button loading` |
| 数字补间 | beUI | 后台统计数字刷新 |
| 用量条（接近上限时变色） | BoardUI meter | `UsageMeter`：存储、积分 |
| 统计卡、通知中心、设置弹窗的结构 | BoardUI | `StatCard`、通知弹层、设置中心 |
| 社交登录按钮的统一样式 | BoardUI social-button | 第三方登录按钮 |
| 分段控件 | BoardUI segmented-control | 皮肤库分类、排序、视图切换 |
| 列表 / 网格视图切换 | LeiBox layout morph（只取想法，不做形变动画） | 衣柜 |
| 输入名称确认危险操作 | bencho inline confirm / slide to confirm（取前者） | 注销账号、删除用户 |
| 密码强度提示 | bencho | 注册、重设密码 |
| 拖放上传 | bencho、beUI | `Dropzone`：上传材质 |
| 选中后出现的批量操作条 | bencho selection actions | 后台表格、衣柜 |
| 可排序列表 | bencho reorder list | 首页区块编辑器（键盘可用的上移、下移，拖动作增强） |
| 未保存修改提示条 | bencho feedback bar | `SaveBar`：设置中心、外观 |

## 4. `@skinloom/ui` 的新结构

```text
web/packages/ui/
  components.json                 shadcn 配置（style nova、base base、iconLibrary lucide）
  src/
    styles/
      globals.css                 token 兜底值、@theme inline、基础样式（从 apps/web 移来）
      fonts.css                   三种字体的 @import
    components/
      ui/                         shadcn 组件（CLI 生成，按 design.md 调整）
      skinloom/                   业务组件（design.md §10.3）
    icons.ts                      菜单图标注册表（design.md §7.2）
    hooks/                        use-media-query、use-confirm 等
    lib/utils.ts                  cn()
    index.ts                      插件可用的公开导出
```

- `apps/web/components.json` 指向 `packages/ui`，用 `bunx shadcn@latest add <组件> -c apps/web` 安装到共享包（shadcn monorepo 的标准做法）。
- `globals.css` 从 `apps/web` 移到 `packages/ui`：将来插件用 Tailwind 时（05 §5.5 的"SDK 预设"），引入的是同一份 token 定义。
- `index.ts` 只导出稳定的部分；shadcn 组件里还在调整的，先不进公开导出。
- 去掉 `@radix-ui/react-slot`；`asChild` 全部改成 Base UI 的 `render` 写法。

## 5. 数据层

### 5.1 生成

orval 增加一个输出目标，生成 TanStack Query 钩子，继续使用现有的 `apiFetch`（CSRF 头、超时、`ApiError`）：

```ts
// web/packages/api-client/orval.config.ts（示意）
skinloomQuery: {
  input: { target: '../../../api/openapi.json' },
  output: {
    target: 'src/gen/query.ts',
    client: 'react-query',
    httpClient: 'fetch',
    override: { mutator: { path: 'src/fetcher.ts', name: 'apiFetch' } },
  },
},
```

服务端组件继续用 `src/gen/api.ts` 里的普通函数；客户端组件用 `src/gen/query.ts` 里的 `useListTextures`、`useWearTextures` 等。查询键由 orval 生成（`getListTexturesQueryKey`），不手写。P0 先确认 orval 8.43 的这种配置与现有 mutator 兼容，不兼容就手写一层薄封装。

### 5.2 约定

- `Providers` 里创建 `QueryClient`（每个浏览器标签一个）。默认 `staleTime: 30s`、`retry: 1`、`refetchOnWindowFocus: false`。
- **首屏数据由服务端取**：页面（服务端组件）用 `asVisitor()` 带着 Cookie 取数据，作为 `initialData` 交给客户端组件的 `useQuery`。公开页的 SEO 与"无 JS 也能读"不受影响。
- 分页列表用 `useInfiniteQuery`，游标是接口返回的 `next`。
- 写操作用 `useMutation`，成功后按查询键失效；收藏、通知已读做乐观更新。
- 统一的 `useApiMutation` 封装：把 `ApiError` 转成文案（沿用 `lib/problem.ts`），失败时弹 Toast；字段错误交给表单。
- 收到 SSE 的 `config.changed` 时，除了 `router.refresh()`，再 `queryClient.invalidateQueries()`。
- 未读通知数 60 秒轮询一次，页面不可见时停止。

## 6. 页面重构

所有页面都按 `design.md` 的外壳、`PageHeader`、组件选择表来搭。下面只写每页的重点。

### 6.1 公开区

**顶栏与页脚**：按 design.md §9.2。页脚一行：版权（`site.copyright`）· 由 SkinLoom 驱动 · 鸣谢；插槽 `site.footer` 保留。

**首页**（区块组成，内置区块全部重写）：

| 区块 | 新样子 |
|---|---|
| `hero` | 默认 `split` 版式：左边大标题、站点描述、"浏览皮肤库"（主）与"注册"（次）；右边是**皮肤展示区**（高光时刻）：本站最新的 5 个公开皮肤轮流以 3D 行走的样子出现，前后两个半透明静止陪衬，下方是头像缩略图与进度条（规则见 design.md §9.5）。站内还没有公开皮肤时改用 `centered` 版式。另有 `centered`（无视觉，短标题）、`banner`（站长上传的背景图铺满）两种版式，由区块属性 `layout` 选择 |
| `announcement` | 内容区顶部的一条提示（`Alert`，`megaphone` 图标），不再是大卡片 |
| `stats` | 一行三个安静的数字：账号、角色、公开材质，不加图标 |
| `latest-textures` / `popular-textures` | 区块标题 + "查看全部" 链接 + 一行 `TextureCard`（双面视图）；手机上横向滚动 |
| `features` | 改为左右两栏的说明列表（每条一个线性图标 + 标题 + 一句具体的话），不再是四张等宽卡片 |
| `launcher-guide` | 三步说明 + `CopyField`（authlib-injector 地址）+ 支持的启动器名称 |
| `markdown`、`image`、`buttons` | 版式随设计系统调整，属性不变 |

**皮肤库**：`PageHeader`（标题 + "上传材质"）→ 筛选栏（分类分段控件、排序分段控件、搜索框 `InputGroup`）→ `TextureGrid`。筛选写进 URL，无 JS 时仍是链接和 GET 表单。手机上分类横向滚动，排序和搜索收进"筛选"抽屉。

**材质详情**：左侧大预览（`SkinViewer` + 工具条：3D/2D、动画、重置），右侧信息面板：名称、类型与公开状态徽章、上传者（头像 + 昵称）、收藏数、大小、上传日期、哈希（`CopyField`）。操作：收藏（主按钮）、穿到角色上（`PlayerPicker`）、下载；编辑、设为私密、删除、举报收进 `ellipsis` 菜单。手机上预览在上（60vh），收藏按钮固定在底部。主题可用 `texture: stacked` 改成上下排列。

**认证页**：登录、注册、找回密码、重设密码、验证邮箱、第三方登录绑定、首次安装都用 `Field`。注册与重设密码加密码强度提示。第三方登录按钮统一样式（品牌图标是 lucide 里没有的少数例外，用各家官方的单色标志 SVG）。首次安装改成分步（站点信息 → 管理员账号 → 完成）。

### 6.2 用户中心

侧栏外壳（design.md §9.2）。

| 页面 | 重点 |
|---|---|
| 概览 | 标题"概览"。统计：积分（金线 `coins`）、角色、衣柜、上传，存储用 `UsageMeter`。签到卡（连续天数、今日可得积分）。"我的角色"列表：每个角色身上皮肤的头像。站点公告 |
| 角色 | `PlayerCard` 网格；"新建角色"在 `PageHeader`，用对话框，显示积分花费与名称规则；换装用 `TexturePicker`（从衣柜里选），替换现在的下拉框；删除走 `useConfirm` 并写明退还积分 |
| 衣柜 | 分类分段 + 搜索 + 网格/列表切换；每项的菜单：穿到角色上、重命名、移出衣柜；多选后出现批量移出 |
| 上传材质 | `Dropzone` + 实时 2D/3D 预览；自动判断经典/纤细模型（可改）；名称、公开开关（`Switch`）、积分花费说明；上传后跳转到材质页 |
| 资料与安全 | 现在 487 行的一个组件拆成分区：基本资料（昵称、头像、语言）、邮箱、密码、登录设备（列表 + 退出其他设备）、第三方账号、外置登录（Yggdrasil 已登录的启动器）、危险区（注销账号：`AlertDialog` + 输入邮箱确认） |
| 通知 | 顶栏铃铛打开 `Popover`，显示最近 5 条与"全部已读"；完整列表页按时间分组 |
| 我的举报 | 列表 + `StatusDot` 状态 |
| 开发者 | `Tabs`：OAuth 应用、已授权的应用、访问令牌。新建令牌后只显示一次，用 `CopyField` 并提示"离开后无法再看到" |
| OAuth 授权页 | 独立的居中卡片：应用名、申请的权限列表（图标 + 一句话）、同意（主）与拒绝 |

### 6.3 后台

侧栏外壳，内容区流式宽度。

| 页面 | 重点 |
|---|---|
| 概览 | 一行四个 `StatCard`（用户、角色、材质、待处理举报，带"比上周"的变化）；下面是一张主图表（近 30 天注册与上传，`chart-1` 与中性色），旁边一个窄列表（最新的待处理举报） |
| 用户 | `DataTable`：搜索、角色与状态筛选、排序、分页；行菜单：编辑（`Sheet`：角色、状态、积分、邮箱验证）、查看衣柜、发通知、删除（`AlertDialog` + 输入邮箱） |
| 角色 | `DataTable`；行内显示皮肤头像；编辑名称、换装（`TexturePicker`）、删除 |
| 举报 | 队列列表：材质预览、举报理由、举报人与上传者、时间；操作：驳回、设为私密、删除材质，按状态筛选 |
| 审计日志 | `DataTable`，按操作类型与操作人筛选，时间用 `RelativeTime` |
| 外观 | `Tabs`：主题（主题卡片：截图、名称、来源"内置/本地"、版本、加载错误）、主题设置（按主题声明的设置项生成表单）、颜色（常用 token 取色器 + 预览面板，"导出为主题"）、首页（区块编辑器）、标识与页脚、自定义 CSS；有修改时出现 `SaveBar` |
| 组件总览 | `/admin/appearance/preview`：所有基础组件与业务组件在当前主题下的样子，亮暗模式切换，给主题作者核对用 |
| 翻译 | `DataTable`：搜索、"只看改过的"开关、行内编辑 |
| 设置 | 左侧分类（站点、注册与登录、邮件、材质与积分、外置登录、存储、安全……）+ 右侧表单；`SaveBar`；密钥类设置只写不读（保持 ADR 0029） |
| 系统状态 | 检查项列表：`StatusDot` + 名称 + 详情；CDN 刷新、发送测试邮件作为页面操作 |

另：后台提供 `⌘K` 命令面板（`Command`），可以跳转到任意后台页面和按邮箱找用户。放在 P3 之后视情况做。

## 7. 主题系统扩展

目标：站长可以**不重新编译**就开发、安装、切换自己的主题，并且有足够的旋钮把站改成自己的样子。

### 7.1 本地主题目录（后端）

- 启动时依次加载：内置主题（嵌入的 `themes/`），然后是 `<数据目录>/themes/*/plugin.json`。本地主题的 id 不能与内置主题重复（重复则跳过并报错）。
- 某个本地主题加载失败，只跳过它并记录错误，不影响站点启动；错误在后台"外观 → 主题"里显示。当前启用的主题加载失败时回退到 `default`。
- **热加载**：后台"重新扫描"按钮、`skinloom theme reload` 命令，以及（`web.mode = dev` 时）监听主题目录的文件变化。重新加载后递增 `config_version`，已打开的页面经 SSE 刷新（ADR 0006 的机制）。
- 本地主题的资源地址用**内容哈希**代替清单里的 `version`（`/themes-assets/<id>/<哈希>/<文件>`），站长改了 CSS 不用手动改版本号也能立即生效，缓存仍然可以长期有效。
- 资源处理：只提供普通文件，不提供 `plugin.json`；按扩展名限定类型（css、woff2、woff、png、webp、avif、jpg、gif、svg）；SVG 附带 `Content-Security-Policy: sandbox`，防止里面的脚本执行。
- 可信边界：能往数据目录放文件的人本来就能控制服务器，所以本地主题视为可信；但主题**仍然不能带 JavaScript**，规则与内置主题相同（自定义 JS 只能由超级管理员在设置里加，ADR 0039）。

### 7.2 清单扩展（`apiVersion` 仍为 1，只增不改）

```jsonc
{
  "id": "my-site",
  "kind": "theme",
  "version": "1.0.0",
  "name": { "zh-CN": "我的站", "en": "My Site" },
  "apiVersion": 1,
  "extends": "default",                       // 新：继承另一个主题，只写差异
  "screenshot": "screenshot.webp",            // 新：后台主题卡片用，16:10
  "theme": {
    "colorScheme": "both",                    // 新：both | light | dark，只有一种时隐藏亮暗切换
    "tokens": {
      "light": { "primary": "oklch(0.55 0.15 150)", "radius": "0.5rem" },
      "dark":  { "primary": "oklch(0.75 0.13 150)" },
      "fonts": { "display": "'My Display', var(--sl-font-sans)" },
      "viewer": { "background": "url(assets/sky.webp) center / cover" }
    },
    "css": "theme.css",
    "layout": { "auth": "split", "skinlib": "compact", "user": "sidebar", "texture": "split" },
    "home": [ { "type": "hero", "props": { "layout": "banner", "background": "assets/hero.webp" } } ],
    "settings": {                             // 新：站长在后台可调的主题设置
      "accent":  { "type": "color",  "label": { "zh-CN": "主色" }, "token": "primary" },
      "heroImage": { "type": "image", "label": { "zh-CN": "首页背景" } },
      "density": { "type": "choice", "options": ["comfortable", "compact"], "default": "comfortable" }
    }
  }
}
```

| 字段 | 说明 |
|---|---|
| `extends` | 先解析父主题：token 逐组合并（子覆盖父），CSS 先父后子，`layout`、`home` 以子为准（没写就用父的）。只允许一层继承，避免循环 |
| `screenshot` | 主题目录里的图片 |
| `colorScheme` | 只支持一种模式的主题强制该模式 |
| `tokens` | 文档化的 token 清单扩充为 design.md §2.2 的全部（新增 `faint-foreground`、`highlight`、`success`、`warning`（默认等于 highlight）、`*-foreground`、`shadow-*`、`pixel-a/b`、`chart-1..5`、`sidebar-*`）；`fonts` 增加 `mono`、`display`。不认识的 token 名照旧允许（给主题自己的 CSS 用） |
| 相对路径 | `viewer.background`、区块的 `image` 属性、设置的 `image` 默认值里的 `assets/...` 解析为该主题的资源地址。token 值里的 `url(assets/...)` 同样改写 |
| `layout` | 新增 `user`（`sidebar`、`tabs`）与 `texture`（`split`、`stacked`）。`site` 暂不加：公开区只有顶栏一种，等有真实需求再加 |
| `home` | `hero` 区块新增属性 `layout`（`split`、`centered`、`banner`）与 `showcase`（`latest`、`popular`、`none`），后端区块目录同步 |
| `settings` | 类型：`color`、`image`、`choice`、`text`、`boolean`。值存在新设置 `theme.settings`（按主题 id 分开存）。带 `token` 的设置项写入对应 token；其余以 `--sl-setting-<名称>` CSS 变量和 `html[data-sl-setting-<名称>]` 属性提供给主题 CSS。后台按声明自动生成表单 |

### 7.3 前端契约

- **样式优先级**：宿主样式全部在 Tailwind 的 cascade layer 里（`theme`、`base`、`components`、`utilities`）；主题 CSS 与自定义 CSS 不放进任何 layer，因此总是优先于宿主的工具类，不用 `!important`。这一条写进主题文档，并有测试保证宿主没有漏在 layer 外的样式。
- **挂钩清单**：design.md §14 的类名与 `data-*` 属性是版本化的公共 API。增加挂钩是小改动；删除或改名按插件 API 的弃用规则处理。前端有一个测试检查每个挂钩都还在渲染结果里。
- **只用 token**：`bun run test` 增加一项样式检查，拒绝组件代码里的 Tailwind 调色板颜色（`slate-*`、`white`、`black` 等）和写死的颜色值。
- `brand-gradient` 保留为兼容 token，默认主题不再使用；`accent` 保持 shadcn 的含义（悬停底色），品牌强调色是新的 `highlight`。这两点在 CHANGELOG 里写明。

### 7.4 工具与文档

| 工具 | 作用 |
|---|---|
| `skinloom theme new <id> [--extends default]` | 在 `data/themes/<id>/` 生成 `plugin.json`、`theme.css`、`README.md` 与示例截图位 |
| `skinloom theme check <目录>` | 按加载规则校验清单、token、区块、资源；对比度不达标时给出警告 |
| `skinloom theme reload` | 重新扫描本地主题 |
| 后台"颜色 → 导出为主题" | 把当前的颜色调整导出成一个继承当前主题的主题包（zip），站长可以从这里起步 |
| JSON Schema | `skinloom.cn/schema/plugin.v1.json` 补齐主题字段，编辑器可以自动补全 |
| 文档站"开发主题" | token 参考表（含含义与对比度要求）、挂钩清单、布局变体、主题设置、继承、字体授权提醒、两个完整示例（只改颜色的；改布局和首页的） |

### 7.5 内置主题

- **默认主题**：按 design.md 重写 token；`theme.css` 只保留 `.sl-pixel-backdrop` 等少量规则，去掉 hero 渐变与斜线纹理。
- **织锦**：保留朱红与金色，改用新 token 名；hero 改用 `banner` 版式；菱格纹只保留在分栏登录页的面板上。它也是"主题能改到什么程度"的示例。

## 8. 移动端

design.md §9.4 是规则；实现上额外要做的：

- 每个阶段结束时，在 375、768、1280 三个宽度，亮暗两种模式，默认与织锦两个主题下逐页检查。
- 用 Playwright 写一个冒烟脚本：打开主要页面，断言 `scrollWidth <= clientWidth`（没有横向溢出），并截图存档，供下一阶段对比。
- 3D 预览在手机上限制像素比（`devicePixelRatio` 最多 2），离开视口时暂停渲染。

## 9. 实施阶段

每个阶段单独提交，结束时用 `skinloom up --dev` 把整套服务跑起来，按 §8 走一遍主要流程。

| 阶段 | 内容 | 验收 |
|---|---|---|
| **P0 地基** | shadcn monorepo 初始化（Base UI、nova）；`globals.css` 移入 `packages/ui` 并换成新 token；三种字体；图标注册表与全局线宽；TanStack Query 与 orval 钩子；样式检查脚本；后端文档化的 token 清单扩充；默认与织锦主题换成新 token | 现有页面在新 token 下能正常显示；`typecheck`、`test`、Go 测试通过；字体从本站加载 |
| **P1 组件** | 安装 design.md §10.2 的基础组件并按规则调整；实现 §10.3 的业务组件；`useConfirm`；组件总览页 | 组件总览页在两个主题、亮暗模式下检查通过；键盘可操作 |
| **P2 外壳** | 顶栏（含手机菜单）、页脚、用户中心与后台的侧栏外壳、认证页外壳、`PageHeader` | 375px 下顶栏无溢出；所有页面挂在新外壳下 |
| **P3 页面** | 公开区 → 用户中心 → 后台，按 §6 重做；去掉全部 `window.confirm`、原生 `<select>`、手写输入框样式；文案按 design.md §11 改一遍（中英两份消息文件） | §1.2 列出的问题清零；Playwright 冒烟通过 |
| **P4 主题系统** | §7 的后端（本地目录、热加载、清单扩展、主题设置、区块属性）、前端（挂钩、布局变体、设置变量）、命令行工具、文档 | 在 `data/themes/` 新建一个继承默认主题的主题，不重启就能在后台看到、切换、调设置；`theme check` 能指出错误 |
| **P5 收尾** | 无障碍检查、性能（首屏字体与 JS 体积）、截图对比、CHANGELOG、文档站更新 | 公开页首屏 JS 不比重构前多出 20% 以上；中文字体首屏下载记录在案 |

P0 到 P3 是前端为主，P4 主要是后端；P4 的后端部分可以和 P1–P3 并行。

## 10. 风险与待定

### 风险

| 风险 | 应对 |
|---|---|
| shadcn 的 Base UI 版组件还在变动 | 组件代码在仓库里，不跟随上游自动更新；只有稳定的进入 `@skinloom/ui` 公开导出 |
| 思源黑体体积大，首次访问慢 | 只用可变字体的按需切片。P0 实测：首页首次访问下载 842 KB 字体（思源黑体 13 片约 750 KB），之后缓存。P5 评估改用静态字重 400/500 两档或常用字子集；主题也可以把 `fonts.sans` 换回系统字体 |
| 主题 CSS 不在 layer 里，可能把宿主组件改坏 | 这是有意的（站长要能改）；文档写清楚，并提供组件总览页供核对 |
| `accent` 含义与直觉不符（shadcn 的历史包袱） | 保持 shadcn 含义以兼容生态；文档里明确写出；品牌强调色用 `highlight` |
| 页面重写面大，回归风险高 | 按页面分批提交；保留现有的 19 个测试文件并补上组件测试；Playwright 冒烟覆盖主要页面 |

### 待定（需要决定）

1. **空站时 hero 展示什么**：已定为改用 `centered` 版式（不显示展示区）。是否另做 3–5 个原创默认皮肤随程序分发（CC0 或 MIT，不能用 Mojang 的默认皮肤），以后再议。
2. **主题预览模式**（05 §6.3，只有当前管理员看到新主题）：建议放在 P4 之后单独做。
3. **后台命令面板**：P3 完成后视情况做。
4. 第三方登录按钮的品牌标志：使用各家官方提供的单色标志，需要核对各自的品牌使用规范。

## 11. 拆分的 ADR

评审通过后写两份 ADR：

- **0042 前端设计系统与组件库**：shadcn/ui（Base UI）、lucide 与图标注册表、TanStack Query 与 orval 钩子、TanStack Table、sonner、字体选择与授权、颜色体系、`design.md` 作为规则来源。
- **0043 本地主题目录与主题清单扩展**：`data/themes`、热加载与内容哈希地址、`extends`、`colorScheme`、`screenshot`、主题设置、新布局变体与区块属性、样式优先级约定、挂钩清单、命令行工具。

## 附：参考

- shadcn/ui 文档（monorepo、Base UI 默认、`shadcn create` 的风格）
- [beUI](https://github.com/starc007/ui-components)、[BoardUI](https://www.boardui.com/zh-hans)、[LeiBox](https://github.com/leishifu666/LeiBox)、[bencho](https://bencho.dev/)
- 字体：[Inter](https://rsms.me/inter/)、[Noto Sans SC（Fontsource）](https://fontsource.org/fonts/noto-sans-sc)、[JetBrains Mono](https://www.jetbrains.com/lp/mono/)；[MiSans 常见问题](https://hyperos.mi.com/font/zh/faq)
- unslop（Damir Sokolovsky，[@heydamir](https://x.com/heydamir)，CC BY 4.0）：本方案的审查清单与方向写法
