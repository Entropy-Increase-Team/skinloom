# @skinloom/cli

[![npm version](https://img.shields.io/npm/v/@skinloom/cli?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/@skinloom/cli)
[![License: MIT](https://img.shields.io/github/license/Entropy-Increase-Team/skinloom?style=flat-square)](https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE)

CLI toolkit (`skinloom-plugin`) for linting, building, dev-reloading, packing, and signing [SkinLoom](https://github.com/Entropy-Increase-Team/skinloom) plugins and themes.

## Install

```bash
npm install -D @skinloom/cli
```

## Usage

```bash
npx skinloom-plugin lint
npx skinloom-plugin build
npx skinloom-plugin dev --host http://localhost:17546 --token <admin-token>
npx skinloom-plugin pack --out my-plugin.slp
```

For Tier 2 Native Go plugins, pass `--native` (and optionally `--split-platforms` or `--fat`).

## Documentation

See the [SkinLoom Plugin Developer Guide](https://docs.skinloom.cn/docs/plugins/overview).

## License

MIT
