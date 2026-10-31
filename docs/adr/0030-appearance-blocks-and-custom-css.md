# 0030 外观：主题切换、区块目录与自定义 CSS

- 状态：已接受（2026-10-30）
- 相关：设计文档 02 F01、F35、§4；03 S16；05 §5.4、§6；ADR 0006、0029

## 背景
M4 第二部分是外观（ADR 0029）。M0 已经把默认主题写成主题包（`plugin.json` 里的 token、CSS、布局与首页区块），设置里也有 `theme.active`、`theme.overrides`、`home.blocks`，但：
- 后台没有界面，只能用命令行或 API 改。
- 只装了一个主题，"后台切换主题无需重启"（04 §13 的验收）无从演示。
- 清单的 `layout`（site、auth、user、skinlib）前端一个都没用；`user: sidebar` 描述的界面并不存在。`viewer` token 也没用。
- 05 §5.4 列的 10 种内置区块只做了 4 种；`home.blocks` 只检查有没有 `type`。
- `theme.overrides` 里不安全的值保存时不报错，渲染时被悄悄丢掉。
- F35 个性化（favicon、版权文案、自定义 CSS 等）与 02 §4 的 `seo.*` 都没有。

## 决策
- **区块目录放在后端**（`internal/theme/blocks.go`）：每种内置区块的类型与属性，属性有 `text`、`markdown`、`link`、`image`、`number`、`choice`、`buttons` 几种。
  - `home.blocks` 与主题清单里的 `home` 都按目录检查：未知的类型或属性、不是本站路径也不是 http(s) 的链接、超出范围的数量、超过 30 个区块，都拒绝。主题包在加载时就报错。
  - `GET /api/v1/admin/blocks` 把目录给后台编辑器，表单按属性类型生成。第二阶段插件在清单里声明的区块走同一套检查。
- **bootstrap 里的区块按访客的语言准备好**：文字属性可以写一个字符串或每种语言一份，bootstrap 只给当前语言的那份；Markdown 由服务端渲染成 `<属性名>Html`（与公告相同，不保留原始 HTML）。前端的区块只读字符串。
- **补齐 05 §5.4 的内置区块**：新增 `stats`、`latest-textures`、`popular-textures`、`markdown`、`image`、`buttons`。
  - `stats` 读新接口 `GET /api/v1/site/stats`（未删除的账号、角色、公开材质），结果缓存一分钟。
  - 材质列表不带访客的会话去取，所有人看到同样的公开材质。
- **布局变体只列已经实现的**：`auth`（`card`、`split`）与 `skinlib`（`comfortable`、`compact`）。site 与 user 目前各只有一种界面，不列。清单里没列的键忽略；列了的键值不认识，加载失败。默认主题的 `layout` 只保留这两项。分栏登录页旁边的面板是 `.sl-auth-panel`，主题可以重新设计它的样式。
- **viewer token** 输出为 `--sl-viewer-*`。3D 预览的画布本来就是透明的，背景用 `--sl-viewer-background`（任何 CSS 背景值）。
- **第二个内置主题"织锦"（`brocade`）**：暖色、分栏登录页、紧凑的皮肤库，首页带最新材质与站点数据。它把主题标准的 ①–④ 层都用了一遍，也让后台切换主题真正有得选。安装第三方主题仍在第二阶段（P2-3、P2-4）。
- **`theme.overrides` 保存时就校验**：分组只能是 light、dark、fonts、viewer，token 名只用小写字母、数字和横线，值不能含 `; { } < >` 和注释，最长 200 个字符。颜色调整对所有主题生效，不按主题分开存：多数站点先选主题再调色，后台提供"全部清除"。
- **个性化与 SEO 设置**：
  - `site.logo`、`site.favicon`：图片地址或本站路径（BS 的 `favicon_url`）。
  - `site.copyright`：多语言 Markdown，渲染方式同公告。BS 的 `copyright_text` 是 HTML，改成 Markdown 就插不进脚本。
  - `seo.keywords`（BS 的 `meta_keywords`）；页面描述继续用 `site.description`。
  - BS 的 `home_pic_url` 对应 hero 区块的背景图，导航栏与侧栏颜色对应颜色调整。`transparent_navbar`、`hide_intro`、`fixed_bg` 不再单独提供，用区块编排和自定义 CSS 实现。`meta_extras`（任意 HTML 放进 head）不提供。
- **自定义 CSS**（`theme.custom_css`，最多 64 KB）：
  - 设置定义增加 `Superadmin`：只有超级管理员、管理员令牌和命令行能改（03 S16）。普通管理员在页面上看到只读，从接口改会得到 403。
  - 不放进页面的 `<style>`，而是单独的样式表 `/themes-assets/_custom/<内容哈希>.css`（`text/css`、`nosniff`），排在主题 CSS 之后。内容没变就可以永久缓存；改了以后地址跟着变。因为不在页面里，里面写什么都结束不了元素、注入不了 HTML。
- **自定义 JS 暂不提供**：要给 Next.js 的每个页面加 CSP nonce，留到和 CSP 一起做（M5 安全自查之前）。
- **外观页** `/admin/appearance`：
  - 主题：列出已安装的主题，一键切换；
  - 颜色：亮色、暗色两组常用 token 与圆角，可以用取色器；
  - 首页：添加、排序、删除区块，按属性类型编辑；"改用主题自带的首页"把 `home.blocks` 设回 null，以后换主题首页也跟着换；
  - 站点标识与页脚、自定义 CSS。

  设置页不再显示这些键。
- **预览模式（05 §6.3）推迟**：保存即生效。

## 影响
- 新设置 `site.logo`、`site.favicon`、`site.copyright`、`seo.keywords`、`theme.custom_css`；`home.blocks` 与 `theme.overrides` 校验更严格。读取时不重新校验已保存的值：旧的未知区块，前端照旧跳过。
- 接口：`SettingView.superadmin`；`ThemeView` 带上主题自己的 token、布局与首页；新增 `listBlockTypes`、`getSiteStats`；`SiteInfo` 增加 `logo`、`favicon`、`copyrightHtml`、`keywords`。没有新表与迁移。
- 主题作者：清单里的首页区块与 token 在加载时检查；主题 id 只能是 1–32 个小写字母、数字或横线（`_custom` 留给自定义 CSS）。
