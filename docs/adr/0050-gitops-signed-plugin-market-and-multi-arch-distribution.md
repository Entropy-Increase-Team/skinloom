# 0050 GitOps 静态签名插件市场、五维环境筛选与多平台官方编译分发

- 状态：已接受（2026-11-19）
- 相关：设计文档 `10-dual-tier-plugin-and-market-plan.md` §1.2–§1.4、§2.6；ADR 0048（`.slp` 签名与多副本同步）、0049（双轨插件架构）

## 背景

随着双轨插件体系（Tier 1 跨平台 JS/WASM 与 Tier 2 原生 Go 多平台二进制）落地，插件与主题的分发面临三大核心问题：

1. **市场架构零运维与高可用**：社区早期无需维护重型动态市场后端数据库与账户体系，但需要同时满足国内外站点的低延迟访问、故障自动切换与防中间人（MITM）篡改。
2. **原生二进制供应链安全**：允许第三方开发者直接上传编译好的原生可执行文件存在极高的投毒与后门风险；生产站点必须确保所有来自官方市场的原生二进制均由公开源码在受控环境中编译并由官方私钥签名。
3. **异构平台与依赖兼容匹配**：不同自托管站点的操作系统/CPU 架构（`linux/amd64`、`linux/arm64`、`windows/amd64`、`darwin/arm64`、`darwin/amd64`）、SkinLoom 版本、Go 版本及已安装依赖各不相同，单节点与多架构集群均需自动筛选并拉取精确匹配的平台包。

## 决策

### 1. GitOps 静态签名插件市场（模式 A：`skinloom-registry` 独立仓库 & `RegistryClient`）

- 官方市场采用纯静态 GitOps 独立仓库（[`Entropy-Increase-Team/skinloom-registry`](https://github.com/Entropy-Increase-Team/skinloom-registry)）：
  - 开发者仅提交 `manifests/<id>.yaml`（声明开源 Git 仓库地址与**固定 40 位 Commit SHA**）。
  - **分级 PR 审核（`pr-check.yml`）**：非首次上架且未新增敏感/高危权限的纯 JS/主题更新由 CI 跑 `skinloom-plugin lint` 自动校验；首次上架、新增敏感/高危权限或任何 `runtime: "native"` 插件强制打上 `needs-security-review` 标签并运行 `go vet`、`govulncheck` 与 CodeQL 扫描，须经人工源码审计后方可合并。
  - **官方受控交叉编译与 Ed25519 签名（`publish.yml`）**：合并后由隔离 Runner 拉取固定 Commit SHA，以 `CGO_ENABLED=0` 交叉编译五平台独立分包（`--native --split-platforms`），使用受保护的官方 Ed25519 私钥对每个 `.slp` 包及总索引 `index.v1.json` 生成签名（`index.v1.json.sig`），同步发布至国内 CDN 主源（`https://registry.skinloom.cn`）与 GitHub 备用源。
- 宿主内置多源客户端 `internal/plugin/registry_client.go`（站点设置 `plugin.registries`）：
  - 支持主备镜像自动故障转移与 15 分钟内存 + 本地磁盘缓存；
  - 对官方市场源强制使用内置 `OfficialPublicKeyPEM` 验证 `index.v1.json.sig`，彻底防止中间人篡改索引或伪造阻断名单；
  - 每 6 小时定时巡检及手动刷新市场时执行 `EnforceBlockedPlugins`：仅采信通过签名验证的市场源 `blocked` 名单，命中已启用插件时立即热停用、记录 `plugin.blocked_disabled` 审计日志并向所有超级管理员发送站内安全通知。

### 2. 五维环境与依赖兼容性筛选引擎（`internal/plugin/compat.go`）

- 宿主拉取 `index.v1.json` 后，基于本机运行环境（`CurrentHostEnv`）对每个包版本执行五维校验并由 `SelectBestVersion` 选出最佳可安装版本：
  1. **平台匹配**：`runtime == "js"` 匹配 `"any"`；`runtime == "native"` 精确匹配 `GOOS/GOARCH` 产物。
  2. **SkinLoom SemVer 范围匹配**（`engines.skinloom`）。
  3. **API 与原生 gRPC 协议版本匹配**（`apiVersion == 1`、`nativeProtocolVersion == 1`）。
  4. **Go 基线版本校验**（`engines.go`）。
  5. **依赖树 DFS 解析**（`ResolveDependencyPlanWithBlocked`）：支持已装插件与市场可用包联合求解，自动产出按拓扑顺序排列的一键级联安装计划（`DependencyPlan`），并检测版本冲突、循环依赖与被阻断依赖。

### 3. 三平台分包为主、多架构集群按需回源与生产强制验签

- **单平台分包为主、全平台胖包（`--fat`）为辅**：市场分发默认采用单平台分包（单包上限 `50 MiB`，仅下载当前服务器架构二进制），同时支持离线内网使用 `--fat` 全平台胖包（上限 `150 MiB`）。
- **多架构集群同步（`PlatformPackages` + `FetchPlatformPackage`）**：`plugins` 表记录 `platform_packages`（各平台 `GOOS/GOARCH -> sha256` 映射）与 `registry_source`。当异构集群（如 `linux/amd64` + `linux/arm64`）中某节点从市场安装原生插件后，另一架构节点在 `SyncCluster` 时自动通过 `RegistryClient.FetchPlatformPackage` 回源拉取本机架构分包并严格核对 `platform_packages[hostPlatform]` 的 SHA-256。
- **生产模式原生包强制验签**：生产模式下（`AllowUnsignedNative == false`），`InstallPackage` 对 `runtime == "native"` 强制要求通过官方公钥或超管在 `plugins.trusted_keys` 中配置的可信 Ed25519 公钥验签，拒绝跳过签名安装未验签的原生二进制（`ErrUnsignedNativeForbidden`）。

## 备选方案

- **默认将所有 5 个平台的 Go 二进制打入同一个 `.slp` 胖包分发**：会导致每次市场下载体积膨胀至 60–100 MiB，浪费带宽与对象存储空间；采用按 `GOOS/GOARCH` 拆分的独立 `.slp` 分包并在 `index.v1.json` 的 `artifacts` 映射中按平台索引，兼顾了极速下载与多架构集群回源校验。
