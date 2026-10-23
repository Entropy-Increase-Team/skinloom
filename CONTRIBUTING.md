# 参与贡献

感谢你愿意参与 SkinLoom！本文说明开发环境、常用命令和提交规范。

## 开发环境

- Go 1.26+（`go.mod` 的 toolchain 指令会自动下载所需版本）
- Node.js 20.9+（推荐 22 LTS）与 pnpm 10
- 无需 Docker；默认使用 SQLite

```bash
cd web && pnpm install && cd ..
go run ./cmd/skinloom up --dev      # 后端 + pnpm dev，访问 http://localhost:8080
```

数据（数据库、配置、密钥）在 `./data`，已被 git 忽略。改 `data/config.toml` 大多数键立即生效。

## 常用命令

| 目的 | 命令 |
|---|---|
| 只起后端 | `go run ./cmd/skinloom serve` |
| 测试 | `go test ./...`、`cd web && pnpm typecheck && pnpm test`（Linux/macOS 另可 `go test -race ./...`，需要 cgo） |
| 静态检查 | `go run honnef.co/go/tools/cmd/staticcheck@latest ./...`、`go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| 改了 `ent/schema` | `go generate ./ent`，然后 `go run ./internal/tools/migdiff -dialect sqlite -name <变更名>`（MySQL/PostgreSQL 用 `-dev <开发库 URL>`，或交给 CI 生成） |
| 改了 API | `go generate ./api && cd web && pnpm gen:api`（CI 会检查 `api/openapi.json` 与生成的客户端是否漂移） |
| 查看/修改运行时设置 | `go run ./cmd/skinloom settings get`、`settings set <key> <json>` |
| 临时管理员令牌（M1 前） | `go run ./cmd/skinloom admin token` |
| API 文档 | 运行后访问 <http://localhost:8080/api/docs> |
| 打发行包 | `bash deploy/packaging/package.sh [os/arch ...]` |
| 文档站 | `cd website && pnpm install && pnpm start` |

## 约定

- **保留路径**：`/api/`、`/oauth/`、`/.well-known/`、`/{name}.json`、`/csl/`、`/usm/`、`/skin/`、`/cape/`、`/textures/`、`/raw/`、`/avatar/`、`/preview/`、`/events`、`/healthz`、`/readyz`、`/plugins-assets/`、`/themes-assets/` 归后端所有（`internal/gateway`），前端路由不得占用。
- **不变量进数据库**：唯一性、外键、CHECK 写在 schema 里；写操作放在事务里；积分与计数用原子更新。
- **请求路径上不做运维动作**：不迁移、不清缓存、不写配置文件。
- **Hook 与插槽是公共 API**：新增随意，改名/删除/改载荷要走弃用流程并写 ADR。
- Go 代码 `gofmt`；注释写"为什么"。前端用 `@skinloom/ui` 组件与主题 token，不写死颜色。

## 提交与 PR

- 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/)：`feat(texture): …`、`fix(yggdrasil): …`、`docs: …`；破坏性变更用 `feat!:` 或正文 `BREAKING CHANGE:`。插件 API 相关加 scope `plugin-api`。
- 使用 DCO：`git commit -s`，表示你有权以 MIT 协议提交这些代码。
- PR 请说明动机与验证方式；涉及用户、站长或插件开发者的变化，同时更新文档（`website/`、`docs/`）与 `CHANGELOG.md` 的 Unreleased 段。
- 重要决策（选型、跨模块接口、公共 API 不兼容变更）请新增 ADR（`docs/adr/`）。

## 行为准则

参与本项目即表示同意遵守 [行为准则](CODE_OF_CONDUCT.md)。
