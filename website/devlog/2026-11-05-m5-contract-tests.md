---
slug: m5-contract-tests
authors: [maintainers]
title: 第 28 期 · M5 契约测试
date: 2026-11-05
tags: [devlog, M5]
---

M5 第三部分完成。游戏接口的每个回答都固定在一个黄金文件里，authlib-injector 的完整登录流程由一个真正的 Java 客户端检查，头像渲染有了像素快照。另外新增了一条命令，迁移时用它把 SkinLoom 和原来的 BS 站逐条对比。

<!-- truncate -->

## 黄金文件（ADR 0037）

- 测试站点有五个角色：Steve（经典皮肤加披风）、Alex（slim）、Naked（什么都没穿）、Hidden（穿着私有皮肤）、Banned（主人被封禁），另有一个不存在的名字。
- 45 个请求覆盖 CSL、USM、旧版 PNG、材质文件、`/raw` 和 Yggdrasil 的全部端点。回答先规范化：
  - JSON 按键排序；
  - 时间戳、令牌换成标记；
  - `textures` 属性解开 base64，签名用站点公钥验证，写成 `<signature valid>`；
  - PNG 写成尺寸加 SHA-256。
- 文件逐条对照 BS 和插件的源码审过。审的时候发现 yggdrasil-api 的 `textures` 里有 `"isPublic": true`，SkinLoom 原来没有，已经补上。
- 有意与 BS 不同的几处写进了 ADR，比如角色名在所有数据库上都不区分大小写、CSL 回答可以被 CDN 缓存。

## 和真实的 BS 站对比

02 §6 原本要求在真实的 BS 6 环境里录黄金文件，可开发环境里没有 PHP，也没有可用的 BS 站。于是把对比做成了命令：

```bash
skinloom import compare --bs https://old.example.com --skinloom http://127.0.0.1:17546 Steve Alex Notch
```

- 它像启动器和游戏服务器那样向两边提同样的问题，每边的签名用各自的公钥验证。
- 只比客户端读的东西：状态码、媒体类型和成功回答的正文。
- 迁移指南把它列为切换域名之前的一步，所以每次真实迁移都会对着真实的 BS 跑一遍。

## authlib-injector 端到端

`internal/httpapi/testdata/authlib/Harness.java` 是一个只用 JDK 的小程序，Java 11 以上直接用 `java Harness.java` 运行。测试会启动真实的服务器，程序依次检查：

- 元数据里的公钥；
- authenticate、refresh、validate；
- 按 Minecraft 的算法计算 serverId，走一遍 join 和 hasJoined；
- 每个属性的签名；
- `skinDomains` 白名单；
- 材质文件的 hash 是否与地址一致。

CI 的 Linux 任务要求有 Java，缺了就失败，而不是跳过。

## 渲染快照

- 参考皮肤是合成的：每个像素的颜色都不同，帽子层半透明。错位一个像素、取错一块、混合算错，快照都会不一样。
- 覆盖经典、旧版 64×32、高清三种皮肤，以及 slim 预览和披风预览。
- 阈值：差值超过 2 的像素不超过 0.5%，用来容纳 arm64 上融合乘加造成的边缘误差。
- 验证过快照确实有效：把 3D 头像侧面的明暗从 0.86 改成 0.80，三个 3D 快照全部失败。

## 下期计划

M5 第四部分：k6 压测，验证 04 §5.4 的性能目标。
