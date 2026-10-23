# 0005 插件标准：TypeScript + JS 沙箱

- 状态：已接受（2026-10-23）
- 相关：设计文档 05

## 背景
要求插件体系有统一标准、易学，开放足够能力（含前端界面与主题），不必兼容多种语言。Go 没有跨平台可用的动态加载（`plugin` 包不支持 Windows）。

## 决策
- 一份 `plugin.json` + TypeScript + `@skinloom/plugin-sdk` + `skinloom-plugin` CLI；插件与主题同一标准（`kind: plugin | theme`）。
- 后端代码运行在内嵌 JS 沙箱：QuickJS-NG 编译为 WASM，跑在 wazero 上（参考实现 fastschema/qjs）——ES2023、内存上限、执行超时、纯 Go。引擎藏在 `internal/jsrt` 接口后，插件只依赖 SDK。
- 能力通过 `ctx` 提供并按清单权限授予。
- 官方功能以内置 Go 模块实现，使用同一 Hook 总线。

## 备选方案
- goja：成熟，但语法只到 ES5.1 + 部分 ES6，且无内存上限。
- Extism 多语言 WASM：生态碎片化、文档成本翻倍；保留为远期"原生模块"选项。

## 影响
- fastschema/qjs 较年轻；必要时自维护 QuickJS-NG 的 WASM 构建。
