# 0024 兼容 Blessing Skin 的管理类资源 API

- 状态：已接受（2026-10-30）
- 相关：设计文档 02 §2.6；03（A01、C07、C15、§7.4）；ADR 0016、0020、0023

## 背景
M3 第三部分。ADR 0023 做了 OAuth 2.0 与用户级的兼容接口，BS 的 `/api/admin/*` 留到这里：管理员的脚本和面板用它管理用户、角色、衣柜、举报和发通知。BS 的问题：
- 管理端、用户端、OAuth API 三处各写一份逻辑，行为不一致（A01）：OAuth API 给角色换材质时不检查类型、不要求在衣柜里（C07），发通知返回 302 跳转到 `/admin`（C15）。
- 列表的 `q` 用 search-string 语法，分页没有总量控制。

## 决策
- **接口与 scope**（`internal/httpapi/bsadmin.go`）：
  - 用户 `GET /api/admin/users`、`PUT /api/admin/users/{uid}/email|verification|nickname|password|score|permission`、`DELETE /api/admin/users/{uid}`（`UsersManagement.*`）；
  - 角色 `GET /api/admin/players`、`PUT /api/admin/players/{pid}/name|owner|textures`、`DELETE /api/admin/players/{pid}`（`PlayersManagement.*`）；
  - 衣柜 `GET|POST|DELETE /api/admin/closet/{uid}`（`ClosetManagement.*`，`tid` 在请求体里）；
  - 举报 `GET /api/admin/reports`、`PUT /api/admin/reports/{id}`（`ReportsManagement.*`）；
  - 通知 `POST /api/admin/notifications`（`Notification.ReadWrite`）。
  - 管理类 scope 只有管理员能授予；令牌主人不再是管理员时，令牌对这些接口立即失效（ADR 0023）。
- **同一套规则与服务**：
  - 权限规则与后台页面相同：管理员只能管理普通用户，超级管理员管理所有人，谁都不能改自己的权限（`account.AdminUpdateUser`、`MayManageOwned`）。
  - 处理举报、发通知、改名与转移角色原来写在 `/api/v1` 的处理函数里，抽成 `reviewReport`、`sendNotification`、`updatePlayerAsAdmin`、`managedPlayer`、`adminAudit`，两套接口都调用它们，写同样的审计记录（修 A01）。
  - 换材质：材质必须在主人的衣柜里、类型要对（修 C07）；`tid` 为 0 表示脱下。
  - 发通知返回 `{"code": 0}`（修 C15）。`receiver` 的 `all`、`normal` 写一条公告，`uid`、`email` 发给一个人（ADR 0021）。
- **BS 的格式**：
  - 列表是 Laravel 分页（用户和角色每页 10 条，举报 9 条），按 `page` 翻页；用户对象多一个 `ip`（注册地址）；举报带 `texture`、`texture_uploader`、`informer`，`status` 是 0 / 1 / 2。
  - `permission`：-1 封禁，0 普通用户，1 管理员；2（超级管理员）不能通过接口设置（同 BS）。
  - `verification` 切换邮箱验证状态（同 BS）；`score` 设为给定值，差额记入积分流水。
  - 没有权限是 403 `{"code": 1, "message"}`；处理举报时没有权限封禁上传者、举报已处理、上传者已不存在，与 BS 一样是 HTTP 200、`code` 为 1。
  - 一个用户的衣柜一次返回全部（同 BS），最多 1000 条。
- **`q` 搜索**：只认第一个 `字段:值`，其余当作普通文字：
  - 用户：值（或整个 `q`）匹配邮箱、昵称或 ID；
  - 角色：`uid:N` 是这个用户的角色，其它值匹配角色 ID、UUID 或名字开头；
  - 举报：`status:0|1|2`、`reporter:N`、`uploader:N`、`tid:N`。
  - BS 完整的 search-string 语法（排序、比较、多个条件）不支持。
- **分页**：`account.ListUsers`、`player.Search`、`report.List` 的过滤条件增加 `Offset`，并新增 `CountUsers`、`player.Count`、`report.Count`（同一个条件构造函数），`report.Filter` 增加 `UploaderID`、`TextureID`。后台页面仍用游标翻页。

## 备选方案
- 在兼容层里直接查数据库：最快，但会把 BS 的问题（三处各写一份）原样搬过来。
- 实现完整的 search-string 语法：使用的人很少，规则多，容易出错；常见的 `uid:N`、`status:0` 和普通文字已覆盖。

## 影响
- 没有新表和迁移，也没有新设置。
- `internal/httpapi`：新文件 `bsadmin.go`；`reports.go`、`notifications.go`、`admin_players.go` 的处理函数改为调用抽出的共用函数，`/api/v1` 的行为不变。
- 至此 BS 的 OAuth 资源 API（02 §2.6）全部可用；M3 还剩社交登录。
