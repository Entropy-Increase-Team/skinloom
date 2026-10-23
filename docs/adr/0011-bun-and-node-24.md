# 0011 包管理器 bun（回退 pnpm / npm），Node.js 24

- 状态：已接受（2026-10-23，项目负责人提出）
- 相关：ADR 0001、0009；取代 0009 中"前端工作区使用 pnpm"的部分

## 背景
负责人要求：前端包管理器用 bun；用户机器上没有 bun 时回退到 pnpm 或 npm；Node.js 统一使用 24 的最新版本。

## 决策
- **bun 为主**：`web/` 与 `website/` 只提交 `bun.lock`；CI、Docker 镜像、发行包构建都用 `bun install --frozen-lockfile`，可复现。
- **pnpm / npm 回退**：
  - 工作区在根 `package.json` 的 `workspaces` 字段声明（bun、npm 读取），同时保留 `pnpm-workspace.yaml`（pnpm 读取）。
  - 内部依赖写成 `"*"` 而不是 `workspace:*`：npm 不支持 `workspace:` 协议；`*` 在三者下都会链接到工作区包（已分别实测）。pnpm 通过 `.npmrc` 的 `link-workspace-packages=true` 实现。
  - 不写 `packageManager` 字段：pnpm 9+ 看到它指定别的包管理器会直接拒绝运行。
  - 根脚本（`dev`/`build`/`test`/`typecheck`/`gen:api`）通过 `web/scripts/ws.mjs` 在各工作区包里执行，识别调用者是 bun、pnpm 还是 npm，所以 `bun run test`、`pnpm test`、`npm test` 都可以用。
  - 回退安装不按锁文件锁版本；`pnpm-lock.yaml`、`package-lock.json` 被 `.gitignore` 忽略。CI 的 "Web (pnpm/npm fallback)" 任务持续验证回退路径能安装、类型检查和跑单测。
- **依赖布局保持扁平**：`bunfig.toml` 设 `install.linker = "hoisted"`，与 0009 中的理由相同（standalone 输出要能直接复制进 zip 和镜像）。CI 检查 standalone 输出里没有符号链接。
- **`skinloom up --dev`** 按 bun → pnpm → npm 的顺序选择包管理器，在 `web/apps/web` 里运行该包的 `dev` 脚本；也可以用热加载的 `web.package_manager` 指定。依赖未安装时给出对应的安装命令。
- **Node.js 24**：发行包内置 Node 24.22.1（LTS），Docker 镜像基于 `node:24-alpine`，CI 使用 Node 24，`engines.node` 为 `>=24`。bun 只做包管理和脚本调度，Next.js 仍运行在 Node 上（Next.js 对 bun 运行时的支持不在我们的兼容范围内）。
- GitHub Actions 升级到运行在 Node 24 上的版本（checkout/setup-go/setup-node v7 等），消除 Node 20 弃用告警。

## 影响
- 贡献者需要安装 bun 才能得到与 CI 一致的依赖版本；回退路径能用，但可能装到同一范围内更新的版本。
- 在国内网络下，镜像源可能落后于官方源（实测 npmmirror 尚未同步 `@orval/effect@8.44.0`），需要时临时切回官方源。
