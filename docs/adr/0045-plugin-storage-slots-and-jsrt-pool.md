# 0045 插件存储预置槽位与 `jsrt` 沙箱实例池设计

- 状态：已接受（2026-11-18）
- 相关：设计文档 `05-plugin-theme-standard.md`、`09-plugin-platform-plan.md`；ADR 0003（ent/Atlas/goose 迁移）、0005（TS + JS 沙箱标准）、0007（插件存储不开放 SQL）

## 背景

ADR 0005 与 ADR 0007 确定了插件后端运行在纯 Go 内嵌 JS/WASM 沙箱中，且存储不开放裸 SQL。进入 P2-1 工程落地阶段，需要拍板两个直接决定多方言兼容性、运维安全性与高并发稳定性的关键工程细节：

1. **文档集合（`plugin_docs`）的动态索引方案**：插件需要在 `plugin.json` 中声明少量索引字段（含唯一约束），如何在不执行运行时 DDL 的前提下同时兼容 SQLite / MySQL / PostgreSQL 与多副本部署？
2. **JS/WASM 沙箱（`internal/jsrt`）的并发模型与抢占中断**：单个 QuickJS 实例是单线程非并发安全的；同时恶意或有缺陷插件可能触发死循环（`while(true){}`）或内存爆炸（OOM），宿主必须在严格时限内物理抢占并无损恢复服务。

## 决策

### 1. `plugin_docs` 预置物理槽位与零运行时 DDL

- `ent/schema/plugindoc.go` 在 `plugin_docs` 表迁移期即一次性建好固定物理槽位与复合索引，运行期**绝不执行** `CREATE INDEX` / `ALTER TABLE` 等 DDL：
  - **2 个唯一字符串槽位**：`u1`、`u2`（`MaxLen(255)`，与 `(plugin, collection)` 构成唯一索引，映射清单 `"string:unique"`）；
  - **3 个普通字符串索引槽位**：`s1..s3`（`MaxLen(255)`，映射 `"string"`）；
  - **3 个数值索引槽位**：`n1..n3`（`float64`，映射 `"number"`）；
  - **2 个时间索引槽位**：`t1..t2`（`time.Time`，映射 `"time"`）。
- `internal/plugin/storage_docs.go` 在加载集合清单时按**字段名字母序确定性绑定**到可用槽位，保证多副本 / 重启后同一字段永远对应同一物理列；索引超额或未声明即作为查询/排序条件时 fail-closed 返回 `ErrBadManifest` 或 `UnindexedFieldError`，唯一键冲突统一映射为 `ErrUniqueConstraint`。
- `plugin_kv` 采用方言可移植的 `schema.KVValue`（`[]byte` + 自定义 JSON 编解码），避免不同 Go 小版本下 `json.RawMessage` 别名差异引起 `ent` 生成代码漂移；所有 `field.Time` 显式声明 `SchemaType(datetimeType)` 保持三方言对齐。

### 2. `internal/jsrt` 沙箱池、抢占中断与损坏重建

- **纯 Go WASM 运行时与版本锁定**：基于 `github.com/fastschema/qjs v0.0.6` 与 `github.com/tetratelabs/wazero v1.9.0`（`CGO_ENABLED=0` 跨平台零 CGO）；因 `fastschema/qjs` 将预编译模块跨越 compiler/instance 两个 wazero `Store` 实例化，锁定 `wazero v1.9.0` 以规避高版本跨 Store `TypeIDs` 签名校验不匹配问题。
- **预编译字节码与预热实例池**：每插件加载时将 ES2023 `server.js` 编译一次 QuickJS 字节码，并维护默认 `PoolSize = 2` 个已执行完模块评估与 `setup(ctx)` 绑定重放的暖实例；请求经 `Pool.Borrow` / `Pool.Do` 独占借出后归还。
- **硬隔离与资源配额**：
  - WASM 虚拟根目录隔离为空临时目录；
  - 启动脚本剥离 `os`、`std`、`bjson`、`setInterval`、`clearInterval`、`scriptArgs`、`navigator`，仅保留受调用上下文生命周期回收约束的 `setTimeout` / `clearTimeout`、`console.*`（桥接至宿主日志）、`TextEncoder` / `TextDecoder`、`URL` / `URLSearchParams` 与基于 `crypto/rand` 的 `crypto.getRandomValues` / `randomUUID`；
  - 单实例限制 WASM 内存上限 `64 MiB`、栈上限 `1 MiB`、包体积上限 `5 MiB`；Guard/Filter 超时 `2 s`，插件 HTTP 路由超时 `30 s`，生命周期/事件超时 `10 s`，调用结束立即清理遗留定时器。
- **上下文抢占、OOM 识别与故障自动替换**：启用 `qjs.Option{CloseOnContextDone: true}` 并挂载超时监护关闭底层 runtime 上下文；无论 `while(true){}` 超时抢占、64 MiB OOM 还是 WASM trap，均将实例标记为 `tainted`、丢弃损坏实例并在后台异步补充新暖实例，防止故障传染或阻塞后续请求。同时绕开 `fastschema/qjs v0.0.6` 中 `QJS_JSONStringify` 在 C 侧提前 `JS_FreeCString` 的悬垂读取缺陷，统一改由 JS `JSON.stringify` 返回普通 `JSString` 读取，并在 `Promise.Await()` 后不重复释放已被 `js_std_await` 消费的句柄。
- **两阶段原子切换与熔断器**：插件启停/热更新先完成池预热与 Hook/路由收集，再通过 `hook.Bus.Replace("plugin:<id>", ...)` 与活跃路由表原子切换并优雅排水旧池；连续运行期异常（超时、OOM、未捕获 JS 异常）达 `DefaultCircuitThreshold = 5` 次即自动熔断，数据库中标记 `failed`、从总线摘除所有 Hook 与路由、排水旧池并向活跃超级管理员发送站内故障通知。

## 备选方案

- **运行时为每个插件集合动态执行 `CREATE INDEX` / 生成列 DDL**：在只读迁移数据库账号、多实例滚动升级与 SQLite/MySQL/PostgreSQL 语法差异下风险极高，且会触发与 Atlas/goose 迁移校验的 Schema 漂移。
- **全插件共享单个隔离实例 + 全局互斥锁**：单个插件执行 2 s Guard 阻塞或被超管配为 30 s HTTP 路由时直接卡死全站所有插件与热路径。
- **不区分损坏实例状态直接复用池内实例**：QuickJS-NG 在超时强制中断或 OOM trap 后堆状态已不可信，直接复用会导致内存损坏或静默异常，必须在检测到异常后标记 `tainted` 并剔除重建。

## 影响

- **容量限制清晰可控**：单个文档集合上限为 2 个唯一字符串、3 个普通字符串、3 个数值与 2 个时间索引字段；需要更大索引空间时应拆分为多个集合或借助 KV 建立反向索引。
- **内存与冷启动开销**：每个已启用插件默认常驻 2 个轻量化 QuickJS-NG WASM 暖实例（按需动态扩容内存上限至 64 MiB），且后续 P2-2~P2-6 阶段扩展 `ctx.*` 命名空间时直接复用本沙箱池与熔断基础设施。
