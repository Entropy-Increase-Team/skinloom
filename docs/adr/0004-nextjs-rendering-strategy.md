# 0004 Next.js 渲染策略

- 状态：已接受（2026-10-23）
- 相关：设计文档 04 §2.2、§2.4；05 §5

## 决策
- Next.js App Router + React 19 + TypeScript；`output: "standalone"`。
- 公开页（首页、皮肤库、材质详情）服务端渲染；用户中心/后台为 SSR 外壳 + 客户端组件。
- 每次渲染从后端取 bootstrap（站点信息、激活主题、菜单、插件前端清单），按请求去重；**不依赖 ISR / Data Cache**，缓存与失效交给 Go。
- 主题 token 由根布局注入为 CSS 变量（选择器优先级高于兜底值）。
- 浏览器订阅 `/events`（SSE），收到新的配置版本即 `router.refresh()`。
- 插件 UI 不使用 Module Federation（`nextjs-mf` 已在弃用），改为运行时 `import()` 独立构建的 ES Module，共享依赖由宿主提供；插件组件只在客户端渲染。
- 语言由后端判定（`?lang`、Cookie、Accept-Language），前端 next-intl 只加载对应消息，避免前后端不一致。

## 影响
- 需要 SEO 的首页使用声明式区块：内置区块服务端渲染，插件区块客户端渲染。
- 后端不可达时前端以默认内容渲染并显示提示，而不是整页报错。
