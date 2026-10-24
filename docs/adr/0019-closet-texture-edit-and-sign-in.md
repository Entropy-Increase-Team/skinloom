# 0019 收藏、材质编辑与删除、签到与用户中心

- 状态：已接受（2026-10-24）
- 相关：设计文档 02 §1.2（F10、F11、F16）、§1.3（F21、F23）；03（C03、C04、C05、C06、C08、C09、C10、C11、C12）；04 §5.2；ADR 0017（积分流水、blob 引用计数）

## 背景
M2 第三部分：收藏（BS 叫 like）、材质的改名/改类型/改公开性/删除、签到、用户中心。Blessing Skin 在这些地方的问题：
- 积分和 `likes` 都是读-改-写，并发签到能多次得分（C03、C04）。
- 取消收藏时从上传者身上扣奖励，哪怕收藏的是自己的材质，上传者账号已删时直接 500（C05）。
- 公开→私有收回上传奖励，私有→公开却不归还，来回切换持续丢分（C12）。
- 改类型不重新校验尺寸，也不处理正在用它的角色（C08）。
- 举报删除材质时不检查同 hash 的其它材质，直接删文件（C06）；访问详情页发现文件缺失就删记录（C10）。
- 删除用户留下私有材质与衣柜孤儿行（C09、C11）。

## 决策
- **收藏**（`POST /api/v1/me/closet`）：只能收藏自己看得见的材质；`closet_items(user_id, texture_id)` 唯一约束，重复收藏 409。扣 `score.per_closet_item`；上传者若是别人，得 `score.award_per_like`（默认 0，同 BS）。`likes` 用原子 `likes = likes + 1` 更新，同一事务里写入衣柜行（修 C04）。
- **取消收藏**（`DELETE /api/v1/me/closet/{textureId}`）：
  - 角色上穿着它的脱下（角色只能穿衣柜里的材质）。
  - 退费与收回奖励都按**积分流水冲正**：找到这条衣柜记录的 `closet.add` 和上传者的 `texture.like`，原样反向（ADR 0017 的原则，修 C05）。收藏自己的材质没有奖励，也就没有收回；上传者已注销，就没有人可扣。
  - 上传者积分不够扣时，跳过收回、不报错，不能因为别人的余额让取消收藏失败。
  - 退费受 `score.return_on_delete` 控制（BS `return_score`）。
  - 衣柜里的名字可以单独改（`PATCH /api/v1/me/closet/{textureId}`），不影响材质本身。
- **编辑材质**（`PATCH /api/v1/textures/{id}`，上传者或管理员）：
  - **改名**：沿用上传时的名字规则。
  - **改类型**：按 blob 记录的宽高重新校验形状（修 C08）。皮肤与披风之间切换时，正在用它的角色从原槽位脱下；steve↔alex 不影响。
  - **公开→私有**：上传者付 `UploadCost(私有) − UploadCost(公开)` 的差额（含公开上传得到的奖励）；私有→公开退还同样的差额，来回切换不再丢分（修 C12）。管理员改别人的材质不向任何人收费，并写审计日志 `admin.texture_updated`。
  - **公开→私有之后**：其他用户的衣柜里移除这个材质，按上面的取消收藏流程退费、收回奖励、脱下角色。
  - **私有→公开**：若同一张图已经有公开材质，409（皮肤库每张图只展示一次，同 BS）。
- **删除材质**（`DELETE /api/v1/textures/{id}`，上传者或管理员，管理员删除写审计 `admin.texture_deleted`）：
  - 所有收藏者按取消收藏流程退费。
  - 上传者退回这个材质在流水里的净支出（`texture.upload` 与 `texture.visibility` 的合计，扣除上传奖励）；记为 `texture.delete`。净收入时收回，余额不够则跳过。
  - 角色槽位外键 `SET NULL`，穿着它的角色什么也不穿。
- **文件删除**（修 C06、C10）：
  - 文件只经 blob 引用计数删除：最后一个引用消失时删掉 blob 行，并排队一个 `texture.sweep` 任务，在 `SweepGrace`（1 小时）之后执行。
  - 任务只在"没有 blob 行**且**文件修改时间早于宽限期"时删文件：上传是先写文件、后在事务里建 blob 行，同一张图被重新上传时 `Put` 会刷新已有文件的时间，所以不会删掉正在被重新使用的文件。
  - 另有每天一次的孤儿巡检（`SweepOrphans`），清理事务失败或任务丢失留下的、没有 blob 行的旧文件。
  - 读请求从不删数据。
- **删除用户**（修 C09）：真正删除账号之前（管理员删除与注销宽限期到期的清理都会走到），先按删除材质的流程删掉他的私有材质（别人看不到，留着只是垃圾）；公开材质保留，上传者显示为"已注销用户"；衣柜、角色随用户级联删除（修 C11）。权限检查通过之后才执行，拒绝的删除不会误删材质。
- **签到**（`POST /api/v1/me/sign`，修 C03）：
  - `users.last_sign_at` 上的条件更新：`WHERE last_sign_at IS NULL OR last_sign_at <= 截止时间`，影响 0 行即为"还没到时间"，返回 409 与 `Retry-After`。
  - 积分在同一事务里经 `score.Apply` 写入流水（`sign_in`），数额在 `score.sign_min`–`score.sign_max` 之间随机。
  - 间隔模式：距上次 `score.sign_gap_hours` 小时（默认 24）。每日模式（`score.sign_daily`，BS `sign_after_zero`）：按 `site.timezone`（默认 `Asia/Shanghai`，二进制内嵌时区数据）的零点刷新。
  - 设计文档写的是"签到周期唯一约束"；单行条件更新同样原子，又不需要额外的表，所以用它。
- **用户中心**（`GET /api/v1/me/dashboard`，页面 `/user`）：积分、角色数、衣柜数、上传数与占用空间、签到规则与下次时间、公告。

## 备选方案
- 取消收藏时按"当前"的价格退费、扣奖励（BS 的做法）：价格改过之后就会退多或扣多，不采用。
- 删除材质时立即删文件：上传与删除并发时会删掉刚写入的同一张图，引用计数加延迟清理更稳。
- `likes` 定期按衣柜行数校准（设计 03 C04 的建议）：现在所有增减都在同一事务里，暂不需要；从 BS 导入数据时（M6）再做一次校准。

## 影响
- 迁移 `20261024081729_sign_in`：`users.last_sign_at`。
- 新设置 `score.award_per_like`、`score.sign_min`、`score.sign_max`、`score.sign_gap_hours`、`score.sign_daily`、`site.timezone`；`score.return_on_delete` 的作用扩大到衣柜与材质删除。
- 新的流水原因：`closet.add`、`closet.remove`、`texture.like`、`texture.unlike`、`texture.visibility`、`texture.delete`、`sign_in`。
- 新任务类型 `texture.sweep`；存储接口增加 `Stat`、`Walk`。
- 前端：材质页的收藏与管理（改名、类型、公开性、删除），衣柜里的改名与移出，`/user` 用户中心与签到。
