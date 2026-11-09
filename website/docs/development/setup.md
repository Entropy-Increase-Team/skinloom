---
title: 开发环境
sidebar_position: 1
---

# 开发环境

## 准备

- Go 1.26+（`go.mod` 会自动下载需要的工具链）
- Node.js 24
- [bun](https://bun.sh)（没有也可以用 pnpm 或 npm）
- 不需要 Docker，默认用 SQLite

## 跑起来

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
cd web && bun install && cd ..      # 或 pnpm install / npm install
go run ./cmd/skinloom up --dev      # 打开 http://localhost:17546
```

`up --dev` 启动后端，并在 `web/` 里运行前端开发服务器（热更新照常可用）。请从 17546 端口访问，这样 `/api`、`/events` 与页面同源。数据在 `./data`，已被 git 忽略。

## 常用命令

| 目的 | 命令 |
|---|---|
| 后端测试 | `go test ./...` |
| 在 PostgreSQL / MySQL 上测试 | 设置 `SKINLOOM_TEST_POSTGRES` / `SKINLOOM_TEST_MYSQL` 为测试库地址，再 `go test -p 1 ./...` |
| 前端检查 | `cd web && bun run typecheck && bun run test` |
| 格式化 | `go fmt ./...`（用 go.mod 指定的工具链，与 CI 一致） |
| 静态检查 | `go vet ./...`、`go run honnef.co/go/tools/cmd/staticcheck@latest ./...` |
| 改了 `ent/schema` | `go generate ./ent`，再 `go run ./internal/tools/migdiff -dialect sqlite -name <变更名>` |
| 改了 API | `go generate ./api && cd web && bun run gen:api` |
| 只起后端 | `go run ./cmd/skinloom serve` |
| 打发行包 | `bash deploy/packaging/package.sh` |
| 文档站 | `cd website && bun install && bun run start` |

CI 会检查生成的文件（ent 代码、三种数据库的迁移、OpenAPI、前端 API 客户端）有没有和源码对上，忘了重新生成会失败。

## 包管理器

仓库以 bun 为准，只提交 `bun.lock`。pnpm 和 npm 也能安装和运行所有脚本，但不按锁文件锁定版本。不要提交 `pnpm-lock.yaml` 或 `package-lock.json`。

## 提交之前

- 读一遍[架构](./architecture.md)里"写代码时要遵守的规则"。
- 改了用户、站长或开发者能感知的行为：同步更新文档和 `CHANGELOG.md`，见[文档与日志](./docs-and-logs.md)。
- 提交规范见仓库的 [CONTRIBUTING.md](https://github.com/Entropy-Increase-Team/skinloom/blob/main/CONTRIBUTING.md)。
