# 参与贡献

感谢你愿意参与云织！本文是开发者需要的全部约定；项目结构和设计规则见文档站的[架构](https://skinloom.cn/docs/development/architecture)。

## 开发环境

- Go 1.26+（`go.mod` 的 toolchain 指令会自动下载所需版本）
- Node.js 24
- [bun](https://bun.sh)（仓库只提交 `bun.lock`；没有 bun 时可用 pnpm 10 或 npm，但不按锁文件安装）
- 不需要 Docker，默认用 SQLite

```bash
cd web && bun install && cd ..
go run ./cmd/skinloom up --dev      # 后端 + 前端开发服务器，访问 http://localhost:17546
```

数据（数据库、配置、密钥）在 `./data`，已被 git 忽略。

## 目录

```text
cmd/skinloom/         入口
internal/app          装配与生命周期        internal/cli       命令行
internal/httpapi      HTTP 层（doc.go 有文件索引）
internal/modules/*    业务模块              internal/platform/*  基础设施
internal/gateway      唯一入口与路径分流     internal/hook      领域事件
ent/schema            数据模型              migrations/        三种数据库的迁移
web/                  前端                  website/           文档站与开发日志
docs/adr              架构决策记录          docs/design        设计文档
```

## 常用命令

| 目的 | 命令 |
|---|---|
| 测试 | `go test ./...`；前端 `cd web && bun run typecheck && bun run test` |
| 在 PostgreSQL / MySQL 上测试 | 设置 `SKINLOOM_TEST_POSTGRES` / `SKINLOOM_TEST_MYSQL` 为测试库地址，`go test -p 1 ./...` |
| 格式化 | `go fmt ./...` |
| 静态检查 | `go vet ./...`、`go run honnef.co/go/tools/cmd/staticcheck@latest ./...`、`go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| 改了 `ent/schema` | `go generate ./ent`，再 `go run ./internal/tools/migdiff -dialect sqlite -name <变更名>`（MySQL / PostgreSQL 加 `-dev <开发库地址>`，或交给 CI 生成） |
| 改了 API | `go generate ./api && cd web && bun run gen:api` |
| 运行时设置 | `go run ./cmd/skinloom settings get`、`settings set <key> <json>` |
| API 文档 | 运行后访问 <http://localhost:17546/api/docs> |
| 打发行包 | `bash deploy/packaging/package.sh [os/arch ...]` |
| 文档站 | `cd website && bun install && bun run start` |

`go fmt` 使用 go.mod 指定的工具链，与 CI 一致；PATH 上的 `gofmt` 可能是旧版本，格式化结果不同，会让 CI 失败。

CI 会检查生成的文件（ent 代码、三种数据库的迁移、OpenAPI、前端 API 客户端）是否与源码一致，并在 MySQL 和 PostgreSQL 上各跑一遍测试。

## 代码约定

- **不变量进数据库**：唯一性、外键、CHECK 写在 schema 里；积分与计数用原子更新。
- **事务用 `db.WithTx`**，并且**先加锁、再判断**：按"材质 → 用户（id 升序）→ 角色 → 其它"的顺序加锁，决定结果的数据在加锁之后重新读取；审计记录和领域事件（`hook.Event.Publish`）写在同一个事务里（ADR 0026–0028）。
- **每个新事务都要有并发测试**（`dbtest.ForEach`），并在 PostgreSQL 上跑过。
- **请求路径上不做运维动作**：不迁移、不清缓存、不写配置文件。
- **保留路径归后端**：`/api/`、`/oauth/`、`/.well-known/`、`/{name}.json`、`/csl/`、`/usm/`、`/skin/`、`/cape/`、`/textures/`、`/raw/`、`/avatar/`、`/preview/`、`/events`、`/healthz`、`/readyz`、`/plugins-assets/`、`/themes-assets/`（`internal/gateway`），前端路由不得占用。
- **Hook 与插槽是公共 API**：新增随意，改名、删除、改载荷要走弃用流程并写 ADR。
- **修 bug 先写能复现它的测试**，确认它在旧代码上失败。
- 注释写"为什么"。前端用 `@skinloom/ui` 组件与主题 token，不写死颜色。

## 提交与 PR

- 提交信息遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/)：`feat(texture): …`、`fix(yggdrasil): …`、`docs: …`；破坏性变更用 `feat!:` 或正文 `BREAKING CHANGE:`。插件 API 相关加 scope `plugin-api`。
- 使用 DCO：`git commit -s`，表示你有权以 MIT 协议提交这些代码。
- PR 请说明动机与验证方式。用户、站长或开发者能感知的变化，同时更新文档（`website/docs/`）和 `CHANGELOG.md` 的 Unreleased 段。
- 重要决策（选型、跨模块接口、公共 API 的不兼容变更）新增 ADR（`docs/adr/`）。

## 发布

推送 `v*` 标签后，[Release 工作流](.github/workflows/release.yml) 构建六个平台的发行包（Windows / macOS / Linux × amd64 / arm64）、仅后端的单文件程序与 `checksums.txt`，发布到 Releases（带 `-` 的标签标记为预发布），并推送多架构镜像 `ghcr.io/entropy-increase-team/{skinloom,server,web}`。main 分支的每次提交更新 `edge` 镜像。

## 行为准则

参与本项目即表示同意遵守[行为准则](CODE_OF_CONDUCT.md)。
