# 0013 默认端口、部署清单与发布渠道

- 状态：已接受（2026-10-23，项目负责人提出）
- 相关：ADR 0001、0009、0011；设计文档 04 §9

## 背景
负责人指出：默认端口 8080 / 3000 几乎必然与站长机器上的其它程序冲突；README 只教了一种 Docker 方式，没有教 Kubernetes / k3s，也没说明二进制如何后台常驻；仓库只有发布工作流，从未产生过 Release；文档站没有发布。

## 决策
- **端口**：对外 17546（1 + 手机九宫格上的 S-K-I-N），前端内部 17547。最初选过四位数的 7546，负责人指出四位数端口大多已被常见软件使用（7546 在 IANA 登记为 Cisco Fabric Services），改为五位数。五位数的取值受三个范围约束：Linux 默认临时端口 32768–60999、Windows 动态端口 49152–65535（Hyper-V/WSL 也在这一段保留端口块）、Kubernetes NodePort 30000–32767，因此只在 10000–29999 里选；同时远离 Minecraft 的 25565 一带。IANA 在 17540–17549 没有任何登记；有些 VoIP 软件的媒体端口段覆盖这里（如 Asterisk 10000–20000），但那是 UDP，与 TCP 不冲突。在 Windows 上核对过保留端口段不含这两个端口。
- **Docker Compose 为首选部署方式**，提供三份：`minimal`（一体镜像 + SQLite）、`standard`（前后端分离 + MariaDB）、`postgres`。镜像版本由 `SKINLOOM_VERSION` 选择，同时保留 `build` 段：拉不到镜像（首个版本发布前）时用源码构建。Valkey 在真正用到之前不放进模板。
- **Kubernetes / k3s 用 Kustomize 清单**（`deploy/kubernetes/{all-in-one,split}`），`kubectl apply -k` 即可，不需要额外工具；Helm Chart 仍在 M4。清单默认非 root、去掉全部 capabilities、seccomp RuntimeDefault，用 `/readyz`、`/healthz` 做探针；把 k3s 的 Pod 网段设为可信代理。数据库密码只存在 Secret，后端 DSN 用 `$(DB_PASSWORD)` 展开拼接。后端暂为单副本（数据目录在 RWO 卷上）。
- **二进制后台常驻**：发行包附带 `service/`：systemd 单元（专用用户 + 沙箱选项，只能写 `data/` 与前端缓存）、launchd agent、Windows 计划任务安装脚本（LOCAL SERVICE 身份、开机启动、失败重试）。`skinloom service install` 仍在 M4。
- **健康检查**：新增 `skinloom healthcheck`（请求本机 `/readyz`），后端镜像是 distroless、没有 shell 与 curl，也能声明 `HEALTHCHECK`。
- **列表型配置的环境变量**用逗号分隔（`SKINLOOM_SERVER__TRUSTED_PROXIES=a,b`）；此前整串被当成一个值，容器与集群里无法配置可信代理。
- **发布**：推送 `v*` 标签即发布正式 Release（不再是草稿），带 `-` 的标签标记为预发布；附六个平台的完整发行包、仅后端的单文件程序与 `checksums.txt`。镜像标签：`x.y.z`、`x.y`（仅正式版）、`latest`（最新发布，含预发布，以便 1.0 之前也能用 latest）、`edge`（main 分支每次提交）。Go 构建阶段改为在构建机架构上交叉编译（不再经 QEMU 模拟），前端阶段仍按目标架构构建，保证原生模块正确。
- **文档站**用 GitHub Pages 发布（`docs.yml`）：站点地址与 base URL 取自 `actions/configure-pages`，配置自定义域名（skinloom.cn）后在根路径提供，未配置时在 `/skinloom/` 下。设计文档与 ADR 之间的仓库相对链接由一个 remark 插件改写为站内路径，两种 base URL 下都不会断。

## 影响
- 已有站点的 `data/config.toml` 保留原来的端口（配置文件优先于默认值），不受影响；新站点使用 17546。
- 启用 Pages 需要仓库管理员在 Settings → Pages 选择 "GitHub Actions"（GITHUB_TOKEN 无权代为开启）；自定义域名需要在 DNS 添加记录。
- main 分支的每次提交都会构建三个多架构镜像（`edge`），文档与 Markdown 修改除外。
