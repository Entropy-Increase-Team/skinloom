# 06 · 文档与日志体系

> 目标：SkinLoom 是要长期维护的开源项目，文档和日志是产品的一部分，不是附属品。本文规定文档放在哪、写什么、谁来更新、怎么保证不过期。
> 关联：[04](04-rewrite-plan.md) §15、[05](05-plugin-theme-standard.md) §13。

---

## 1. 文档地图

| 类别 | 读者 | 位置 | 更新节奏 | 版本化 |
|---|---|---|---|---|
| 用户指南 | 玩家 | 文档站 `/guide` | 功能变化时 | 跟随核心大版本 |
| 站长指南 | 部署与运营者 | 文档站 `/admin-guide` | 每次发版检查 | 跟随核心大版本 |
| 开发者指南 | 核心贡献者 | 文档站 `/dev` + 仓库根 `CONTRIBUTING.md` | 随代码 | 只维护最新 |
| 插件开发 | 插件/主题作者 | 文档站 `/plugins` | 随插件 API | **按插件 API 大版本** |
| API 参考 | 第三方开发者 | 文档站 `/api`（OpenAPI 生成）、`/plugins/reference`（TypeDoc/Hook/插槽目录生成） | 每次发版自动生成 | 跟随版本 |
| 更新日志 | 所有人 | 仓库 `CHANGELOG.md`（自动）+ 文档站 `/releases`（人写的发布说明） | 每次发版 | — |
| 开发日志（devlog） | 社区、贡献者、未来的自己 | 文档站 `/devlog`（博客形式），源文件在仓库 `website/devlog/` | 每两周或每个里程碑 | — |
| 架构决策记录（ADR） | 核心贡献者 | 仓库 `docs/adr/`，文档站 `/dev/adr` 渲染 | 每个重要决策 | 只追加 |
| 路线图 | 所有人 | 文档站 `/roadmap` + GitHub Projects | 每个里程碑 | — |

## 2. 文档站技术选型

**Docusaurus**（React，Meta 维护）：
- 内置**文档版本化**（插件 API 1.x / 2.x 文档并存，这是长期维护最需要的能力）；
- 内置**博客**，直接用作 devlog 与发布说明；
- 内置 i18n（中文为主、英文同步），可接 Crowdin；
- 生态插件：OpenAPI 文档（`docusaurus-plugin-openapi-docs`）、TypeDoc（SDK 参考）、本地搜索或 Algolia DocSearch（开源项目免费）；
- 与项目前端同为 React 技术栈，可直接嵌入 `@skinloom/ui` 组件做交互示例（例如在线调主题 token）。

托管：GitHub Pages 或 Cloudflare Pages；国内镜像同步到国内静态托管/CDN。每个 PR 生成预览地址。

## 3. 仓库内的文档文件

```
skinloom/
├── README.md               项目介绍、截图、快速开始、鸣谢（第一条：Blessing Skin）
├── README.en.md
├── CHANGELOG.md            release-please 自动生成（核心）
├── CONTRIBUTING.md         开发环境、分支与提交规范、PR 清单、DCO
├── CODE_OF_CONDUCT.md
├── SECURITY.md             漏洞报告方式（GitHub 私密报告）
├── NOTICE                  第三方版权声明（Blessing Skin MIT 声明等）
├── docs/adr/               架构决策记录 0001-xxx.md …
├── website/
│   ├── docs/               用户/站长/开发者指南
│   ├── plugins/            插件开发文档（版本化）
│   ├── devlog/             开发日志（博客）
│   ├── releases/           发布说明（博客）
│   └── i18n/en/            英文翻译
└── web/packages/plugin-sdk/CHANGELOG.md   SDK 独立更新日志（插件作者最关心）
```

## 4. 更新日志（Changelog）规范

### 4.1 自动部分：`CHANGELOG.md`
- 提交信息遵循 **Conventional Commits**：`feat(texture): …`、`fix(yggdrasil): …`、`perf(skinlib): …`、`docs: …`、`refactor: …`；破坏性变更写 `feat!:` 或正文 `BREAKING CHANGE:`。
- 插件 API 相关改动额外加 scope `plugin-api`，自动汇总进 SDK 的 CHANGELOG。
- **release-please**（manifest 模式）管理多组件独立版本：核心（server + web 同版本号）、`@skinloom/plugin-sdk`、`@skinloom/ui`、`create-skinloom-plugin`、Helm Chart。每个组件各自生成 CHANGELOG 与发版 PR。
- CI 校验提交信息格式（commitlint）。

