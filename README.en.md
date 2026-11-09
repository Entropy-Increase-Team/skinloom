<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">SkinLoom · 云织</h1>

<p align="center">Open-source, self-hosted Minecraft skin site</p>

<p align="center"><a href="https://docs.skinloom.cn">Docs (Chinese)</a> · <a href="https://github.com/Entropy-Increase-Team/skinloom/releases">Download</a> · <a href="CHANGELOG.md">Changelog</a> · <a href="README.md">中文</a></p>

---

Players upload and share skins and capes on SkinLoom and dress their game characters in them; launchers and the game show those skins through authlib-injector (external login) or mods such as CustomSkinLoader.

SkinLoom is a reimplementation of [Blessing Skin](https://github.com/bs-community/blessing-skin-server) that keeps its external protocols: existing sites migrate with one command, and players and servers keep their configuration.

> 1.0 is being finished; current releases are 0.x previews.

## Features

- **Players**: skin library, uploads and favorites, closet, several characters, 3D preview, daily check-in score, reports, notifications, sign-in with GitHub / Microsoft / OpenID Connect
- **Game**: authlib-injector (Yggdrasil), CustomSkinLoader, UniSkinMod, avatars and previews
- **Site owners**: admin panel and audit log; settings, theme and home page change live, no restart; SQLite / MySQL / PostgreSQL; local or S3 storage; CDN purge; backup and restore
- **Developers**: OAuth 2.0, personal access tokens, Blessing Skin compatible resource API, REST API (OpenAPI 3.1)
- **Deployment**: Docker, Kubernetes / Helm, or Windows / macOS / Linux bundles (runtime included, can run as a system service)

## Quick start

**Docker:**

```bash
mkdir skinloom && cd skinloom
curl -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
docker compose up -d
docker compose logs skinloom | grep setup     # one-time setup link
```

**Release bundle:** download from [Releases](https://github.com/Entropy-Increase-Team/skinloom/releases), unpack, then run `start.bat` (Windows), `start.command` (macOS) or `./start.sh` (Linux).

Open the setup link from the log (`http://<host>:17546/setup?token=…`) to name the site and create the administrator. The only port to open is 17546; everything the site stores is in its data directory (`/data` in containers, `data/` in bundles), so back that up. Other deployment options are in the [docs](https://docs.skinloom.cn/docs/deployment/quick-start).

## Development

Go 1.26+, Node.js 24 and [bun](https://bun.sh) (or pnpm / npm); no Docker needed:

```bash
git clone https://github.com/Entropy-Increase-Team/skinloom && cd skinloom
cd web && bun install && cd ..
go run ./cmd/skinloom up --dev     # http://localhost:17546
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for commands and conventions, the [architecture](https://docs.skinloom.cn/docs/development/architecture) page for the layout, and [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## Acknowledgements

Thanks to the [Blessing Skin](https://github.com/bs-community/blessing-skin-server) team and community; some translations and rendering ideas come from Blessing Skin (MIT), see [NOTICE](NOTICE). Thanks also to [skinview3d](https://github.com/bs-community/skinview3d), [authlib-injector](https://github.com/yushijinhun/authlib-injector) and [CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader).

## License

[MIT](LICENSE). SkinLoom is not affiliated with Mojang Studios or Microsoft.
