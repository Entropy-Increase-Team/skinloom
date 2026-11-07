---
slug: m5-load-tests
authors: [maintainers]
title: 第 29 期 · M5 压测
date: 2026-11-05
tags: [devlog, M5]
---

M5 第四部分完成。我们在一个有一百万个材质的站点上用 k6 压测，04 §5.4 的目标在 SQLite 和 PostgreSQL 上全部达到。压测还找出了两个慢点，都已修正。

<!-- truncate -->

## 怎么测（ADR 0038）

- **种子数据**：一万个账号，一百万个材质，十万个角色，用 ent 直接写入。写完立刻执行 `ANALYZE`（PostgreSQL 上是 `VACUUM ANALYZE`）。
- **k6 按固定速率请求**：CSL 每秒 500 次，材质文件和缓存命中的头像各 200 次，USM 与旧版 PNG 100 次，join 加 hasJoined、皮肤库列表各 50 次。在这之前先把每个头像渲染一次，测缓存未命中的情况。
- **阈值就是目标**，达不到时 k6 以非零状态退出。`bench/run.sh` 把编译、种子数据、冷启动、内存测量和 k6 串在一起；GitHub Actions 里有手动触发的 `Load test` 工作流。

## 结果（p99）

| 场景 | 目标 | SQLite | PostgreSQL 18 |
|---|---|---|---|
| CSL | < 5 ms | 0.89 ms | 1.05 ms |
| USM 与旧版 PNG | < 5 ms | 1.18 ms | 1.23 ms |
| 头像命中 | < 10 ms | 1.03 ms | 1.09 ms |
| 头像未命中 | < 50 ms | 31.85 ms | 29.15 ms |
| hasJoined | < 20 ms | 1.16 ms | 1.55 ms |
| 皮肤库（一百万材质） | < 50 ms | 1.66 ms | 1.59 ms |

- 空闲内存 33 MB，压测后约 70 MB，其中最多 64 MiB 是渲染缓存。
- 冷启动 143 ms。

数字来自一台 Windows 开发机，k6 和服务器跑在同一台机器上。

## 压测找出的问题

- **大头像编码太慢。** 纯 Go 的 WebP 编码器处理 512 像素的 3D 头像要 200 ms 以上，缓存未命中的 p99 是 230 ms。现在：
  - 超过 300×300 像素的图输出 PNG；
  - 128 像素以上用最快的压缩级别；
  - 渲染并发数等于 CPU 数。

  改完以后 p99 约 30 ms。小头像照旧是 WebP。
- **PostgreSQL 上翻页慢。** 皮肤库按点赞数翻页的游标条件是一个 OR，PostgreSQL 换成通用计划后用不上点赞数的索引，p99 是 238 ms。加上一个等价的 `likes <= L` 之后降到 1.6 ms。
- **自动清理。** PostgreSQL 第一轮压测全部超标：种子数据刚写完，自动清理正好在压测中途开始。所以种子工具写完就执行 `VACUUM ANALYZE`。

## 下期计划

M5 第五部分：安全自查，包括 CSP、自定义 JavaScript，以及 M4 审查时记下的遗留问题。
