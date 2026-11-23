# @skinloom/ui

[![npm version](https://img.shields.io/npm/v/@skinloom/ui?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/@skinloom/ui)
[![License: MIT](https://img.shields.io/github/license/Entropy-Increase-Team/skinloom?style=flat-square)](https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE)

Theme-token driven React UI component library and TypeScript definitions shared by the [SkinLoom](https://github.com/Entropy-Increase-Team/skinloom) host application and runtime-loaded web plugins.

## Install

```bash
npm install -D @skinloom/ui
```

When building SkinLoom web plugins with `@skinloom/cli`, `@skinloom/ui` is automatically treated as a host-provided external module so plugins share the active SkinLoom theme tokens at runtime with zero duplicate bundle weight.

## Exports

- `@skinloom/ui` — Core shadcn/Base UI & SkinLoom components (`Button`, `Card`, `Dialog`, `Table`, `PageHeader`, `CopyField`, `SkinViewer`, `useConfirm`, `toast`, etc.)
- `@skinloom/ui/components/ui/chart` — Recharts chart helpers
- `@skinloom/ui/globals.css` — SkinLoom design token CSS stylesheet

## Documentation

See the [SkinLoom Plugin Developer Guide](https://docs.skinloom.cn/docs/plugins/overview).

## License

MIT
