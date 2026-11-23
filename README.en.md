<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">SkinLoom · 云织</h1>

<p align="center">Open-source, self-hosted Minecraft skin site</p>

<p align="center">
  <a href="https://github.com/Entropy-Increase-Team/skinloom/releases"><img src="https://img.shields.io/github/v/release/Entropy-Increase-Team/skinloom?style=flat-square&logo=github" alt="Release"></a>
  <a href="https://github.com/Entropy-Increase-Team/skinloom/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Entropy-Increase-Team/skinloom/ci.yml?branch=main&style=flat-square&logo=githubactions&logoColor=white&label=CI" alt="CI"></a>
  <a href="https://github.com/Entropy-Increase-Team/skinloom/pkgs/container/skinloom"><img src="https://img.shields.io/badge/GHCR-skinloom-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker GHCR"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Entropy-Increase-Team/skinloom?style=flat-square" alt="License"></a>
  <a href="https://github.com/Entropy-Increase-Team/skinloom/stargazers"><img src="https://img.shields.io/github/stars/Entropy-Increase-Team/skinloom?style=flat-square&logo=github" alt="GitHub Stars"></a>
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/@skinloom/plugin-sdk"><img src="https://img.shields.io/npm/v/@skinloom/plugin-sdk?style=flat-square&logo=npm&label=%40skinloom%2Fplugin-sdk&color=CB3837" alt="@skinloom/plugin-sdk"></a>
  <a href="https://www.npmjs.com/package/@skinloom/cli"><img src="https://img.shields.io/npm/v/@skinloom/cli?style=flat-square&logo=npm&label=%40skinloom%2Fcli&color=CB3837" alt="@skinloom/cli"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/Entropy-Increase-Team/skinloom?style=flat-square&logo=go&logoColor=white" alt="Go Version">
  <img src="https://img.shields.io/badge/Next.js-16-000000?style=flat-square&logo=nextdotjs&logoColor=white" alt="Next.js 16">
  <img src="https://img.shields.io/badge/React-19-149ECA?style=flat-square&logo=react&logoColor=white" alt="React 19">
  <img src="https://img.shields.io/badge/TypeScript-5.9-3178C6?style=flat-square&logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/Tailwind_CSS-4-06B6D4?style=flat-square&logo=tailwindcss&logoColor=white" alt="Tailwind CSS 4">
</p>

<p align="center"><a href="https://docs.skinloom.cn">Docs (Chinese)</a> · <a href="https://github.com/Entropy-Increase-Team/skinloom/releases">Download</a> · <a href="CHANGELOG.md">Changelog</a> · <a href="README.md">中文</a></p>

---

Players upload and share skins and capes on SkinLoom and dress their game characters in them; launchers and the game show those skins through authlib-injector (external login) or mods such as CustomSkinLoader.

SkinLoom is a reimplementation of [Blessing Skin](https://github.com/bs-community/blessing-skin-server) that keeps its external protocols: existing sites migrate with one command, and players and servers keep their configuration.

## Features

- **Players**: skin library, uploads and favorites, closet, several characters, 3D preview, daily check-in score, reports, notifications, sign-in with GitHub / Microsoft / OpenID Connect
- **Game**: authlib-injector (Yggdrasil), CustomSkinLoader, UniSkinMod, avatars and previews
- **Site owners**: admin panel and audit log; settings, theme and home page change live, no restart; signed plugin & theme market with one-click install; SQLite / MySQL / PostgreSQL; local or S3 storage; CDN purge; backup and restore
- **Developers**: OAuth 2.0, personal access tokens, Blessing Skin compatible resource API, REST API (OpenAPI 3.1), and a dual-tier plugin & theme platform (QuickJS/WASM TypeScript sandbox + Native Go gRPC subprocesses, `@skinloom/plugin-sdk`, `sdk/go`, `create-skinloom-plugin`)
- **Deployment**: Docker, Kubernetes / Helm, or Windows / macOS / Linux bundles (runtime included, can run as a system service)

## Quick start

**Docker:**

```bash
mkdir skinloom && cd skinloom
curl -4 -fsSLO https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom/main/deploy/compose/minimal/compose.yaml
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
