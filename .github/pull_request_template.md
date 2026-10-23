## 做了什么 / What

## 为什么 / Why

## 如何验证 / How was it tested

## 清单 / Checklist

- [ ] 提交信息符合 Conventional Commits，并已 `git commit -s`（DCO）
- [ ] 改了 `ent/schema` → 已生成并提交迁移（三种方言）
- [ ] 改了 API → 已 `go generate ./api && cd web && pnpm gen:api`
- [ ] 影响用户 / 站长 / 插件开发者 → 已更新文档与 `CHANGELOG.md`（Unreleased）
- [ ] 改了 Hook、插槽、`plugin.json` 或其它公共 API → 已写 ADR / 弃用说明
