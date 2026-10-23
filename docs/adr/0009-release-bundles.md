# 0009 发行包由脚本构建，前端依赖扁平化

- 状态：已接受（2026-10-23）
- 相关：ADR 0001；设计文档 04 §9

## 背景
桌面发行包 = `skinloom` 二进制 + Next.js standalone + **按平台下载的** Node 运行时 + 启动脚本。设计稿原计划用 GoReleaser。

## 决策
- 用 `deploy/packaging/package.sh` 构建发行包（任意平台的 bash 均可运行，CI 在 Linux 上交叉构建全部目标）：一次构建前端，逐目标交叉编译 Go、下载并缓存对应平台的 Node（`NODE_MIRROR` 可切国内镜像）、组装、打包、生成 `checksums.txt`。GoReleaser 难以优雅表达"每个目标附带不同的 Node 运行时"，签名/SBOM 将在发布工作流中单独接入。
- 前端工作区使用 pnpm `node-linker=hoisted`：standalone 输出不含符号链接。原因：pnpm 默认的符号链接布局在 Windows 上无法解压/复制（需要开发者模式），解引用复制又会破坏 Node 的依赖解析（M0 实测 `Cannot find module '@swc/helpers/...'`）。

## 影响
- Windows x64 包约 49 MB（解压 133 MB）。
- 开发时依赖也是扁平布局，行为与发行包一致。