### 4.2 人写部分：发布说明（`/releases`）
每个 minor/major 版本一篇，模板：

```markdown
---
title: SkinLoom v1.2.0 发布
date: YYYY-MM-DD
tags: [release]
---
## 亮点
（2–4 条，配截图）
## 升级须知
- 破坏性变更：……
- 需要的操作：（迁移会自动执行 / 需要修改配置 xxx）
- 最低版本要求：……
## 插件开发者需要知道的
- 新增能力 / 弃用 / 将在 vX 移除
## 完整更新日志
（链接到 CHANGELOG 对应段落）
## 致谢
（本版本的贡献者）
```

## 5. 开发日志（Devlog）规范

- 频率：每两周一篇；每个里程碑结束必有一篇。文件名 `YYYY-MM-DD-slug.md`。
- 目的：记录**过程**——做了什么、为什么这么做、踩了什么坑、数据怎么样。CHANGELOG 记录"结果"，devlog 记录"来龙去脉"。
- 模板：

```markdown
---
title: 第 N 期 · 一句话标题
date: YYYY-MM-DD
authors: [name]
tags: [devlog, M2]
---
## 本期完成
## 进行中
## 决策（附 ADR 链接）
## 问题与风险
## 数据（性能/测试覆盖/包体积等，可选）
## 下期计划
```

本工作区已按此规范开始记录：[devlog/](/devlog)。项目仓库建立后整体迁入 `website/devlog/`。

## 6. 架构决策记录（ADR）

- 格式：MADR 精简版，文件 `docs/adr/NNNN-标题.md`，编号只增不改。
- 状态：`提议` → `已接受` / `已拒绝`；被新决策取代时标记 `已被 NNNN 取代`，**不删除旧记录**。
- 什么时候写：选型、跨模块接口、数据模型重大变化、公共 API（插件 API、REST API、Hook/插槽目录）的不兼容变更、安全模型变化。
- 模板：

```markdown
# NNNN 标题
- 状态：已接受（YYYY-MM-DD）
- 相关：issue/PR 链接、前置 ADR
## 背景
## 决策
## 备选方案与取舍
## 影响（正面 / 负面 / 需要的后续工作）
```

首批 ADR（建仓时由 04/05 中的决策拆出）：0001 前后端分离与启动器、0002 Go 模块化单体与框架、0003 数据访问与迁移、0004 Next.js 渲染策略、0005 插件标准（TypeScript + JS 沙箱）、0006 热加载模型、0007 插件存储不开放 SQL、0008 项目命名。

## 7. 让文档不过期的机制

| 机制 | 做法 |
|---|---|
| 生成优先 | REST API 参考由 OpenAPI 生成；`ctx` API 由 TypeDoc 生成；Hook 目录、插槽目录、`plugin.json` 参考、设置项参考由 Go 代码中的目录生成。CI 检查生成物未漂移 |
| PR 清单 | PR 模板包含"是否影响用户/站长/插件开发者文档"勾选项；改动公共 API 的 PR 没有文档改动时 CI 提示 |
| 示例即测试 | 文档中的插件示例放在 `plugins/examples/`，CI 构建并跑测试；文档用代码片段引用这些文件而不是复制粘贴 |
| 链接与拼写 | CI 跑死链检查与中英文排版检查 |
| 发版检查 | 发版清单含"站长指南的升级章节已更新""发布说明已写" |
| 归属 | 每个文档目录在 CODEOWNERS 中有负责人 |

## 8. 本仓库的落地情况（M0）

- `docs/design/`：01–06 设计文档（本目录），文档站以"设计文档"栏目渲染；源码链接已固定为 Blessing Skin 的永久链接。
- `docs/adr/`：首批 ADR 0001–0008。
- `website/`：Docusaurus 文档站，`website/devlog/` 为开发日志（博客）。
- `CHANGELOG.md`：手工维护到 v0.1 发布前，之后交给 release-please。
