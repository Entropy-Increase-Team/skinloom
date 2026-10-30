# 0028 领域事件经任务队列投递，审计记录与变更同事务

- 状态：已接受（2026-10-30）
- 相关：设计文档 04 §4、05 §4.4；03（A01、A02、A08）；ADR 0006、0015、0024、0027

## 背景
2026-10-30 的全量审查发现两处架构与设计文档不一致：
- **Hook 总线只有 `settings.changed` 一个事件。** M1–M3 的模块都没有声明领域 Hook，也没有 outbox。04 §1 把"插件平台的地基在 v1 就位"列为第一阶段目标，`Event.Emit` 的注释也写着"durable at-least-once delivery through the outbox table arrives with the job queue in M1"，实际没有做。等插件运行时（第二阶段）再往每个服务里补，成本会高得多，也难保证"同一事件两处参数一致"（BS C18）。
- **管理操作的审计写在事务外。** `/api/v1` 与兼容 BS 的管理接口共用的辅助函数（ADR 0024）先调用服务、再写审计，写审计失败只打一条日志。管理员同时改名和转移角色是两个事务，可能只成功一半。这和 03 A01"handler → service 是唯一业务入口"不一致。

## 决策
- **领域事件经任务队列投递（outbox）**：
  - `hook.Event[P].Publish(ctx, bus, tx, payload)` 在变更所在的事务里往 `jobs` 表写一条 `hook.event` 任务（载荷是事件名@版本、投递键与 JSON）。事件和变更同时提交或同时回滚。
  - 任务执行器 `Bus.Deliver` 按当时注册的处理器依次调用；任何一个失败，整条任务按队列的退避重试（1、5、15、60 分钟），所有处理器再收到一次。处理器用 `hook.DeliveryKey(ctx)` 识别重复投递，同一事件每次投递的键相同。第 5 次仍失败的事件标记为 failed，留在 `jobs` 表里供排查，不再投递。所以"至少一次"指的是重试期内。事件之间不保证顺序。
  - 发布时没有处理器就不写任务，所以现在没有插件时不增加任何写入。`Emit` 只用于进程内的通知（`settings.changed`，各节点自己重载）。
  - 事件最长约 5 秒后投递（队列的轮询间隔），没有专门唤醒执行器。
- **事件目录**（`internal/hook/catalog.go`，名称取自 05 §4.4，都是 `@1`）：
  - 用户：`user.registered`（`method`：password / setup / social:提供方）、`user.banned`、`user.deleted`。
  - 角色：`player.created`、`player.renamed`（带 `oldName`）、`player.updated`（穿戴变化或换了主人；包括因材质删除、移出衣柜、改为私有而被脱下）、`player.deleted`。
  - 材质：`texture.uploaded`、`texture.updated`、`texture.deleted`。
  - 衣柜：`closet.added`、`closet.removed`。
  - 举报：`report.submitted`、`report.resolved`。
  - 载荷只放 id 和监听者判断要不要处理所需的少量字段（例如 CDN 刷新需要角色名），其它信息监听者自己通过服务读取。字段可以增加；改名、删除或改变含义要升版本并写 ADR（CONTRIBUTING）。
  - 暂不发布：`user.loggedIn`、`user.updated`、`score.changed`、`yggdrasil.joined`。它们在热路径上，等有插件需要时再加。Guard 与 Filter 随插件运行时（第二阶段）一起加，到时再定调用点和拒绝时的 HTTP 映射。
- **审计记录与变更同事务**：
  - 新包 `internal/audit`（`Entry`、`Write`）。账号服务原有的审计写入改为调用它。
  - 做管理操作的服务方法接收 `*audit.Entry`，在自己的事务里写入，并补上只有事务里才知道的信息（举报涉及的上传者、退还的积分、新通知的 id、改名前后的名字）。用户自己的操作传 `nil`。
  - 覆盖：后台与兼容接口的角色改名/转移、换材质、删除，衣柜放入/取出，材质的管理员编辑与删除（只有改的是别人的材质时才写），处理举报，发通知/公告，保存设置（命令行的 `settings set` 也记录，`meta.via = "cli"`）。
  - 管理员同时改名和转移角色改为 `player.Service.Edit` 一个事务完成。

## 备选方案
- 发布事件时直接 `Emit`（进程内、尽力而为）：进程崩溃或事务回滚都会丢失事件，或发出没发生过的事件。
- 单独建 `outbox` 表：`jobs` 已经有租约、退避、清理，再建一张只是重复。
- 通过 context 隐式携带审计记录、由统一的事务助手写入：调用方看不出一个操作会写什么，和"显式的领域服务 + 事务"（03 A08）相悖。

## 影响
- 服务构造函数不变，事件总线通过 `SetHooks` 注入（`app.Core` 里设置），测试里不注入就不发布。
- 审计参数让若干服务方法多了一个参数（列表见 ADR 0027）。
- 文档站的插件开发页面在第二阶段由这份目录生成 Hook 参考。
- **第二阶段上线插件之前要解决**：
  - 事件与邮件共用一个按顺序执行的任务执行器，一个慢的处理器（每个最多 30 秒）会让验证邮件、重置邮件排在它后面。要给事件单独的执行器或并发度。
  - `texture.*` 事件的载荷含私有材质的 hash（凭 hash 能取到文件），订阅它的插件需要相应的权限声明（05 §8）。
