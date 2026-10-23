---
title: 开发环境
sidebar_position: 1
---

# 开发环境

完整说明见仓库的 [CONTRIBUTING.md](https://github.com/Entropy-Increase-Team/skinloom/blob/main/CONTRIBUTING.md)，这里是摘要。

```bash
cd web && pnpm install && cd ..
go run ./cmd/skinloom up --dev     # http://localhost:8080
```

`up --dev` 启动后端并在 `./web` 运行 `pnpm dev`（Next.js 热更新照常可用）。请通过 8080 端口访问，这样 `/api`、`/events` 等后端路径与页面同源。

## 常用命令

```bash
go test ./...                                   # 后端测试
cd web && pnpm typecheck                         # 前端类型检查
go generate ./ent                                # 改了 ent/schema 之后
go run ./internal/tools/migdiff -dialect sqlite -name add_users   # 生成迁移
go generate ./api && cd web && pnpm gen:api      # 改了 API 之后
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
web/                 前端 pnpm 工作区：apps/web、packages/ui、packages/api-client
website/             本文档站
deploy/              发行包脚本、Dockerfile、Compose
docs/design/  docs/adr/   设计文档与架构决策记录
```
