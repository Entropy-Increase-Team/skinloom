# 0008 项目命名：SkinLoom · 云织

- 状态：已接受（2026-10-23，项目负责人确认）
- 相关：设计文档 04 §11

## 决策
英文名 **SkinLoom**，中文名 **云织**。命令行 `skinloom`，npm scope `@skinloom`。

- 源码仓库：<https://github.com/Entropy-Increase-Team/skinloom>（由熵增项目组维护），Go 模块 `github.com/Entropy-Increase-Team/skinloom`
- 官方域名：**skinloom.cn**（官网与文档站）；插件清单 JSON Schema 发布在 `https://skinloom.cn/schema/`
- 容器镜像：`ghcr.io/entropy-increase-team/{skinloom,server,web}`（ghcr 要求小写）

约束：不以 blessing 开头；不含 Minecraft / Mojang 商标。命名时（2026-10-22）npm 包名与 GitHub 账号均空闲，同名仓库为 0。

## 鸣谢要求
Blessing Skin 必须出现在：仓库 README 的鸣谢第一条、`NOTICE`（保留其 MIT 版权声明）、程序内"关于"页面、文档站鸣谢页。

## 待办
注册 npm 组织 `skinloom`（发布插件 SDK 时需要）；商标检索。

## 修订
- 2026-10-23：仓库确定为 Entropy-Increase-Team/skinloom，域名确定为 skinloom.cn（原计划的独立 GitHub 组织与 .dev/.org 域名不再需要）。
