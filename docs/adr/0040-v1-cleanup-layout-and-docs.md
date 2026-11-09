# 0040 1.0 收尾：目录分层、统一的事务工具与按读者组织的文档

- 状态：已接受（2026-11-09）
- 相关：ADR 0002（模块化单体）、0026–0028（事务规则）、设计文档 04 §3.3

## 背景

1.0 之前做一次整理，目标是让新贡献者能快速看懂代码，让玩家和站长能快速找到要看的文档。整理前的问题：

- `internal/` 下 33 个包平铺，业务模块、基础设施和进程级的包混在一起，看目录看不出分层。
- 事务辅助函数 `withTx` 在 9 个包里各有一份，写法不完全一样（有的总用 `errors.Join` 包一层，有的原样返回），而且**都没有在回调 panic 时回滚**：panic 被 HTTP 服务器恢复后，事务一直占着连接和锁。SQLite 只有一个写者，此后整个站点的写入都会等待并失败。
- 按字节截断字符串的函数有三份，`sameJSON`、`cmpOr` 各有重复。
- 几个大文件职责混杂：`texture/manage.go`（894 行：衣柜、编辑删除、加锁、文件清理）、`account/service.go`（700 行：安装、登录、注册、会话）、`cli/root.go`（所有命令）。
- README 有 276 行，大段重复文档站的部署说明；文档站"开始使用"一栏把玩家操作、站长设置、实现细节和 Blessing Skin 缺陷编号写在同一页，玩家要翻过设置键才能找到"怎么上传"。

## 决策

### 目录分层

```text
internal/
  app cli config gateway launcher daemon backup   进程级：装配、命令行、入口、运维
  httpapi                                         HTTP 层
  hook                                            扩展总线（将来的插件接入点）
  modules/                                        业务模块
    account audit captcha cdn mail notify oauth player render report
    score settings sitesettings social stats texture theme yggdrasil
  platform/                                       与业务无关的基础设施
    db dbtest migrate datalock fsperm httpx realip validate
    pubsub jobs storage logging secrets buildinfo registry clip
  importer/bs contract tools/
```

依赖方向是 `cli → app → httpapi → modules → platform`，`platform` 不导入任何业务包。`captcha` 和 `mail` 声明了自己的站点设置，所以归入 `modules`。命名沿用设计文档 04 §3.3 的 `modules` 与 `platform`。

迁移只改导入路径，不改包名和代码。`buildinfo` 移到 `internal/platform/buildinfo`，Dockerfile 和打包脚本里的 `-ldflags -X` 随之更新。

### 统一的事务工具

所有事务都用 `db.WithTx(ctx, client, fn)`：

- `fn` 返回 nil 时提交；返回错误时回滚，并**原样**返回这个错误（调用方用 `==` 比较哨兵错误），只有回滚本身也失败时才用 `errors.Join` 合并；
- `fn` panic 时先回滚再继续 panic。

`internal/platform/db/tx_test.go` 覆盖这三种情况，并在三种数据库上验证 panic 之后还能写入（旧写法在 SQLite 上超时）。统一过程中，测试还抓到了把 `errors.Join` 写法推广到所有包会破坏 `err != ErrNotFound` 这类比较的问题，最终采用上面的语义。

### 其它清理

- `platform/clip` 提供 `Bytes`（按字节、不切断字符）和 `Runes`（按字符），替换四份截断函数；`sameJSON` 导出为 `settings.SameJSON`；`cmpOr` 换成标准库 `cmp.Or`。
- 拆分大文件，只移动声明、不改逻辑（用工具按声明搬移，并逐行比对搬移前后的代码）：`texture` 拆成 `closet.go`、`edit.go`、`locks.go`、`upkeep.go`；`account` 拆出 `setup.go`、`login.go`、`register.go`、`session.go`；`cli` 拆成 `serve.go`、`maintain.go`、`site.go`、`info.go`。
- `httpapi` 的管理接口文件统一加 `admin_` 前缀，新增 `doc.go` 列出每个文件负责的接口。
- `app.OpenCore` 改为先建 `Core`、出错时统一调用 `Close`：原来每个出错分支各自关闭数据库，并且出错时 hook 总线的 goroutine 不会退出。新增 `Core.APIDeps`，替换 `run.go` 里那行很长的依赖列表。

### 文档按读者组织

- 文档站分成**玩家指南**（`players/`：账号、皮肤与披风、角色、在游戏里使用）、**站长指南**（`deployment/` 与 `admin/`：快速开始、部署、配置文件、站点设置、注册登录与邮件、后台管理、外观、第三方登录、外置登录与游戏接口、备份、迁移）和**开发**（`development/`：环境、架构、接口，以及插件与主题）。
- 玩家文档只讲怎么做，不出现设置键和实现细节；站长文档给出设置键和命令；Blessing Skin 缺陷编号（C03、S05 等）和实现说明不再出现在使用文档里，留在设计文档和 ADR 中。
- 站点设置速查表按 `skinloom settings get` 的输出逐项核对，覆盖全部 80 个设置。
- README 只保留介绍、功能、快速开始和按读者分的入口；部署细节交给文档站，发布流程移到 CONTRIBUTING。

## 备选方案

- **把 `httpapi` 按领域拆成子包**：处理函数共用的请求上下文、错误映射和权限检查都得导出，换来的只是目录层级。改为文件前缀加 `doc.go` 索引。
- **业务目录叫 `internal/domain`**：设计文档一直用 `modules`，沿用以减少概念。
- **旧文档地址加重定向**：需要新增插件依赖。部署类页面保留了 `deployment/` 路径（发行包 README 链接的地址不变），旧的 `getting-started/*` 只在开发日志里被引用过，已改为新地址。

## 影响

- 导入路径变化：自行用 `-ldflags -X …/internal/buildinfo.Version` 构建的脚本要改为 `…/internal/platform/buildinfo.Version`。
- 文档站 `/docs/getting-started/*` 地址不再存在。
- 已有的 ADR 与开发日志保留当时的路径，作为历史记录不改写。
