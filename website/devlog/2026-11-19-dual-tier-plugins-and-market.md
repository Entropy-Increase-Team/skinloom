---
title: 双轨插件平台、官方签名市场与首批官方扩展收官
slug: dual-tier-plugins-and-market
date: 2026-11-19
tags: [devlog, P2, plugins, market]
---

云织 SkinLoom 插件与主题平台（P2-1 ~ P2-6）已全部完成基建验收、独立市场上线及首批官方扩展落地，**`apiVersion: 1`** 与 **`native.protocolVersion: 1`** 正式冻结。

## 本期完成

- **双轨插件运行时（ADR 0045–0049）**：
  - **Tier 1（`runtime: "js"`）**：进程内 QuickJS/WASM 确定性沙箱池、指令燃料/内存硬限、25 组 `ctx.*` 宿主 API、47 个 Hook（21 Guard / 7 Filter / 19 Event）、20 个前端插槽、5 个声明式表单扩展点与 Level ⑤ 主题组件覆盖。
  - **Tier 2（`runtime: "native"`）**：基于 `hashicorp/go-plugin` 双向 gRPC 子进程、`sdk/go`、`sdk/go/plugindb` 三方言隔离 SQL 数据库（`plg_<id>_` 规范前缀）、`sdk/go/plugintest` 进程内测试套件、本地 HTTP/SSE/WebSocket 流式反代与 `Storage`/`Search`/`Render` 底层驱动扩展。
- **官方静态签名插件市场（ADR 0050）**：
  - 独立维护 [`Entropy-Increase-Team/skinloom-registry`](https://github.com/Entropy-Increase-Team/skinloom-registry)：纯源码 Commit SHA 登记、差量 PR 安全门禁、真实源码检出与 `plugin.json` 一致性核验、受保护环境 5 平台交叉编译 + 官方 Ed25519 私钥签名与 `blocked.yaml` 恶意包全网自动热禁用。
  - 宿主侧完成 `RegistryClient`、五维环境兼容匹配、拓扑依赖级联安装、多架构集群按需回源、后台「插件市场」与外观页「浏览主题市场」UI 及 `skinloom plugin search / market-install / upgrade` CLI。
- **独立官方插件库与首批 5 个官方扩展（`skinloom-plugins`）**：
  - Tier 1 JS 沙箱插件：`invitation-codes`、`mojang-verification`、`texture-description`；
  - Level ⑤ 示范主题：`theme-atelier`（完整覆盖 L1–L5 与 `site.footer` / `texture.card` 组件覆盖）；
  - Tier 2 Native Go 插件：`bs-importer`（遵循方案 A，按需安装、迁完可卸载，直连 BS 数据库或对接升级后的 `blessing-skin-export-bridge` 补迁第三方 BS 插件扩展表）。
- **文档站定稿**：补齐 TS 插件快速入门、Go 原生插件进阶指南、主题 L1–L5 指南与 API v1.0 参考手册。

## 关键架构决策

- [ADR 0045](/adr/0045-plugin-storage-slots-and-jsrt-pool) · [ADR 0046](/adr/0046-plugin-host-extensions-and-sdk-toolchain) · [ADR 0047](/adr/0047-frontend-plugin-runtime-and-theme-overrides) · [ADR 0048](/adr/0048-plugin-package-signing-and-cluster-hot-reload) · [ADR 0049](/adr/0049-dual-tier-plugin-architecture-grpc-and-isolated-db) · [ADR 0050](/adr/0050-gitops-signed-plugin-market-and-multi-arch-distribution)。
- **三仓职责解耦**：`skinloom`（核心服务 + 前端 + SDK/CLI）、`skinloom-plugins`（官方插件与主题源码）、`skinloom-registry`（GitOps 静态签名索引与受控分发）。
- **Blessing Skin 迁移两层分离（方案 A）**：核心账号/角色/材质/密钥/设置保留在主程序 Pre-Core 安装向导与离线 CLI（进入正常模式后零运行时开销）；BS 第三方插件扩展表迁移通过可卸载 Tier 2 插件 `bs-importer` + 跨插件服务闭环完成。

## 下期计划

- 推送 `skinloom-plugins` 独立仓库并在 `skinloom-registry` 配置正式离线 Ed25519 签名密钥与 `production-signing` 环境；
- 通过 PR 将首批 5 个官方扩展上架至官方市场，完成云端 Runner 5 平台编译与线上实测闭环；
- 准备 SkinLoom 1.0 正式发布。
