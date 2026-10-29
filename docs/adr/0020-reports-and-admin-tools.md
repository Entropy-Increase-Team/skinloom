# 0020 举报、后台概览与后台角色、衣柜管理

- 状态：已接受（2026-10-24）
- 相关：设计文档 02 §1.2（F17）、§1.4（F30、F32、F33、F34）；03（C03、C06、C07）；04 §5.1、§5.2、§13；ADR 0017、0019

## 背景
M2 第四部分：用户举报材质、管理员处理举报，以及后台概览和后台的角色、衣柜管理。Blessing Skin 的问题：
- 举报处理"删除材质"直接删文件，不看同一文件的其它材质（C06）。
- 举报者的积分增减是读-改-写（C03），任何材质编号都能举报，包括看不到的私有材质。
- 后台给角色换材质不检查类型（C07）。
- 概览图表把一个月的注册和上传记录全部读进 PHP 再分组。

**M2 拆分调整：** ADR 0017 原把服务端渲染器（头像、预览图、OG 图片）放在第四部分；第三部分的开发日志把第四部分定为后台概览、后台角色管理与举报。按后者执行。渲染器、站内通知与公告（F12）、S3 兼容存储移到 M2 第五部分。

## 决策
- **reports 表**：`texture_id`、`uploader_id`（举报时材质的上传者，封禁对象，材质删了也知道是谁）、`reporter_id`、`handler_id` 都可为空，对应的行删除时置空，举报作为审核记录保留。
  - 字段：`reason`（最多 500 字）、`status`（pending / resolved / rejected）、`action`（delete / ban / reject）、`handled_at`。
  - 索引：`UNIQUE(reporter_id, texture_id)`，同一人对同一材质只能举报一次，并发重复提交由数据库裁决；审核队列按 `(status, id)`。
- **提交举报**（`POST /api/v1/textures/{id}/reports`）：
  - 只能举报自己看得见的材质，不能举报自己的材质；每人每小时最多 20 次。
  - `score.reporter_modification`（BS `reporter_score_modification`，默认 0）：负数是举报费，余额不足返回 402；正数是举报奖励。提交与积分变化在同一事务里写流水 `report.submit`。
  - 我的举报：`GET /api/v1/me/reports`，页面 `/user/reports`。不再公开的材质不显示。
- **处理举报**（`POST /api/v1/admin/reports/{id}/review`）：只有待处理的举报能处理。条件更新 `status = 'pending'` 决定谁赢，两名管理员同时处理只有一个成功，另一个得到 409。
  - **驳回**：收回提交时的奖励（举报者积分不够则跳过）；举报费不退，同 BS。
  - **删除材质**：走 ADR 0019 的唯一删除流程（收藏者退费、blob 引用计数、延迟清理文件，修 C06），与结案在同一事务里。
    - 举报者拿回举报费（冲正 `report.submit`，记 `report.refund`），并得到 `score.reporter_reward`（BS `reporter_reward_score`，记 `report.reward`）。
    - 同一材质的其它待处理举报一并结案：退举报费，不给奖励。只有被处理的那一条得奖励，同 BS，也避免小号刷奖励。
    - 材质已被上传者删除时，只退举报费。
  - **封禁上传者**：经账号服务的管理员修改执行，沿用"管理员只能管理普通用户"的规则，并让对方所有会话下线。先封禁，成功后再结案，没有权限时举报保持待处理。举报者照常退费、得奖励。
  - 每次处理写审计 `admin.report_reviewed`。页面 `/admin/reports`：按状态筛选，三个操作。
- **后台概览**（`GET /api/v1/admin/overview`，页面 `/admin`）：
  - 内容：用户、角色、材质（含公开数）、衣柜条目、存储文件数与总大小（按 blob 计，同一张图只算一次）、待处理举报，以及最近 31 天每天的注册数和上传数。
  - 每天的数量是两次 `created_at` 区间计数（新增 `users(created_at)`、`textures(created_at)` 索引），日界按 `site.timezone` 计算。查询在三种数据库上相同，夏令时也正确，不把记录读进内存。
  - 图表是两张单序列柱状图（小多图，不共用两条纵轴）：悬停、聚焦显示数值，并附表格视图。颜色 `--chart-1` 在明暗两种模式下各自校验过亮度与对比度。
- **后台角色管理**（`/api/v1/admin/players`，页面 `/admin/players`，F32）：
  - 按角色 ID、UUID（带不带连字符都行）或名字开头搜索，也可只看某个用户的角色。
  - 改名沿用名字规则，v3 UUID 随之变化。
  - 转移给另一个用户：新主人衣柜里没有的材质脱下；积分流水留在原主人名下，新主人删除时不退费。
  - 换皮肤、披风：同样只能用主人衣柜里的材质，并校验类型（修 C07）。BS 后台可以按任意材质编号设置，会让角色穿上别人看不到的私有材质，也绕过"移出衣柜即脱下"的规则，所以不沿用。
  - 删除：按 `score.return_on_delete` 退费。
  - 权限同用户管理：管理员管理普通用户与自己的角色，超级管理员管理所有人的（`account.MayManageOwned`）。操作写审计 `admin.player_updated`、`admin.player_textures`、`admin.player_deleted`。
- **后台衣柜管理**（F33，与 BS 一样只有接口）：
  - `GET/POST /api/v1/admin/users/{id}/closet`、`DELETE …/{textureId}`。
  - 管理员放进去的材质不收费、上传者不得奖励；移出时只退这个条目当初的费用（管理员放的为 0）。
  - 审计 `admin.closet_added`、`admin.closet_removed`。
- **修正第三部分的衣柜流水**：收藏费与被收藏奖励原来按"材质/用户"关联流水，而零积分的变化不写流水。站点从收费改为免费以后，同一用户反复收藏、取消同一材质，每次都会找到早已退过的那笔旧流水，再退一次。现在改为按衣柜条目 ID 关联，每次收藏都是新条目，只结算它自己的费用与奖励。回归测试在旧代码上失败（每次多退 5 分）。

## 备选方案
- 处理举报时把封禁放进同一个事务：账号服务的封禁有自己的事务、审计与会话吊销，SQLite 下嵌套写事务会互相等待。先封禁后结案，两名管理员同时用不同操作处理同一条举报时，可能出现"上传者已封禁但举报被驳回"，影响很小。
- 删除材质时给所有举报者奖励：鼓励小号刷奖励，不采用。
- 概览按天 `GROUP BY`：按站点时区截日需要各数据库各自的时区函数；区间计数在 `created_at` 索引上同样只扫一遍当月数据。

## 影响
- 迁移 `20261024100910_reports`：`reports` 表，`users`、`textures` 的 `created_at` 索引。`players.user_id` 不再是不可变字段（只影响生成代码）。
- 新设置 `score.reporter_modification`、`score.reporter_reward`；`GET /api/v1/textures/rules` 的积分规则里增加这两项，供举报表单提前显示。
- 新流水原因 `report.submit`、`report.refund`、`report.reject`、`report.reward`。
- 新包 `internal/report`、`internal/stats`；`texture.Service` 增加 `DeleteIn`（在调用方事务中删除）、`GiveToCloset`；`settings.Location` 统一读取站点时区。
