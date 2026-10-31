# 0033 系统服务命令、Helm Chart 与部署冒烟测试

- 状态：已接受（2026-10-31）
- 相关：设计文档 02 F76；04 §9、§13（M4 验收）；ADR 0009、0013、0029

## 背景
M4 最后一部分（ADR 0029）：Helm、`skinloom service install`，以及"四种部署形态按文档从零部署成功"这条验收。
- 后台常驻靠发行包 `service/` 目录里手写的三份文件：systemd 单元、launchd plist、Windows 任务计划脚本。Windows 上其实是计划任务而不是服务；三份文件各自要改路径，和程序里的逻辑也是两份。
- Kubernetes 只有 Kustomize 清单，改参数要改文件。
- 部署文件在 CI 里只做了语法检查（`compose config`、kubeconform），从来没有真正启动过。本机不运行 Docker（ADR 0013 时的约定）。

## 决策
- **`skinloom service install | uninstall | start | stop | restart | status`**，用 kardianos/service v1.3.0（MIT），一套代码覆盖三种系统：
  - Linux：systemd 单元由程序生成，沿用原单元的全部沙箱选项（`ProtectSystem=strict`，只能写 `data/` 与前端缓存）。默认以 `skinloom` 用户运行（`--user` 可改），这个用户要先建好；安装时把数据目录交给它。
  - macOS：`/Library/LaunchDaemons` 下的系统守护进程，开机运行、退出后重启。
  - Windows：真正的 Windows 服务，以 LOCAL SERVICE 运行，延迟自动启动，失败后 5 秒重启。安装时用 icacls 只给它 `data\` 与前端缓存的写权限。
  - 服务运行的是 `skinloom service run --data <目录>`，与 `skinloom up` 相同，另外响应服务管理器的启动与停止。站点自己退出时（比如配置错误）进程也退出，让服务管理器重启，而不是看起来在运行、实际什么都不服务。
  - 安装先注册服务、再改权限；改权限失败就把服务删掉。没有管理员权限时直接失败，并提示用管理员终端或 sudo。
  - 发行包的 `service/` 目录和 `deploy/service/` 下的三份文件删除。
- **日志文件 `log.file`**（冷键，相对于数据目录）：配置了就同时写文件；以服务运行且没有配置时，写 `logs/skinloom.log`，因为服务的控制台没人看，一次性安装链接会丢。启动时文件超过 20 MB，就保留一份为 `.1`。先写文件再写控制台：Windows 服务的标准错误可能是无效句柄，`io.MultiWriter` 遇到第一个错误就会停。
- **`/readyz` 等前端就绪**：`web.mode` 为 managed 或 dev（前端是本进程的子进程）时，`/readyz` 还要能连上前端端口。否则启动后有一秒左右，就绪探针已经通过，页面却返回 502。external（前后端分离）时前端有自己的探针，后端不依赖它。
- **Helm Chart** `deploy/helm/skinloom`：
  - `mode: allInOne`（默认，单 Pod、SQLite、5 Gi 卷）或 `split`（后端加前端，前端带 PDB，可选 HPA）。
  - 数据库：`database.driver`，PostgreSQL / MySQL 的 DSN 从已有的 Secret 读取，不进 values。
  - 其余：`ingress.*`、`persistence.*`（卸载 Chart 时保留数据卷）、`trustedProxies`、`server.extraEnv`（例如 S3）、资源、调度。安全设置与 Kustomize 清单一致：非 root、去掉全部 capabilities、seccomp、不挂载 ServiceAccount 令牌。
  - 互相矛盾的参数在渲染时报错并说明原因：SQLite 配多个后端副本；多副本却用 ReadWriteOnce 卷；allInOne 配多副本；外部数据库没给 Secret。
  - Kustomize 清单保留，给不想装 Helm 的人用。
  - 不做迁移 Job：后端启动时带锁执行迁移（ADR 0003）。没有指标接口，所以也不做 ServiceMonitor。
- **部署冒烟测试**：`deploy/smoke/smoke.sh` 像新站长一样使用站点，依次检查：等待就绪、首页能渲染、从日志里取安装链接完成安装、登录、创建角色、用 CustomSkinLoader 接口取回角色、系统状态没有 error。
  - 新工作流 `deploy-smoke.yml` 在 GitHub 的机器上对每种部署方式跑一遍：
    - Compose 的三个变体（minimal、standard、postgres）；
    - Helm 在 kind 集群上（allInOne + SQLite，split + PostgreSQL）；
    - Linux 发行包：先按文档前台启动，再用 `skinloom service install` 注册为 systemd 服务。
  - 镜像都从当前提交构建，不拉取已发布的镜像。
  - 原有的 `Deployment files` 任务加上 Helm 的 lint、多组参数渲染、kubeconform 校验和非法参数的拒绝检查。
- **Windows 发行包的 zip** 改用 Windows 10 以上自带的 `tar.exe`（没有 `zip` 时）。Windows PowerShell 的 `Compress-Archive` 把 `\` 写成路径分隔符，`unzip` 会警告，macOS 与部分工具会解压错。

## 影响
- 新依赖 `github.com/kardianos/service`；新命令 `service`；新配置 `log.file`。
- 发行包不再有 `service/` 目录，文档与 README 改为 `skinloom service install`。
- Docker、Compose、Helm 的从零部署由 CI 的冒烟测试验证，本机只验证发行包。
