# @skinloom/plugin-sdk

[![npm version](https://img.shields.io/npm/v/@skinloom/plugin-sdk?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/@skinloom/plugin-sdk)
[![License: MIT](https://img.shields.io/github/license/Entropy-Increase-Team/skinloom?style=flat-square)](https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE)

TypeScript SDK, React web runtime hooks, and Vitest test harness for [SkinLoom](https://github.com/Entropy-Increase-Team/skinloom) plugins and themes.

## Install

```bash
npm install -D @skinloom/plugin-sdk
```

## Subpaths

- `@skinloom/plugin-sdk/server` — QuickJS/WASM Tier 1 server plugin definition (`definePlugin`, `reject`, typed host APIs)
- `@skinloom/plugin-sdk/web` — React web runtime hooks (`defineWebPlugin`, `usePluginApi`, `useSession`, `useSettings`, `useSite`, `useTheme`, `useT`, `toast`, `confirm`, `navigate`)
- `@skinloom/plugin-sdk/testing` — In-memory Vitest host harness (`createTestHost`)
- `@skinloom/plugin-sdk/catalog` — Generated permission, hook, slot, and theme override catalogs

## Documentation

See the [SkinLoom Plugin Developer Guide](https://docs.skinloom.cn/docs/plugins/overview).

## License

MIT
