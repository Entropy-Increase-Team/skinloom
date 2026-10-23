---
title: Kubernetes / k3s
sidebar_position: 4
---

# Kubernetes / k3s

仓库的 [`deploy/kubernetes/`](https://github.com/Entropy-Increase-Team/skinloom/tree/main/deploy/kubernetes) 提供两套 Kustomize 清单（`kubectl apply -k` 直接可用，不需要另装工具）。在 k3s 上开箱即用：自带的 Traefik 作为 Ingress，local-path 提供存储。Helm Chart 在 M4 提供。

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

## 单 Pod

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

## 前后端分离 + PostgreSQL

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

后端目前是单副本：配置、密钥与上传文件在 ReadWriteOnce 卷上，部署策略是 `Recreate`（升级时会有几秒中断）。多副本后端需要共享存储（S3 / RWX 卷）与跨副本限流，计划在 M2（存储）与 M4（Helm、HPA）实现。前端可以随意扩容。
