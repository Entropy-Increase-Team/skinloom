---
title: 开发环境
sidebar_position: 1
---

# 开发环境

完整说明见仓库的 [CONTRIBUTING.md](https://github.com/Entropy-Increase-Team/skinloom/blob/main/CONTRIBUTING.md)，这里是摘要。

```bash
cd web && bun install && cd ..     # 没有 bun：pnpm install 或 npm install
go run ./cmd/skinloom up --dev     # http://localhost:17546
```

`up --dev` 启动后端并在 `./web` 运行前端开发服务器（Next.js 热更新照常可用）。包管理器按 bun → pnpm → npm 的顺序自动选择，也可以在 `data/config.toml` 用 `web.package_manager` 指定。请通过 17546 端口访问，这样 `/api`、`/events` 等后端路径与页面同源。

## 包管理器

仓库以 **bun** 为准（提交的锁文件是 `web/bun.lock`、`website/bun.lock`，CI 与发行包都用它）。没有安装 bun 时，pnpm 与 npm 也能安装和运行所有脚本（`pnpm test`、`npm run build` 等），但不会按锁文件锁定版本，CI 也会定期验证这两条回退路径。不要提交 `pnpm-lock.yaml` 或 `package-lock.json`。

## 常用命令

```bash
go test ./...                                   # 后端测试
cd web && bun run typecheck && bun run test     # 前端类型检查与单测
go generate ./ent                                # 改了 ent/schema 之后
go run ./internal/tools/migdiff -dialect sqlite -name add_users   # 生成迁移
go generate ./api && cd web && bun run gen:api  # 改了 API 之后
bash deploy/packaging/package.sh                 # 打当前平台的发行包
```

## 仓库结构

```text
cmd/skinloom/        命令行入口
internal/            后端：app、config、gateway、launcher、hook、settings、theme、httpapi……
ent/schema/          数据模型
migrations/          三种数据库的迁移文件（生成后提交）
themes/              内置主题包
api/openapi.json     生成的 OpenAPI 文档
web/                 前端工作区（bun.lock）：apps/web、packages/ui、packages/api-client
website/             本文档站
deploy/              发行包脚本、Dockerfile、Compose
docs/design/  docs/adr/   设计文档与架构决策记录
```
