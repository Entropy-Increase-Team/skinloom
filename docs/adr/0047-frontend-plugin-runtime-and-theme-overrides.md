# 0047 前端插件运行时、20 处插槽挂载与主题 Level ⑤ 组件覆盖

- 状态：已接受（2026-11-18）
- 相关：设计文档 `09-plugin-platform-plan.md` §0（决策 5）、§3.2；`05-plugin-theme-standard.md` §5；ADR 0004（SSR 与非 Module Federation 策略）、0039（CSP）、0043（主题清单与 L1–L4）、0046（SDK/CLI 工具链）

## 背景

P2-2 打通了插件前后端类型契约与 CLI 构建管线：插件与主题前端源码在构建阶段将 `react`、`react-dom`、`@tanstack/react-query`、`@skinloom/ui`、`@skinloom/plugin-sdk/web` 重映射至全局共享依赖表，以避免每个扩展自带多份 React 运行时。要让插件 UI 与 Level ⑤ 主题在真实页面生效，还需解决：

1. **CSP 严格脚本策略下加载外部扩展**：宿主页每请求签发 `script-src 'self' 'nonce-...' 'strict-dynamic'`，必须由带 nonce 的宿主客户端入口通过原生 `import()` 动态加载已启用扩展。
2. **SSR 零闪烁与防破坏边界**：主题覆盖站点外壳（头部、页脚、Logo、认证背景、首页 Hero、材质/角色卡片）与插件挂件绝不能引发 Next.js SSR Hydration Mismatch，单个插件异常也不能弄垮整页。
3. **管理员安全预览与局部主题 JS 信任边界**：本地未签名主题目录下放置 JS 入口不能不经确认自动执行；管理员预览主题不能影响普通访客流量与全局首页缓存。

## 决策

### 共享依赖单实例与动态加载器（`plugin-runtime.tsx`）

- 客户端引导阶段初始化 `globalThis.__SKINLOOM_SHARED__`，导出宿主同一个 `react`、`react-dom`、`react-dom/client`、`react/jsx-runtime`、`react/jsx-dev-runtime`、`@tanstack/react-query`、`@skinloom/ui` 与 `@skinloom/plugin-sdk/web` 实例。
- `Bootstrap` 下发已启用插件（显式过滤 `type === "secret"` 设置项，仅下发公开非敏感配置、当前语言包、`menus`、`forms`、`blocks` 及单调版本号 `rev` 构成的可缓存资源 URL）。
- 客户端 `PluginRuntimeProvider` 动态加载 `/plugins-assets/<id>/<rev>/<entry>` 与样式表，为每个扩展注入独立的 `PluginWebRuntimeProvider` 上下文与 `PluginErrorBoundary`。

### 20 个 `<Slot>`、5 个 `<PluginForms>` 作用域与插件页面/区块

- 站点外框、认证页、用户中心、皮肤库及后台共挂载全部 **20 个规范 `<Slot>`**；动态插件页面分别托管在 `/p/[id]/[[...slug]]`、`/user/p/[id]/[[...slug]]`、`/admin/p/[id]/[[...slug]]`；后台 `admin` 区域菜单项在 `getBootstrap` 时按当前登录角色（`admin` / `superadmin`）过滤。
- `<PluginForms>` 为 `auth.register`、`auth.login`、`user.profile`、`texture.upload`（含 multipart `form` JSON 透传）、`player.create` 渲染声明式字段并将值提交至后端 Guard/Event。
- 主题模块 `blocks.go` 支持插件注册自定义首页区块并在 `LocalizeBlocks` 中原样透传其配置；前端 `Blocks` 将非内置类型交给 `<PluginBlock>` 在错误边界内渲染。

### 主题 Level ⑤ 组件覆盖与会话级管理员预览

- 主题清单新增顶层 `web` 与 `theme.overrides`（白名单限定 7 个目标：`site.header`、`site.footer`、`site.logo`、`auth.background`、`home.hero`、`texture.card`、`player.card`）。
- `<ThemeOverride>` 在服务端渲染及首次客户端水合期间固定输出 `fallback` 标记（消除水合不一致与首屏闪烁），水合完成后若启用主题声明并导出了对应组件，则平滑替换为错误边界隔离的主题组件。
- `/themes-assets/<id>/<rev>/web/*.js|mjs` 与 `Bootstrap.Theme.WebEntry` 仅对内置主题、经 `plugin.Manager` 启用的受管主题包，或由超级管理员显式加入只写高危设置 `theme.trusted_web` 的本地主题开放，未确认的本地目录主题返回 `403 Forbidden`。
- 新增 `POST /api/v1/admin/themes/preview` 设置 `HttpOnly; SameSite=Lax` Cookie `skinloom_theme_preview`，仅对持有合法管理员会话的 `getBootstrap` 请求生效，绕过公共首页缓存并在页面底部渲染 `ThemePreviewBar`（支持一键应用全站或退出预览）。

## 备选方案

- **在服务端执行主题/插件 React 组件以实现 SSR 覆盖**：需要把第三方 TSX 代码放进 Node SSR 上下文，打破沙箱边界并增加构建耦合与 XSS 风险；采用客户端错误边界渐进接管配合服务端静态默认壳，兼顾 SEO 首屏性能与运行安全。
