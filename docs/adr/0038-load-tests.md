# 0038 压测：k6 场景、百万材质的种子数据与压测发现的修正

- 状态：已接受（2026-11-05）
- 相关：设计文档 04 §5.4；ADR 0021、0035

## 背景
04 §5.4 给出了 M5 要用压测验证的目标：
- CSL、USM、Legacy 的 p99 < 5 ms；
- 头像缓存命中 < 10 ms，未命中 < 50 ms；
- hasJoined < 20 ms；
- 百万材质时皮肤库列表 < 50 ms；
- 空闲内存 < 100 MB，冷启动 < 1 s。

## 决策
- **种子数据用 ent 直接写**（`internal/tools/benchseed`）：一万个账号，一百万个材质，十万个角色。
  - 前 64 个材质有真实的皮肤文件，角色轮流穿它们；其余材质只有 blob 记录（列表不读文件）。十分之一私有，类型轮换。
  - 写完执行 `VACUUM ANALYZE`（PostgreSQL）、`ANALYZE`（SQLite）或 `ANALYZE TABLE`（MySQL）。没有这一步，PostgreSQL 的自动清理会在压测中途开始，所有场景一起变慢。
  - SQLite 用时 5 分 37 秒，PostgreSQL 1 分 33 秒。
- **k6 脚本按固定速率压**（`bench/k6/game.js`），阈值就是目标，未达标时 k6 以非零状态退出：
  - 先跑 `avatar_miss`：每个文件、每种尺寸、2D 和 3D 各渲染一次，8 个并发；
  - 之后同时按固定速率请求：CSL 500/s、USM 与旧版 PNG 100/s、材质文件 200/s、头像命中 200/s、join 加 hasJoined 50/s、皮肤库列表 50/s（随机翻页、排序）；
  - 固定速率测的是稳定负载下的延迟，不是单机的最大吞吐。
- **`bench/run.sh` 串起整个流程**：编译、种子数据、首次启动（生成 4096 位签名密钥）、第二次启动计冷启动与空闲内存、k6、汇总。GitHub Actions 的 `Load test` 工作流手动触发，SQLite 与 PostgreSQL 各跑一遍。共享的 runner 噪声大，那里的数字只用来发现数量级的退步。
- **压测发现并修正的问题**：
  - **大尺寸头像编码太慢。** nativewebp 是纯 Go 实现，512 像素的 3D 头像编码要 200 ms 以上，未命中的 p99 是 230 ms。改为：
    - 超过 300×300 像素（`render.WebPMaxPixels`）的输出一律 PNG，即使客户端接受 WebP；
    - 超过 128×128 像素用最快的压缩级别；
    - 渲染池的并发数等于 `GOMAXPROCS`，以前是固定值。

    未命中 p99 降到 30 ms 左右。
  - **PostgreSQL 上翻页慢。** 皮肤库按点赞数翻页的游标条件只有一个 OR（`likes < L OR (likes = L AND id < I)`）。PostgreSQL 换用通用计划后，索引只能用来过滤可见性与类型，按点赞排序要扫一遍，p99 是 238 ms。加上等价但能用索引的 `likes <= L` 之后，计划用索引定位（`likes <= $6`），p99 是 1.6 ms。其它查询里没有同样形状的游标条件。
- **结果**（Windows 11 开发机，k6 与服务器在同一台机器，每个稳定场景 60 秒，p99）：

  | 场景 | 目标 | SQLite | PostgreSQL 18 |
  |---|---|---|---|
  | CSL | < 5 ms | 0.89 ms | 1.05 ms |
  | USM 与旧版 PNG | < 5 ms | 1.18 ms | 1.23 ms |
  | 材质文件 | < 5 ms | 通过 | 0.66 ms |
  | 头像命中 | < 10 ms | 1.03 ms | 1.09 ms |
  | 头像未命中 | < 50 ms | 31.85 ms | 29.15 ms |
  | hasJoined | < 20 ms | 1.16 ms | 1.55 ms |
  | 皮肤库（一百万材质） | < 50 ms | 1.66 ms | 1.59 ms |

  - 空闲内存 33 MB，压测后 74 MB（SQLite）与 68 MB（PostgreSQL）。压测后的内存里最多 64 MiB 是渲染缓存。
  - 冷启动：从启动到日志里出现 "SkinLoom is running" 是 143 ms。在 Windows 上，连接一个还没监听的本地端口要等约 2 秒才被拒绝，所以 `run.sh` 用的"轮询 `/readyz`"在这里会多算这段时间；Linux 上没有这个问题。
  - SQLite 那一轮在加 `likes <= L` 之前跑的；SQLite 的计划本来就用索引，这一处不影响它。
  - MySQL 只在 CI 里跑，没有在本机压测。

## 影响
- 大于 300×300 的头像和预览改为 PNG，较小的仍按 `Accept` 给 WebP。
- 渲染池大小随 CPU 数变化。
- 新的开发工具 `internal/tools/benchseed`、`bench/`，以及 `Load test` 工作流。
