<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">云织 SkinLoom</h1>

<p align="center">开源、自托管的 Minecraft 皮肤站 · Go 后端 + Next.js 前端 · MIT</p>

<p align="center"><a href="https://skinloom.cn">官网与文档 skinloom.cn</a> · <a href="README.en.md">English</a> · <a href="https://github.com/Entropy-Increase-Team/skinloom/releases">下载</a> · <a href="docs/design/README.md">设计文档</a> · <a href="docs/adr/README.md">架构决策</a> · <a href="CHANGELOG.md">更新日志</a></p>

---

> **状态：开发中（M1 账号已完成，下一步 M2 材质与角色）。** 尚不能用于生产。路线图见 [设计文档 04 §13](docs/design/04-rewrite-plan.md#13-路线图)。

SkinLoom 是以 [Blessing Skin](https://github.com/bs-community/blessing-skin-server) 为蓝本重新设计实现的皮肤站：上传、分享皮肤与披风，管理游戏角色，并为 CustomSkinLoader、authlib-injector（外置登录）等客户端提供皮肤与认证服务。目标是在保持对 Blessing Skin 外部协议兼容（老站可迁移、游戏端无需改配置）的同时，从数据模型、运行模型到插件体系全面重做。

## 特性（规划）

- **一键运行**：Docker Compose、Kubernetes / k3s，或 Windows / macOS / Linux 发行包解压即用（内置 Node 运行时）。
- **热加载**：站点设置、主题、配置文件、插件安装/升级/卸载均无需重启，已打开的页面秒级刷新。
- **兼容 Blessing Skin**：CSL / USM / Legacy / Yggdrasil 协议与 OAuth API 兼容，提供只读导入工具，玩家 UUID、密码、皮肤链接不变。
- **统一的插件标准**：TypeScript 一门语言、一个 SDK、一个 CLI，后端在沙箱中运行，前端可扩展页面、插槽与首页区块，主题同一标准。
- **多数据库**：SQLite（零依赖）、MySQL / MariaDB、PostgreSQL。

## 部署

| 方式 | 适合 | 数据库 | 说明 |
|---|---|---|---|
| [Docker Compose](#方式一docker-compose推荐) **推荐** | 绝大多数站点、NAS | SQLite / MariaDB / PostgreSQL | 三份现成的 compose 文件，开机自启、数据在卷里 |
| [docker run](#方式二docker-run) | 已有 Docker 环境、想自己编排 | 任意 | 一条命令起一个容器，或前后端分开两个容器 |
| [Kubernetes / k3s](#方式三kubernetes--k3s) | 集群、需要横向扩容 | SQLite / PostgreSQL | Kustomize 清单，k3s 开箱即用 |
| [发行包](#方式四发行包二进制) | Windows / macOS 桌面、没有 Docker 的 Linux | 任意 | 解压即用；附 systemd / launchd / 计划任务配置，后台常驻 |
| [源码运行](#从源码运行开发) | 开发者 | SQLite | `go run ./cmd/skinloom up --dev` |

不论哪种方式：

- **端口**默认 **17546**（1 + 手机九宫格上的 S-K-I-N）。四位数端口大多已经被常见软件占用，所以用五位数；它同时避开了系统分配临时端口的范围（Linux 32768 起、Windows 49152 起）和 Kubernetes NodePort 范围（30000–32767），IANA 也没有登记。改端口见 [端口](#端口)。
- **首次启动**会在日志里打印一条一次性安装链接 `http://<地址>:17546/setup?token=…`，打开它设置站点名称并创建管理员账号。链接 24 小时有效，用后作废；找不到时运行 `skinloom setup-link` 再生成一个。只有能看到日志的人才能完成安装，公网部署不会被抢装。
- **数据**（数据库、配置、密钥、上传文件）都在数据目录 `/data`（容器）或 `data/`（发行包）。**备份就是备份这个目录**（外部数据库另行备份）。
- **升级**：换新镜像或新程序后重启即可，数据库迁移在启动时自动执行（SQLite 会先自动快照）。

> 第一个版本发布之前，镜像仓库里还没有镜像：请按下文"从源码构建镜像"的方式运行，或使用 `edge`（main 分支最新构建）。

### 方式一：Docker Compose（推荐）

**最小部署**（一个容器，SQLite，适合个人站和中小站）：

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # 一次性安装链接
```

打开 <http://服务器地址:17546>。容器设置了 `restart: unless-stopped`，Docker 开机自启后站点也随之启动。

**前后端分离 + MariaDB**（中大站，前端可以多开几个）：

```bash
mkdir skinloom && cd skinloom
base=https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/standard
curl -fsSLO $base/compose.yaml && curl -fsSL $base/.env.example -o .env
sed -i "s/^DB_PASSWORD=.*/DB_PASSWORD=$(openssl rand -hex 24)/" .env   # 生成数据库密码
docker compose up -d
docker compose logs server | grep setup
docker compose up -d --scale web=2     # 需要时增加前端实例
```

**PostgreSQL**：把上面的 `standard` 换成 `postgres`，其余相同。

常用操作：

```bash
docker compose pull && docker compose up -d       # 升级
docker compose exec skinloom /app/skinloom setup-link     # 重新生成安装链接（分离部署里服务名是 server，路径为 /usr/local/bin/skinloom）
SKINLOOM_VERSION=0.1.0 docker compose up -d       # 固定版本（默认 latest；edge = main 分支）
SKINLOOM_PORT=8000 docker compose up -d           # 换宿主机端口
```

备份数据卷（最小部署）：

```bash
docker compose stop
docker run --rm -v skinloom_data:/data -v "$PWD":/backup alpine tar czf /backup/skinloom-data.tgz -C /data .
docker compose start
```

**从源码构建镜像**（还没有发布版本时，或想用自己的修改）：

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker compose -f deploy/compose/minimal/compose.yaml up -d --build
```

### 方式二：docker run

一个容器跑前后端（SQLite）：

```bash
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  ghcr.io/entropy-increase-team/skinloom:latest
docker logs skinloom 2>&1 | grep setup
```

使用已有的 MySQL / MariaDB 或 PostgreSQL，加两个环境变量即可：

```bash
  -e SKINLOOM_DATABASE__DRIVER=mysql \
  -e SKINLOOM_DATABASE__DSN='skinloom:密码@tcp(数据库地址:3306)/skinloom' \
# 或
  -e SKINLOOM_DATABASE__DRIVER=postgres \
  -e SKINLOOM_DATABASE__DSN='postgres://skinloom:密码@数据库地址:5432/skinloom?sslmode=disable' \
```

前后端分成两个容器（`server` 是唯一入口，把页面请求转给 `web`）：

```bash
docker network create skinloom
docker run -d --name skinloom-web --network skinloom --restart unless-stopped \
  -e SKINLOOM_API_INTERNAL=http://skinloom-server:17546 \
  ghcr.io/entropy-increase-team/web:latest
docker run -d --name skinloom-server --network skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  -e SKINLOOM_WEB__MODE=external -e SKINLOOM_WEB__URL=http://skinloom-web:17547 \
  ghcr.io/entropy-increase-team/server:latest up
```

镜像：`skinloom`（前后端一体）、`server`（仅后端，distroless，非 root）、`web`（仅前端），均为 amd64 / arm64 多架构，自带健康检查。标签：`x.y.z` / `x.y` 为正式版本，`latest` 为最新发布，`edge` 为 main 分支最新构建。

### 方式三：Kubernetes / k3s

仓库提供两套 Kustomize 清单，在 k3s 上开箱即用（自带的 Traefik 作为 Ingress、local-path 提供存储）：

| 目录 | 内容 |
|---|---|
| [`deploy/kubernetes/all-in-one`](deploy/kubernetes/all-in-one) | 一个 Pod 跑前后端，SQLite，5 Gi 数据卷 |
| [`deploy/kubernetes/split`](deploy/kubernetes/split) | 后端 + 前端（2 副本，可扩容，带 PodDisruptionBudget）+ PostgreSQL |

**单 Pod（最快上手）：**

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
# 1. 把 deploy/kubernetes/all-in-one/ingress.yaml 里的 skins.example.com 换成你的域名
kubectl apply -k deploy/kubernetes/all-in-one
kubectl -n skinloom rollout status deploy/skinloom
kubectl -n skinloom logs deploy/skinloom | grep setup              # 一次性安装链接
kubectl -n skinloom exec deploy/skinloom -- /app/skinloom setup-link   # 或重新生成一个
```

还没有域名时，用端口转发先访问：`kubectl -n skinloom port-forward svc/skinloom 17546:17546`，然后打开 <http://localhost:17546>。

**前后端分离 + PostgreSQL：**

```bash
kubectl create namespace skinloom
kubectl -n skinloom create secret generic skinloom-db --from-literal=password="$(openssl rand -hex 24)"
# 同样先改 deploy/kubernetes/split/ingress.yaml 里的域名
kubectl apply -k deploy/kubernetes/split
kubectl -n skinloom logs deploy/skinloom-server | grep setup
kubectl -n skinloom scale deploy/skinloom-web --replicas=3        # 扩容前端
```

说明：

- **HTTPS**：集群里装了 cert-manager 时，取消 `ingress.yaml` 里 `tls` 与注解的注释即可自动签发证书。
- **可信代理**：清单把 k3s 的 Pod 网段 `10.42.0.0/16` 设为可信代理（`SKINLOOM_SERVER__TRUSTED_PROXIES`），这样才能拿到真实客户端 IP 与 https 协议；其它集群请改成自己的 Pod 网段。
- **固定版本**：改 `kustomization.yaml` 里 `images` 的 `newTag`。
- **已有数据库**：在 `split/kustomization.yaml` 里去掉 `postgres.yaml`，把 `server.yaml` 里的 `SKINLOOM_DATABASE__DSN` 指向它。
- 后端目前是单副本（配置与上传文件在 ReadWriteOnce 卷上）；共享存储与 Helm Chart 在 M4 提供。

### 方式四：发行包（二进制）

从 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) 下载对应平台的压缩包并解压。包里有 `skinloom` 程序、前端、便携 Node.js 运行时，**不需要另外安装任何东西**。

**前台运行**（试用，关掉窗口即停止）：Windows 双击 `start.bat`，macOS 双击 `start.command`，Linux 运行 `./start.sh`，然后打开 <http://localhost:17546>。

**后台常驻**（开机自启、崩溃自动重启），包里的 `service/` 目录有对应平台的配置：

**Linux（systemd）**，以解压到 `/opt/skinloom` 为例：

```bash
sudo mkdir -p /opt/skinloom && sudo tar xzf skinloom-*-linux-amd64.tar.gz -C /opt/skinloom --strip-components=1
sudo useradd --system --home /opt/skinloom --shell /usr/sbin/nologin skinloom
sudo chown -R skinloom:skinloom /opt/skinloom
sudo cp /opt/skinloom/service/skinloom.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now skinloom
journalctl -u skinloom | grep setup        # 一次性安装链接
```

单元文件开启了 systemd 的沙箱选项（只读系统、只能写 `data/` 与前端缓存目录）。解压到别处时，把文件里的 `/opt/skinloom` 全部替换掉。

**macOS（launchd）**：把 `service/cn.skinloom.plist` 里的 `/Users/you/skinloom` 换成你的解压目录，然后：

```bash
cp service/cn.skinloom.plist ~/Library/LaunchAgents/
launchctl load -w ~/Library/LaunchAgents/cn.skinloom.plist
grep setup data/skinloom.log
```

**Windows（任务计划程序）**：在**管理员** PowerShell 里进入解压目录：

```powershell
powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1
Select-String setup .\data\skinloom.log
```

它注册一个开机启动、以 LOCAL SERVICE 身份运行、失败自动重启的计划任务（只给 `data\` 与前端缓存目录写权限）。卸载：同一命令加 `-Uninstall`。

**升级**：停止服务 → 用新版本的文件覆盖（保留 `data/`）→ 启动。

### 放在 Nginx / Caddy 后面（HTTPS）

把整站反代到 17546，并让 SkinLoom 信任这个代理（否则它看到的客户端 IP 是代理的、协议是 http）：

```toml
# data/config.toml（或环境变量 SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,::1）
[server]
trusted_proxies = ["127.0.0.1", "::1"]
```

Caddy（自动申请证书）：

```caddyfile
skins.example.com {
    reverse_proxy 127.0.0.1:17546
}
```

Nginx（宝塔、1Panel 的反向代理同理）：

```nginx
location / {
    proxy_pass http://127.0.0.1:17546;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $host;
    proxy_http_version 1.1;
    proxy_buffering off;        # /events 是长连接推送（设置修改后页面自动刷新）
    proxy_read_timeout 1h;
    client_max_body_size 10m;
}
```

最后在后台或命令行设置站点地址：`skinloom settings set site.url '"https://skins.example.com"'`。

### 端口

| 端口 | 用途 |
|---|---|
| **17546** | 唯一需要对外开放的端口：API、游戏协议、页面全部从这里进 |
| 17547 | 前端进程的内部端口（发行包里只监听 127.0.0.1；被占用时自动换一个空闲端口） |

改端口：`data/config.toml` 的 `[server] listen = ":8000"`，或环境变量 `SKINLOOM_SERVER__LISTEN=:8000`；Docker 下只改宿主机端口即可（`-p 8000:17546`，compose 用 `SKINLOOM_PORT=8000`）。

## 从源码运行（开发）

需要 Go 1.26+、Node.js 24 与 [bun](https://bun.sh)（没有 bun 也可以用 pnpm 或 npm）。

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
cd web && bun install && cd ..   # 或 pnpm install / npm install
go run ./cmd/skinloom up --dev
```

`up --dev` 会启动后端并在 `./web` 里运行前端开发服务器（依次尝试 bun、pnpm、npm），统一从 <http://localhost:17546> 访问。更多命令见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 发布

推送 `v*` 标签后，[Release 工作流](.github/workflows/release.yml) 自动：

- 构建六个平台的发行包（Windows / macOS / Linux × amd64 / arm64，内置 Node.js）与仅后端的单文件程序，附 `checksums.txt`，发布到 [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases)（带 `-` 的标签如 `v0.1.0-alpha.1` 标记为预发布）；
- 构建并推送三个多架构镜像到 `ghcr.io/entropy-increase-team/{skinloom,server,web}`。

main 分支的每次提交还会更新 `edge` 镜像。

## 文档

- 文档站：<https://skinloom.cn>（源码在 [`website/`](website/)，含开发日志，由 GitHub Pages 发布）
- [设计文档](docs/design/README.md)：Blessing Skin 拆解、兼容契约、缺陷清单、重构方案、插件与主题规范、文档体系
- [架构决策记录](docs/adr/README.md)

## 鸣谢

- **[Blessing Skin Server](https://github.com/bs-community/blessing-skin-server)**：SkinLoom 的设计参考与致敬对象。感谢 Blessing Skin 团队与社区多年的工作；部分翻译文本与渲染算法思路源自 Blessing Skin（MIT），版权声明保留在 [NOTICE](NOTICE)。
- [skinview3d](https://github.com/bs-community/skinview3d)、[authlib-injector](https://github.com/yushijinhun/authlib-injector)、[CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader)，以及 Go 与 Node.js 生态中的众多开源项目。

## 许可

[MIT](LICENSE)。SkinLoom 与 Mojang Studios、Microsoft 无关。
