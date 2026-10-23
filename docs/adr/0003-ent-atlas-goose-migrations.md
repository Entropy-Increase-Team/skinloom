# 0003 ent 建模，Atlas 生成迁移，goose 执行

- 状态：已接受（2026-10-23）
- 相关：设计文档 04 §2.3、§5；03 §7

## 背景
需同时支持 SQLite / MySQL / MariaDB / PostgreSQL；BS 的问题根源之一是数据库没有约束、迁移靠运行时探测。

## 决策
- 数据模型写在 `ent/schema`，`go generate ./ent` 生成代码（用 `entc` 程序化生成，避免 ent CLI 的依赖冲突）。
- 迁移文件由 `internal/tools/migdiff`（ent + Atlas diff，goose 格式）按方言生成到 `migrations/{sqlite,mysql,postgres}/`，提交进仓库、人工审阅。
- 运行时用 goose 执行内嵌的迁移：PostgreSQL 用会话锁、MySQL 用表锁防并发迁移；SQLite 迁移前自动 `VACUUM INTO` 快照到 `data/backups/`。
- 启动时自动迁移；绝不在请求路径上迁移。
- 所有时间列在 MySQL 上使用 `DATETIME(6)`（`ent/schema/fields.go` 的 `datetimeType`）：ent 默认的 `TIMESTAMP` 在 2038 年溢出且受会话时区影响；驱动始终以 UTC 读写。（2026-10-23 代码审查补充）

## 影响
- SQLite 迁移可在本地用内存库生成；MySQL / PostgreSQL 需要一次性开发库，CI 用服务容器生成并做漂移检查（`git diff --exit-code migrations/`）。
- M0 的 MySQL / PostgreSQL 初始迁移为手写，由 CI 漂移检查验证。
