# 0021 渲染器、站内通知与 S3 兼容存储

- 状态：已接受（2026-10-29）
- 相关：设计文档 02 §1.2（F12、F13、F16"设为头像"）、§1.3（F21 SEO）、§2.5；03（C14、C15）；04 §5.1、§5.4；ADR 0017、0020

## 背景
M2 第五部分，也是最后一部分：头像与预览图的服务端渲染、站内通知与公告、S3 兼容的对象存储。Blessing Skin 的问题：
- 渲染结果先按固定比例画、再平滑缩放，像素画被插值模糊。
- 预览图的缓存键不含 `height`，第一次请求的高度被缓存一年（C14）。
- 头像按任意 `size` 渲染，缓存条目无上限。
- 群发通知为每个用户写一行，大站点上管理员要等很久；读一条不存在的通知直接 500（C15）。

## 决策
- **渲染器**（`internal/render`，纯 Go，不需要 cgo）：
  - **规范化**：旧版 64×32 皮肤按 Minecraft 的方式逐面镜像出左手左腿；整块不透明的帽子区域视为空（旧皮肤常把它涂成纯色）。基础层画成不透明，外层（帽子、外套、袖子、裤子）带透明度叠加。高清皮肤按宽度/64 取样。
  - **画法**：2D 头像是脸加帽子；3D 头像是从前上方看的立方体（顶面、正面、头的左侧，三面不同明暗），用逆映射逐像素取样。预览图是皮肤正面与背面并排（纤细皮肤手臂 3 像素），披风是披风外侧。一律最近邻取样，不做平滑。
  - **契约（02 §2.5）**：`/avatar/player/{name}`、`/avatar/user/{uid}`、`/avatar/hash/{hash}`、`/avatar/{tid}`、`/preview/{tid}`、`/preview/hash/{hash}`。
    - `size`、`height` 取白名单里最接近的一档，其它参数（`3d`、`png`）原样保留。
    - 角色不存在是 404（同 BS）；没有皮肤、主人被封禁、看不到的私有材质给灰色占位头像（我们自己画的，不用 BS 的图片）；披风的头像是 422。
  - **输出**：默认无损 WebP（`github.com/HugoSmits86/nativewebp`，纯 Go，MIT）；`?png` 或 `Accept` 里没有 WebP 时输出 PNG，响应带 `Vary: Accept`。
  - **缓存**：
    - 键 = 文件哈希 + 种类 + 尺寸 + 格式（预览图再加材质类型），修 C14；`ETag` 是键的摘要（键里有文件哈希，而头像可能显示主人自己的私有皮肤，`/textures/{hash}` 凭哈希就能取到文件，所以哈希不能出现在公开响应里），带 `If-None-Match` 的重新验证在渲染之前就回 304。
    - 进程内按字节数淘汰的 LRU（64 MiB），同一个键同时只画一次（singleflight），最多 4 个渲染同时进行。渲染结果不写进存储，重启后按需重画。
    - 按哈希的地址永久缓存（`immutable`）；按角色、用户、编号的地址缓存 `delivery.profile_cache_seconds` 秒。
    - 答案取决于访问者时（私有材质）标记为 `private, no-cache`，CDN 不会把上传者看到的图发给别人。
  - `/avatar`、`/preview` 和 `/raw` 一样在 `/api` 之外，自己从 Cookie 读会话（`cookieViewer`）。
  - **SEO（F21）**：公开材质页的 `og:image` / `twitter:image` 是 `/preview/{tid}?height=256&png`；`metadataBase` 取自 `site.url`。
- **头像**（F13、F16）：
  - `PUT /api/v1/me/avatar` 选择一个看得见的皮肤（同 BS，不要求在衣柜里），0 为移除。`/avatar/user/{uid}` 渲染时再检查一次：材质后来变成别人的私有材质，就画占位头像，不泄露。
  - `users.avatar_texture_id` **不设外键**：SQLite 给已有表加外键列需要重建整张表，见下。删除材质的唯一流程会清空指向它的头像。
