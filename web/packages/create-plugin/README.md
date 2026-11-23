# create-skinloom-plugin

[![npm version](https://img.shields.io/npm/v/create-skinloom-plugin?style=flat-square&logo=npm&color=CB3837)](https://www.npmjs.com/package/create-skinloom-plugin)
[![License: MIT](https://img.shields.io/github/license/Entropy-Increase-Team/skinloom?style=flat-square)](https://github.com/Entropy-Increase-Team/skinloom/blob/main/LICENSE)

Scaffolding CLI for [SkinLoom](https://github.com/Entropy-Increase-Team/skinloom) plugins and themes.

## Usage

```bash
npm create skinloom-plugin@latest my-plugin -- --template basic
# or with Bun
bunx create-skinloom-plugin my-plugin --template page
```

### Available templates

- `basic` — Minimal Tier 1 JS plugin with hooks and Vitest tests
- `page` — Plugin with custom user/admin React pages (`@skinloom/ui`) and i18n locales
- `auth-provider` — Custom OAuth/SSO provider plugin
- `block` — Homepage layout block plugin
- `theme` — Level 5 theme package with design tokens and component overrides
- `native-go` — Tier 2 Native Go plugin with gRPC host SDK and SQL migrations

## Documentation

See the [SkinLoom Plugin Developer Guide](https://docs.skinloom.cn/docs/plugins/overview).

## License

MIT
