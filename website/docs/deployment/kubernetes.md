---
title: Kubernetes / k3s
sidebar_position: 4
---

# Kubernetes / k3s

有两种方式，在 k3s 上都开箱即用（自带的 Traefik 作为 Ingress，local-path 提供存储）：

- **Helm Chart**（[`deploy/helm/skinloom`](https://github.com/Entropy-Increase-Team/skinloom/tree/main/deploy/helm/skinloom)）：参数化，推荐；
- **Kustomize 清单**（[`deploy/kubernetes/`](https://github.com/Entropy-Increase-Team/skinloom/tree/main/deploy/kubernetes)）：`kubectl apply -k` 直接可用，不需要另装工具。

| 目录 | 内容 |
|---|---|
| `all-in-one` | 1 个 Pod 跑前后端（`skinloom up`），SQLite，5 Gi 数据卷 |
| `split` | 后端 1 副本 + 前端 2 副本（带 PodDisruptionBudget）+ 单实例 PostgreSQL（10 Gi） |

两套清单都：以非 root 运行、去掉全部 Linux capabilities、开启 seccomp；用 `/readyz`、`/healthz` 做启动、就绪与存活探针；Ingress 把整站交给后端。

## 安装 k3s（如果还没有集群）

```bash
curl -sfL https://get.k3s.io | sh -
sudo k3s kubectl get nodes        # k3s 自带 kubectl；也可以把 /etc/rancher/k3s/k3s.yaml 复制为 ~/.kube/config
```

国内网络可以使用 k3s 的国内镜像安装脚本，参见 k3s 官方文档。

## Helm

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
helm install skinloom deploy/helm/skinloom -n skinloom --create-namespace   --set ingress.enabled=true --set 'ingress.hosts[0]=skins.example.com'   --set 'trustedProxies[0]=10.42.0.0/16'
kubectl -n skinloom logs deploy/skinloom | grep setup   # 一次性安装链接
```

默认是单 Pod（`mode=allInOne`）加 SQLite 和 5 Gi 数据卷。还没有发布版本时加 `--set image.tag=edge` 使用主分支的镜像。常用参数（完整列表见 `values.yaml`）：

| 参数 | 说明 |
|---|---|
| `mode` | `allInOne`：一个 Pod 跑前后端；`split`：前后端分开，前端可以扩容（带 PDB，可选 HPA `web.autoscaling.enabled`） |
| `database.driver` / `database.existingSecret` | `postgres` 或 `mysql` 时，DSN 放在你创建的 Secret 里（键 `dsn`） |
| `persistence.*` | 数据卷大小、StorageClass、已有的 PVC；卸载 Chart 时数据卷会保留 |
| `ingress.*` | Ingress 类、域名、注解（如 cert-manager）、TLS |
| `trustedProxies` | Ingress 控制器的 Pod 网段，k3s 是 `10.42.0.0/16` |
| `server.extraEnv` | 其它配置，例如 S3 存储（`SKINLOOM_STORAGE__DRIVER=s3` 等，密钥用 `valueFrom.secretKeyRef`） |
| `server.replicas` | 后端副本数：大于 1 时需要 PostgreSQL/MySQL，并且材质放在 S3（`persistence.enabled=false`）或使用 ReadWriteMany 卷 |

前后端分离 + PostgreSQL 的例子：

```bash
kubectl create namespace skinloom
kubectl -n skinloom create secret generic skinloom-db   --from-literal=dsn='postgres://skinloom:密码@db.example.com:5432/skinloom?sslmode=require'
helm install skinloom deploy/helm/skinloom -n skinloom   --set mode=split --set database.driver=postgres --set database.existingSecret=skinloom-db
```

互相矛盾的参数（例如 SQLite 配多个后端副本）会在安装时直接报错并说明原因。升级：`helm upgrade skinloom deploy/helm/skinloom -n skinloom --reuse-values --set image.tag=<新版本>`。

`helm uninstall` 会保留数据卷（PVC `<release>-skinloom-data`，也就是默认的 `skinloom-data`）。之后想用原来的数据重新安装，加 `--set persistence.existingClaim=skinloom-data`；不要数据了就手动 `kubectl -n skinloom delete pvc skinloom-data`。

## Kustomize：单 Pod

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
# 把 deploy/kubernetes/all-in-one/ingress.yaml 里的 skins.example.com 换成你的域名
kubectl apply -k deploy/kubernetes/all-in-one
kubectl -n skinloom rollout status deploy/skinloom
kubectl -n skinloom logs deploy/skinloom | grep setup                 # 一次性安装链接
kubectl -n skinloom exec deploy/skinloom -- /app/skinloom setup-link  # 或重新生成一个
```

还没有解析域名时，先用端口转发访问：

```bash
kubectl -n skinloom port-forward svc/skinloom 17546:17546
```

然后打开 <http://localhost:17546>（安装链接里的地址换成这个）。

## Kustomize：前后端分离 + PostgreSQL

```bash
kubectl create namespace skinloom
kubectl -n skinloom create secret generic skinloom-db --from-literal=password="$(openssl rand -hex 24)"
# 改 deploy/kubernetes/split/ingress.yaml 里的域名
kubectl apply -k deploy/kubernetes/split
kubectl -n skinloom get pods -w
kubectl -n skinloom logs deploy/skinloom-server | grep setup
kubectl -n skinloom scale deploy/skinloom-web --replicas=3   # 扩容前端
```

数据库密码只存在 Secret 里：PostgreSQL 直接读取，后端的 DSN 通过 Kubernetes 的 `$(DB_PASSWORD)` 变量展开拼出来。请使用只含字母数字的密码（如上面的 `openssl rand -hex`），避免需要在 URL 里转义。

已有数据库：在 `split/kustomization.yaml` 的 `resources` 里去掉 `postgres.yaml`，把 `server.yaml` 里的 `SKINLOOM_DATABASE__DSN` 指向你的数据库。

## 需要按集群调整的地方

- **可信代理**：清单把 `SKINLOOM_SERVER__TRUSTED_PROXIES` 设为 k3s 的 Pod 网段 `10.42.0.0/16`，这样 SkinLoom 才会采信 Ingress 控制器传来的客户端 IP 与 https 协议。其它发行版请改成自己的 Pod 网段（多个用逗号分隔）。
- **Ingress 类**：非 k3s 集群在 `ingress.yaml` 里设置 `ingressClassName`（例如 `nginx`）。
- **HTTPS**：装了 cert-manager 时，取消 `ingress.yaml` 里 `tls` 与 `cert-manager.io/cluster-issuer` 注解的注释。
- **存储**：PVC 使用集群的默认 StorageClass；需要时在 `pvc.yaml` / `server.yaml` 里指定 `storageClassName`。
- **版本**：改 `kustomization.yaml` 里 `images` 的 `newTag`（`split` 的 server 与 web 保持同一版本）。
- **站点地址**：完成安装后在后台设置 `site.url`（例如 `https://skins.example.com`）。

## 现状与限制

使用数据卷时后端是单副本：配置、密钥与上传文件在 ReadWriteOnce 卷上，部署策略是 `Recreate`（升级时会有几秒中断）。用 PostgreSQL/MySQL 并把材质放进 S3 后，可以用 Helm 的 `server.replicas` 运行多个后端副本；登录等限流按副本分别计数，每个副本的管理员令牌（`skinloom admin token`）也各不相同。前端可以随意扩容。
