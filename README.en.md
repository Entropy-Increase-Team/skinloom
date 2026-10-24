<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">SkinLoom · 云织</h1>

<p align="center">Open-source, self-hosted Minecraft skin site · Go backend + Next.js frontend · MIT</p>

<p align="center"><a href="https://skinloom.cn">Website &amp; docs</a> · <a href="README.md">中文</a> · <a href="https://github.com/Entropy-Increase-Team/skinloom/releases">Download</a> · <a href="docs/design/README.md">Design docs</a> · <a href="CHANGELOG.md">Changelog</a></p>

---

> **Status: in development (M1 accounts done; M2 textures and profiles in progress).** Not ready for production.

SkinLoom is a ground-up redesign of [Blessing Skin](https://github.com/bs-community/blessing-skin-server): upload and share skins and capes, manage game profiles, and serve skins and authentication to CustomSkinLoader, authlib-injector and other clients. It keeps Blessing Skin's external protocols (existing sites can migrate, game clients need no changes) while rebuilding the data model, runtime and plugin system.

## Deploy

| Method | For | Database | Notes |
|---|---|---|---|
| [Docker Compose](#docker-compose-recommended) **recommended** | most sites, NAS | SQLite / MariaDB / PostgreSQL | three ready-made compose files |
| [docker run](#docker-run) | existing Docker setups | any | one container, or frontend and backend apart |
| [Kubernetes / k3s](#kubernetes--k3s) | clusters, horizontal scaling | SQLite / PostgreSQL | Kustomize manifests, work as-is on k3s |
| [Release bundle](#release-bundle-binary) | Windows / macOS desktops, Linux without Docker | any | unzip and run; systemd / launchd / Task Scheduler files included |
| [From source](#from-source-development) | developers | SQLite | `go run ./cmd/skinloom up --dev` |

Whichever you pick:

- **Port** 17546 by default (1 + "SKIN" on a phone keypad). Five digits because most four-digit ports already belong to common software; it also stays clear of the ephemeral port ranges (Linux 32768+, Windows 49152+) and the Kubernetes NodePort range (30000–32767), and IANA has nothing registered on it. See [Ports](#ports).
- **First start** prints a one-time setup link `http://<host>:17546/setup?token=…` in the log. Open it to name the site and create the administrator. It is valid for 24 hours and works once; `skinloom setup-link` prints a new one. Only someone who can read the log can claim the site.
- **Data** (database, config, secrets, uploads) lives in `/data` (containers) or `data/` (bundles). **Back up that directory** (and an external database, if you use one).
- **Upgrade** by replacing the image or program and restarting; database migrations run on start (SQLite is snapshotted first).

> Until the first release is published there are no images in the registry yet: build from source as shown below, or use the `edge` tag (latest main build).

### Docker Compose (recommended)

Smallest setup, one container with SQLite:

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # one-time setup link
```

Frontend and backend apart, with MariaDB (scale the frontend as needed):

```bash
mkdir skinloom && cd skinloom
base=https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/standard
curl -fsSLO $base/compose.yaml && curl -fsSL $base/.env.example -o .env
sed -i "s/^DB_PASSWORD=.*/DB_PASSWORD=$(openssl rand -hex 24)/" .env
docker compose up -d
docker compose logs server | grep setup
docker compose up -d --scale web=2
```

For PostgreSQL use `postgres` instead of `standard`.

```bash
docker compose pull && docker compose up -d        # upgrade
SKINLOOM_VERSION=0.1.0 docker compose up -d        # pin a version (default latest; edge = main)
SKINLOOM_PORT=8000 docker compose up -d            # another host port
```

Build the images yourself (before the first release, or with your own changes):

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
docker compose -f deploy/compose/minimal/compose.yaml up -d --build
```

### docker run

```bash
docker run -d --name skinloom --restart unless-stopped \
  -p 17546:17546 -v skinloom-data:/data \
  ghcr.io/entropy-increase-team/skinloom:latest
docker logs skinloom 2>&1 | grep setup
```

External database: add `-e SKINLOOM_DATABASE__DRIVER=mysql -e SKINLOOM_DATABASE__DSN='user:pass@tcp(host:3306)/skinloom'` (or `postgres` with `postgres://user:pass@host:5432/skinloom?sslmode=disable`).

Frontend and backend apart (the server is the single entry point and proxies pages to the frontend):

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

Images: `skinloom` (all-in-one), `server` (backend only, distroless, non-root), `web` (frontend only); amd64 and arm64, with health checks. Tags: `x.y.z` / `x.y` releases, `latest` newest release, `edge` main branch.

### Kubernetes / k3s

Two Kustomize variants, ready for k3s (Traefik ingress, local-path storage): [`all-in-one`](deploy/kubernetes/all-in-one) (one pod, SQLite) and [`split`](deploy/kubernetes/split) (backend, 2× frontend with a PodDisruptionBudget, PostgreSQL).

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
# set your host name in deploy/kubernetes/all-in-one/ingress.yaml
kubectl apply -k deploy/kubernetes/all-in-one
kubectl -n skinloom logs deploy/skinloom | grep setup
kubectl -n skinloom port-forward svc/skinloom 17546:17546      # no domain yet
```

Split variant:

```bash
kubectl create namespace skinloom
kubectl -n skinloom create secret generic skinloom-db --from-literal=password="$(openssl rand -hex 24)"
kubectl apply -k deploy/kubernetes/split
kubectl -n skinloom scale deploy/skinloom-web --replicas=3
```

HTTPS: with cert-manager installed, uncomment `tls` and the annotation in `ingress.yaml`. The manifests trust the k3s pod network `10.42.0.0/16` as proxies (`SKINLOOM_SERVER__TRUSTED_PROXIES`) so client IPs and https are seen correctly; use your cluster's pod CIDR elsewhere. Pin versions with `images.newTag` in `kustomization.yaml`. The backend runs one replica for now; shared storage and a Helm chart come in M4.

### Release bundle (binary)

Download your platform's archive from [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) and unpack it; it includes the program, the frontend and a portable Node.js runtime.

Foreground (try it out): run `start.bat` (Windows), `start.command` (macOS) or `./start.sh` (Linux), then open <http://localhost:17546>.

In the background (start at boot, restart on crash) with the files in `service/`:

Linux (systemd), unpacked to `/opt/skinloom`:

```bash
sudo mkdir -p /opt/skinloom && sudo tar xzf skinloom-*-linux-amd64.tar.gz -C /opt/skinloom --strip-components=1
sudo useradd --system --home /opt/skinloom --shell /usr/sbin/nologin skinloom
sudo chown -R skinloom:skinloom /opt/skinloom
sudo cp /opt/skinloom/service/skinloom.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now skinloom
journalctl -u skinloom | grep setup
```

macOS (launchd): replace `/Users/you/skinloom` in `service/cn.skinloom.plist` with your folder, then `cp service/cn.skinloom.plist ~/Library/LaunchAgents/ && launchctl load -w ~/Library/LaunchAgents/cn.skinloom.plist`.

Windows (Task Scheduler), in an administrator PowerShell inside the folder:

```powershell
powershell -ExecutionPolicy Bypass -File .\service\install-task.ps1
Select-String setup .\data\skinloom.log
```

Upgrade: stop, overwrite the files with the new version (keep `data/`), start.

### Behind Nginx / Caddy (HTTPS)

Proxy the whole site to port 17546 and trust the proxy (`[server] trusted_proxies = ["127.0.0.1", "::1"]` in `data/config.toml`, or `SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,::1`). Caddy: `skins.example.com { reverse_proxy 127.0.0.1:17546 }`. Nginx: pass `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`, and set `proxy_buffering off` (live page refresh uses server-sent events at `/events`). Then set the public URL: `skinloom settings set site.url '"https://skins.example.com"'`.

### Ports

| Port | Use |
|---|---|
| **17546** | the only public port: API, game protocols and pages |
| 17547 | internal frontend port (loopback only in bundles; a free one is picked if taken) |

Change with `[server] listen = ":8000"` or `SKINLOOM_SERVER__LISTEN=:8000`; with Docker just map another host port (`-p 8000:17546`, or `SKINLOOM_PORT=8000` for compose).

## From source (development)

Requires Go 1.26+, Node.js 24 and [bun](https://bun.sh) (pnpm or npm work too).

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
cd web && bun install && cd ..   # or: pnpm install / npm install
go run ./cmd/skinloom up --dev
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Releases

Pushing a `v*` tag runs the [release workflow](.github/workflows/release.yml): bundles for Windows / macOS / Linux × amd64 / arm64 (Node.js included), bare backend binaries and `checksums.txt` on [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases) (tags with a `-`, e.g. `v0.1.0-alpha.1`, are marked pre-release), plus multi-arch images on `ghcr.io/entropy-increase-team/{skinloom,server,web}`. Every push to main refreshes the `edge` images.

## Acknowledgements

- **[Blessing Skin Server](https://github.com/bs-community/blessing-skin-server)** — the design reference SkinLoom pays tribute to. Thanks to the Blessing Skin team and community; some translations and rendering ideas come from Blessing Skin (MIT), see [NOTICE](NOTICE).
- [skinview3d](https://github.com/bs-community/skinview3d), [authlib-injector](https://github.com/yushijinhun/authlib-injector), [CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader), and many open-source projects in the Go and Node.js ecosystems.

## License

[MIT](LICENSE). SkinLoom is not affiliated with Mojang Studios or Microsoft.
