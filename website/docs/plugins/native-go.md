---
title: 进阶指南：开发 Go 原生插件
sidebar_position: 3
---

# 进阶指南：开发 Tier 2 Go 原生系统级插件

当插件需要**独立关系型 SQL 建表与复杂迁移**、**连接外部数据库 / RCON / TCP 服务**、**HTTP/SSE/WebSocket 大流量流式代理**，或替换底层**对象存储 / 全文搜索 / 3D 渲染驱动**时，可以使用基于 `sdk/go` 的 **Tier 2 原生插件（`runtime: "native"`）**。

本指南以官方插件 [`bs-importer`](https://github.com/Entropy-Increase-Team/skinloom-plugins/tree/main/bs-importer) 为例说明完整开发流程。

## 1. 安全边界与架构约定（ADR 0049）

- **进程级隔离**：原生插件以独立 OS 子进程运行（基于 `hashicorp/go-plugin` 双向 gRPC）；插件崩溃或 OOM 不会影响主站，宿主会自动指数退避重启并在连续崩溃时熔断。
- **核心表严禁裸 SQL**：原生插件必须通过双向 gRPC `HostService`（`ctx.Users`、`ctx.Players`、`ctx.Textures`、`ctx.Score`、`ctx.Services.Call` 等）操作核心业务数据，严禁绕过宿主直接读写核心表。
- **三方言隔离 SQL 数据库（`sdk/go/plugindb`）**：声明 `storage.sql` 权限和 `native.database.required: true` 后，宿主自动为插件分配专属数据库空间与强约束表前缀 `plg_<safe_id>_`（SQLite 为独立 `<DataDir>/plugins-data/<id>/plugin.db` WAL 库；MySQL/PostgreSQL 支持独立 `[plugins.database]` 配置或主库前缀/Schema 隔离），卸载 `--purge` 时由宿主精准清理。
- **强供应链签名**：生产环境下，宿主强制拒绝未携带官方市场公钥或超级管理员 `plugins.trusted_keys` Ed25519 签名的 `native` 插件；提交至 [skinloom-registry](https://github.com/Entropy-Increase-Team/skinloom-registry) 后由官方隔离 Runner 统一交叉编译 5 个平台分包并加盖官方签名。

## 2. 声明清单 `plugin.json`

```json
{
  "$schema": "https://skinloom.cn/schema/plugin.v1.json",
  "id": "my-native-plugin",
  "kind": "plugin",
  "runtime": "native",
  "version": "1.0.0",
  "name": { "zh-CN": "原生插件示例", "en": "Native Example" },
  "apiVersion": 1,
  "engines": { "skinloom": ">=0.1.0 <2.0.0", "go": ">=1.26.10" },
  "web": "web/index.js",
  "native": {
    "protocolVersion": 1,
    "executable": "bin/my-native-plugin",
    "platforms": [
      "linux/amd64",
      "linux/arm64",
      "windows/amd64",
      "darwin/arm64",
      "darwin/amd64"
    ],
    "httpProxy": true,
    "database": { "required": true }
  },
  "permissions": ["admin.ui", "native.exec", "storage.sql", "http.stream", "users.read"]
}
```

也可以运行 `bunx create-skinloom-plugin my-native-plugin --template native-go` 直接生成完整工程模板。

## 3. 三方言隔离 SQL 迁移

在 `migrations/{sqlite,mysql,postgres}/001_init.sql` 中使用 `{{prefix}}` 占位符编写标准 goose SQL 迁移：

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS {{prefix}}jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS {{prefix}}jobs;
```

在 `Setup` 中只需一行调用，SDK 会按当前宿主方言自动选取对应目录、展开 `{{prefix}}` 并执行迁移；后续执行查询时 `db.Query` / `db.Exec` 也会自动把 `?` 绑定占位符在 PostgreSQL 上改写为 `$1, $2, ...`：

```go
//go:embed migrations
var migrationsFS embed.FS

func (p *MyPlugin) Setup(ctx *plugin.Context) error {
	db := ctx.DB()
	if err := db.Migrate(context.Background(), migrationsFS, "migrations"); err != nil {
		return err
	}
	// ...
}
```

## 4. 注册 JSON 接口、SSE 流式接口与跨插件调用

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
)

type MyPlugin struct{}

func (p *MyPlugin) Setup(ctx *plugin.Context) error {
	// 1. 标准 JSON HTTP 路由（宿主已完成会话鉴权、CSRF 校验与限流）
	ctx.HTTP.Post("/admin/run", func(reqCtx context.Context, req *plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		// 调用另一个插件暴露的服务（如 invitation-codes.importCodes）
		raw, err := ctx.Services.Call(reqCtx, "invitation-codes", "importCodes", []map[string]any{
			{"code": "MIGRATED-001"},
		})
		if err != nil {
			return nil, plugin.Reject("importCodes failed: " + err.Error())
		}
		return plugin.JSON(http.StatusOK, map[string]any{"result": string(raw)}), nil
	}, plugin.WithAuth(plugin.AuthSuperadmin))

	// 2. 流式 SSE / WebSocket 路由（需声明 native.httpProxy: true 与 http.stream 权限）
	ctx.HTTP.StreamFunc(http.MethodGet, "/admin/jobs/:id/stream", func(w http.ResponseWriter, r *http.Request) {
		params := plugin.StreamParams(r)
		viewer := plugin.ProxyViewer(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = fmt.Fprintf(w, "event: progress\ndata: {\"job\":%q,\"actor\":%d}\n\n", params["id"], viewer.UserID)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}, plugin.WithAuth(plugin.AuthSuperadmin))

	return nil
}

func main() {
	plugin.Serve(&MyPlugin{})
}
```

> **可选底层基础设施驱动**：只需在插件结构体上实现 `plugin.StorageProvider`、`plugin.SearchProvider` 或 `plugin.RenderProvider` 接口并声明对应权限（`provider.storage`、`provider.search`、`provider.render`），`plugin.Serve` 会自动将其注册为 gRPC Provider 服务。

## 5. 进程内单元测试（`sdk/go/plugintest`）

使用 `sdk/go/plugintest` 无需拉起子进程或真实宿主即可用标准 `go test` 完成全套单测（包含内存隔离 SQLite 库、Mock `HostService` 与流式 HTTP 路由测试）：

```go
func TestMyPlugin(t *testing.T) {
	ctx := context.Background()
	h := plugintest.New(t, &MyPlugin{},
		plugintest.WithPluginID("my-native-plugin"),
		plugintest.WithSQL(),
		plugintest.WithPermissions("admin.ui", "native.exec", "storage.sql", "http.stream"),
	)

	req := httptest.NewRequest(http.MethodGet, "/admin/jobs/42/stream", nil)
	req.Header.Set("X-SkinLoom-User-ID", "1")
	req.Header.Set("X-SkinLoom-User-Role", "superadmin")
	rec := h.DoStreamHTTP(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
}
```

## 6. 五平台交叉编译与分包打包

确保代码满足纯 Go（`CGO_ENABLED=0`）跨平台编译后，使用 `@skinloom/cli` 一键交叉编译并输出 5 个平台的独立分包与 `artifacts.json`：

```bash
# 本地编译当前平台与前端包
bunx skinloom-plugin build --native

# 交叉编译 5 大目标平台并输出独立分包 .slp（用于市场分发）
bunx skinloom-plugin pack --native --split-platforms \
  --targets linux/amd64,linux/arm64,windows/amd64,darwin/arm64,darwin/amd64 \
  --key ./keys/signing.key.pem \
  --out-dir ./release

# 或打成一个包含全部平台二进制的离线胖包（适合离线内网部署）
bunx skinloom-plugin pack --native --fat --key ./keys/signing.key.pem
```
