---
slug: /
title: 介绍
sidebar_position: 1
---

# 云织 SkinLoom

云织（SkinLoom）是一个开源、自托管的 Minecraft 皮肤站：玩家上传、分享皮肤与披风，管理自己的游戏角色；游戏客户端与启动器通过 CustomSkinLoader、authlib-injector（外置登录）等方式加载皮肤与登录。

项目以 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 为蓝本重新设计：保持外部协议兼容（老站可以迁移，游戏端不用改配置），同时重做数据模型、运行模型与插件体系。后端使用 Go，前端使用 Next.js，以 MIT 协议发布。

官网：<https://skinloom.cn> · 源码：<https://github.com/Entropy-Increase-Team/skinloom>

## 当前状态

项目已完成 **M1（账号）**，正在进行 **M2（材质与角色）**。一键启动、热加载、主题、设置、发行包等基础设施，安装向导、注册与登录、验证码、邮件、邮箱验证、找回密码、个人资料与登录设备、注销账号、后台用户管理与审计日志，以及材质上传、皮肤库、收藏与衣柜、材质管理、签到与用户中心、角色和 CustomSkinLoader / UniSkinMod / Legacy 游戏接口都已可用；外置登录等功能将陆续交付。进度见 [开发日志](/devlog) 与 [更新日志](https://github.com/Entropy-Increase-Team/skinloom/blob/main/CHANGELOG.md)。

## 从哪里开始

| 我是…… | 去看 |
|---|---|
| 想搭一个皮肤站 | [快速开始](getting-started/quick-start.md) → [配置](getting-started/configuration.md) → [部署](deployment/overview.md) |
| 想参与开发 | [开发环境](development/setup.md) → [架构概览](development/architecture.md) |
| 想写插件或主题 | [插件开发概览](plugins/overview.md) |
| 想了解设计取舍 | [设计文档](/design/) 与 [架构决策记录](/adr/) |
