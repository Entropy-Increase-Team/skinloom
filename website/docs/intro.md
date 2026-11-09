---
slug: /
title: 介绍
sidebar_position: 1
---

# 云织 SkinLoom

云织是一个开源、自托管的 Minecraft 皮肤站。玩家在这里上传、分享皮肤和披风，给游戏角色换装；启动器和游戏通过外置登录（authlib-injector）或 CustomSkinLoader 等 Mod 显示这些皮肤。

它以 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 为蓝本重新实现，对外协议保持兼容：老站可以一键迁移，玩家和服务器不用改配置。

## 你是谁？

| 我想…… | 从这里开始 |
|---|---|
| 在某个皮肤站上用皮肤 | [玩家指南](players/account.md) |
| 自己搭一个皮肤站 | [快速开始](deployment/quick-start.md) |
| 从 Blessing Skin 搬过来 | [从 Blessing Skin 迁移](deployment/from-blessing-skin.md) |
| 接入本站账号、调用接口 | [开放平台与接口](development/api.md) |
| 参与开发 | [开发环境](development/setup.md) → [架构](development/architecture.md) |

## 有什么

**玩家**：皮肤库、上传与收藏、衣柜、多个角色、3D 预览、签到积分、举报、站内通知，以及 GitHub / Microsoft / OpenID Connect 登录。

**游戏**：authlib-injector 外置登录、CustomSkinLoader、UniSkinMod、固定图片地址，头像与预览图。

**站长**：后台管理用户、角色、举报与审计日志；在线修改设置、主题与首页，不用重启；SQLite / MySQL / PostgreSQL；本地或 S3 存储；CDN 刷新；备份恢复；Docker、Kubernetes、Windows / macOS / Linux 发行包与系统服务。

**开发者**：OAuth 2.0、个人访问令牌、与 Blessing Skin 相同的资源接口，以及完整的 REST API（OpenAPI 3.1）。

## 状态

1.0 正在收尾，目前发布的是 0.x 预览版，接口和设置在 1.0 之前仍可能调整。更新内容见[更新日志](https://github.com/Entropy-Increase-Team/skinloom/blob/main/CHANGELOG.md)，开发过程见[开发日志](/devlog)。