- **站内通知**（F12，`internal/notify`）：
  - `notifications` 是每个用户的收件箱：标题、Markdown 正文、`read_at`。
  - 群发写一行 `announcements`（受众：所有用户 / 普通用户）。用户下次打开收件箱或查看未读数时，把还没收到的公告复制成自己的通知行。`users.announcements_seen` 记住复制到哪一条，平时只多一次按主键的范围查询；`UNIQUE(user_id, announcement_id)` 让并发投递只留一份。之后注册的用户收不到旧公告，同 BS。
  - 设计文档（04 §5.1）原是"公告表 + 已读表、列表时合并"；改为按需复制，收件箱只有一张表，分页、未读数、已读状态都简单。成本只落在真正打开收件箱的用户身上。
  - 给单个用户发通知前先投递他未收到的公告，收件箱按 id 排序就是发送顺序。
  - 读一条不属于自己或不存在的通知返回 404（修 C15）。
  - **Markdown** 用 goldmark（GFM，换行即换行）在服务端渲染：保持默认的安全模式，原始 HTML 不输出，`javascript:` 链接被丢弃。通知正文和站点公告（`site.announcement`）都以 `…Html` 字段返回，前端直接插入。
  - 接口：`GET /api/v1/me/notifications`、`GET …/unread`、`POST …/{id}/read`、`POST …/read-all`、`POST /api/v1/admin/notifications`（`to`：all / users / user，按 `userId` 或 `email`）。发送写审计 `admin.notification_sent`。
- **S3 兼容存储**（`internal/storage/s3.go`，minio-go v7，Apache-2.0）：
  - 配置：`[storage] driver = "s3"` 与 `endpoint`、`bucket`、`region`、`access_key`、`secret_key`（建议用 `SKINLOOM_STORAGE__SECRET_KEY`）、`prefix`、`path_style`、`insecure`、`public_url`。启动时检查桶是否存在。
  - 键与本地驱动相同（哈希），放在 `prefix` 下。
  - "重复写入只刷新修改时间"（延迟清理依赖它，见 ADR 0019）用"把对象复制到自身并替换元数据"实现，这是 S3 刷新 LastModified 的唯一办法。
  - 读取是带 Range 的 GET。`Walk` 只列 `prefix` 下、名字是哈希的对象，桶里的其它东西不碰。
  - 设置了 `public_url` 时，`/textures/{hash}` 302 到 `public_url/prefix/hash`，文件由 CDN 直接发出；`/raw/{tid}` 仍由服务器转发，以保留下载文件名。
  - 测试用测试文件里的一个小型假 S3（PUT、HEAD、带 Range 的 GET、DELETE、ListObjectsV2、复制，以及 minio 的 aws-chunked 上传体）。gofakes3 会把整个 AWS SDK 带进模块图，没有采用。
- **SQLite 迁移不重建表**：
  - Atlas 在 SQLite 上改不了的表会重建：`PRAGMA foreign_keys = off`，建 `new_x`，拷贝，`DROP TABLE x`，改名。
  - goose 把每个迁移文件放在事务里执行，那条 PRAGMA 在事务里无效，而 SkinLoom 打开 SQLite 时开着外键：`DROP TABLE users` 会先删光所有行，并级联删除会话、衣柜、角色、积分流水。连接池有 4 个连接，改成 `NO TRANSACTION` 也不能保证 PRAGMA 和 DROP 在同一个连接上。
  - 因此本次 `users` 加列手写为 `ADD COLUMN`（第三部分的 `last_sign_at` 也是这样），并新增测试 `migrations.TestSQLiteMigrationsDoNotRebuildTables`：任何 SQLite 迁移出现重建就失败，必须手写改掉。

## 备选方案
- 渲染结果写进存储（04 §5.4 原设想的 `renders/{hash}/…`）：存储键目前只接受哈希，S3 上还要另算清理；渲染很快（一个头像毫秒级），进程内缓存足够，多实例各自缓存。
- 有损 WebP：像素画用无损更小也更清晰。
- 群发时一次写 N 行（BS 的做法）：大站点上请求会非常久，并且大部分用户可能永远不会打开收件箱。
- 用 aws-sdk-go-v2：依赖更重；minio-go 对各家 S3 兼容服务都有实际使用。

## 影响
- 迁移 `20261024112658_notifications_avatars`：`announcements`、`notifications` 表；`users.avatar_texture_id`（带索引）、`users.announcements_seen`。
- 新依赖：`github.com/HugoSmits86/nativewebp`、`github.com/yuin/goldmark`、`github.com/minio/minio-go/v7`；`golang.org/x/net` 升到 v0.62.0（GO-2026-6866、GO-2026-6860，代码没有调用到）。
- 新包 `internal/render`、`internal/notify`；`storage.S3`、`storage.Linker`；`settings.Location` 之外新增 `account.GetUserByEmail`。
- 前端：页头头像与通知铃铛、`/user/notifications`、个人资料页的头像、衣柜"设为头像"、后台概览的"发送通知"、公告按 Markdown 显示、材质页 `og:image`。
- M2 的全部内容（04 §13：上传管线、blob、皮肤库、详情页 SEO、衣柜、角色、积分与签到、举报、通知/公告、渲染器、CSL/USM/Legacy）至此完成。
