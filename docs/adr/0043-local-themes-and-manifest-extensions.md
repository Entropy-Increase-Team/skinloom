# 0043 本地主题目录与主题清单扩展

- 状态：已接受（2026-11-09），实现在设计文档 07 的 P4 阶段
- 相关：设计文档 05 §6（主题标准）、07 §7；ADR 0006（热加载）、0030（外观、区块与自定义 CSS）、0039（CSP 与自定义 JS）、0042（设计系统）

## 背景

主题只能编进程序（`themes/embed.go` 嵌入 `default` 与 `brocade`），站长没有办法安装自己的主题；布局变体只有 `auth`、`skinlib` 两项；主题标准里的主题设置项没有实现；主题 CSS 能依赖哪些类名、与宿主 Tailwind 谁的优先级高，都没有约定。

## 决策

### 本地主题目录

- 启动时先加载内置主题，再加载 `<数据目录>/themes/*/plugin.json`。本地主题的 id 不能与内置主题重复，重复的跳过并报错。
- 单个本地主题加载失败只跳过它，错误记录在日志并显示在后台"外观 → 主题"；当前启用的主题失败时回退到 `default`，站点照常启动。
- 热加载：后台"重新扫描"、`skinloom theme reload`，以及 `web.mode = dev` 时监听主题目录。重新加载后递增 `config_version`，已打开的页面经 SSE 刷新。
- 本地主题的资源地址用内容哈希代替清单的 `version`（`/themes-assets/<id>/<哈希>/<文件>`）：改了文件不用改版本号也立即生效，缓存仍可长期有效。
- 资源只提供普通文件，不提供 `plugin.json`；扩展名限定为 css、woff2、woff、png、webp、avif、jpg、gif、svg；SVG 附带 `Content-Security-Policy: sandbox`。
- 能往数据目录放文件的人本来就控制着服务器，本地主题视为可信；但主题仍然不能带 JavaScript（自定义 JS 只由超级管理员在设置里加，ADR 0039）。

### 清单扩展（`apiVersion` 仍为 1，只增不改）

- `extends`：继承另一个主题，只写差异。token 逐组合并，CSS 先父后子，`layout`、`home` 以子为准。只允许一层。
- `screenshot`：后台主题卡片的预览图（16:10）。
- `theme.colorScheme`：`both`（默认）、`light` 或 `dark`；只支持一种时隐藏亮暗切换并强制该模式。
- `tokens`：文档化的清单扩充为 `web/design.md` §2.2 的全部；`fonts` 增加 `mono`、`display`。不认识的 token 名照旧允许。token 值与区块、设置的图片里写的 `assets/...` 相对路径解析为该主题的资源地址。
- `layout`：新增 `user`（`sidebar`、`tabs`）与 `texture`（`split`、`stacked`）。
- `home`：`hero` 区块新增属性 `layout`（`split`、`centered`、`banner`）与 `showcase`（`latest`、`popular`、`none`），后端区块目录同步。
- `settings`：主题声明的设置项，类型 `color`、`image`、`choice`、`text`、`boolean`。值存在新设置 `theme.settings`（按主题 id 分开存）。带 `token` 的写入该 token，其余以 `--sl-setting-<名称>` 变量和 `html[data-sl-setting-<名称>]` 属性提供给主题 CSS。后台按声明生成表单。

### 前端契约

- 宿主样式全部在 Tailwind 的 cascade layer 里；主题 CSS 与自定义 CSS 不放进任何 layer，因此总是优先于宿主的工具类，不需要 `!important`。主题想让规则优先级更低，就包进 `@layer components`。
- 主题可依赖的类名与属性（`web/design.md` §14 的挂钩表）是版本化的公共 API，前端有测试检查它们都还在渲染结果里；增加是小改动，删除或改名走插件 API 的弃用流程。

### 工具

- `skinloom theme new <id> [--extends default]`：在 `data/themes/<id>/` 生成清单、`theme.css` 与说明。
- `skinloom theme check <目录>`：按加载规则校验，对比度不达标时警告。
- 后台"颜色 → 导出为主题"：把当前的颜色调整导出为继承当前主题的主题包。
- `skinloom.cn/schema/plugin.v1.json` 补齐主题字段。

## 备选方案

- **主题市场或上传 zip 安装**：要签名、权限确认与卸载流程，属于第二阶段的插件平台（P2-4）。本地目录先解决"站长自己开发自己用"。
- **允许本地主题覆盖内置主题的 id**：站长改坏了无法回退到内置版本；改为用 `extends` 继承。
- **主题 CSS 放进低优先级的 layer**：站长写的规则会被宿主工具类压住，只能处处 `!important`。
- **组件覆盖（05 §6.1 ⑤）一起做**：需要先有插件前端运行时，留到 2.0。

## 影响

- 新设置 `theme.settings`；`listThemes` 返回来源（内置/本地）、截图与加载错误；新增接口"重新扫描主题"；新命令 `skinloom theme`。
- 区块目录：`hero` 多两个属性。
- 文档站新增"开发主题"页（token 参考、挂钩清单、布局变体、主题设置、继承、两个完整示例）。
