<p align="center"><img src="web/apps/web/src/app/icon.svg" width="72" alt=""></p>

<h1 align="center">SkinLoom (云织)</h1>

<p align="center">An open-source, self-hosted Minecraft skin site · Go backend + Next.js frontend · MIT</p>

<p align="center"><a href="https://skinloom.cn">skinloom.cn</a> · <a href="README.md">简体中文</a> · <a href="docs/design/README.md">Design docs (zh)</a> · <a href="docs/adr/README.md">ADRs</a> · <a href="CHANGELOG.md">Changelog</a></p>

---

> **Status: in development (M0, foundations).** Not ready for production.

SkinLoom is a redesign and rewrite modelled on [Blessing Skin](https://github.com/bs-community/blessing-skin-server): upload and share skins and capes, manage in-game players, and serve skins and authentication to CustomSkinLoader, authlib-injector and other clients. It keeps Blessing Skin's external protocols compatible — existing sites can migrate without players changing their client config — while rebuilding the data model, runtime and plugin system from scratch.

## Planned features

- **One-click run**: release bundles for Windows, macOS and Linux start with a double click (Node.js runtime included); Docker, Docker Compose and Helm (k3s/k8s) too.
- **Hot reload**: settings, themes, the config file and plugin install/upgrade/uninstall apply without a restart; open pages refresh within seconds.
- **Blessing Skin compatible**: CSL / USM / legacy / Yggdrasil protocols and the OAuth API, plus a read-only importer that keeps player UUIDs, passwords and skin URLs.
- **One plugin standard**: TypeScript, one SDK, one CLI; backend code runs sandboxed, frontend code extends pages, slots and home-page blocks; themes follow the same standard.
- **Databases**: SQLite (zero dependencies), MySQL / MariaDB, PostgreSQL.

## Quick start

**Release bundle**: unzip, then run `start.bat` (Windows), `start.command` (macOS) or `./start.sh` (Linux) and open <http://localhost:8080>. Data lives in `data/` next to the scripts.

**From source** (Go 1.26+, Node.js 20.9+, pnpm 10):

```bash
cd web && pnpm install && cd ..
go run ./cmd/skinloom up --dev
```

**Docker**: `docker compose -f deploy/compose/minimal/compose.yaml up -d`

See [CONTRIBUTING.md](CONTRIBUTING.md) for development commands.

## Acknowledgements

- **[Blessing Skin Server](https://github.com/bs-community/blessing-skin-server)** — the design SkinLoom follows and pays tribute to. Some translations and rendering ideas come from Blessing Skin (MIT); its copyright notice is kept in [NOTICE](NOTICE).
- [skinview3d](https://github.com/bs-community/skinview3d), [authlib-injector](https://github.com/yushijinhun/authlib-injector), [CustomSkinLoader](https://github.com/xfl03/MCCustomSkinLoader) and the many open-source projects of the Go and Node.js ecosystems.

## License

[MIT](LICENSE). SkinLoom is not affiliated with Mojang Studios or Microsoft.
