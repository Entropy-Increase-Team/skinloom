# 0017 材质、文件存储与积分流水

- 状态：已接受（2026-10-24）
- 相关：设计文档 02 §1.3（F16、F20–F22）、§2.1（F51、F53）、03（C03、C06、C11、C12、S07、P01、P02、P05）、04 §5.1–5.4

## 背景
M2 第一部分：上传、皮肤库、衣柜与材质文件。Blessing Skin 的问题：材质文件经 PHP 整个读进内存输出，无 Range（P05）；同一 hash 的多行材质共享文件，删除时不看引用直接删（C06）；积分读-改-写，并发可透支或重复得分（C03）；公开↔私有切换反复丢分（C12）；皮肤库按客户端给的任意列排序（S07），COUNT + OFFSET 分页、无索引（P01、P02）；上传只看 getimagesize 后整图交给 Imagick。

## 决策
- **文件存储**（`internal/storage`）：内容寻址，键为文件字节的 SHA-256（只接受 64 位小写十六进制，路径穿越无从谈起）。写入先写临时文件再改名，重复写同一键为空操作。本部分只有本地驱动（`[storage] path`，默认数据目录下 `textures/`，与 BS 的 `storage/textures/{hash}` 一样是平铺目录），S3 兼容驱动在 M2 后续部分加入。
- **blobs 表 + 引用计数**：每个文件一行（hash、大小、宽高、`ref_count`）；材质指向 blob。上传同一张图（别人的私有副本）共用 blob、`ref_count + 1`。文件只在引用为 0 时才可删除（删除流程在 M2 第三部分）。上传时先写文件、再在事务里写 blob 与材质：材质行永远不会指向不存在的文件；事务失败留下的无引用文件由孤儿清理任务回收（同在第三部分）。
- **上传管线**（`internal/texture`）：
  1. 校验名称（1–50 字符、可选站点正则）、类型、文件大小（`texture.max_size_kb`，硬上限 8 MiB）。
  2. 只读 PNG 头得到宽高，按类型校验（宽为 64 的倍数、高为 32 的倍数；经典皮肤 1:1 或 2:1，纤细皮肤 1:1，披风 2:1；宽 ≤ `texture.max_width` ≤ 2048）。所有合法形状都满足高 ≤ 宽，所以一次解码最多 2048² 像素（16 MiB），伪造尺寸的解压炸弹在解码前被拒；同时解码数限制为 4。
  3. 解码后重新编码为 PNG（最高压缩），丢弃所有辅助块（元数据、文字、动画）；16 位等格式转为 8 位 RGBA。hash = 重新编码后字节的 SHA-256。
  4. 去重：已有同 hash 且（公开，或上传者本人的）材质 → 直接返回该材质，`duplicate = true`，不扣分。
  5. 预检积分（避免为付不起的上传写文件）→ 写文件 → 事务：blob upsert、材质（`likes = 1`）、衣柜条目、积分扣减。
  - 与 BS 不同：重新编码的实现不同，同一张图在 SkinLoom 与 BS 里的 hash 不同；从 BS 导入的文件保留原 hash（M5 导入器），只影响新旧之间的去重。
- **积分**（`internal/score`）：唯一入口 `score.Apply(tx, Change)`：`UPDATE users SET score = score + Δ WHERE id = ? AND score >= -Δ`，受影响 0 行 = 积分不足；同事务写 `score_ledger`（变化量、变化后余额、原因、关联对象、操作的管理员）。注册赠送与管理员修改积分也改走这里（管理员设定余额，流水记差值与操作人）。上传费用：每个开始的 KB × 公开/私有单价 + 衣柜条目费，**奖励只给公开材质**（为第三部分的公开↔私有切换保持对称，修 C12）。
- **皮肤库列表**：只列公开材质；私有材质只出现在"某个用户的上传"里（`uploader=` 为自己，或管理员查看时），以及衣柜里。BS 在每一页里混入"或者是我自己的私有材质"，这个 `OR` 让查询无法按索引顺序扫描。排序白名单 `time | likes`（S07）；游标分页：按时间用 id（id 随上传时间递增，等价于按时间且无需额外索引列），按热度用 `(likes, id)` 作游标。索引 `(visibility, id)`、`(visibility, likes, id)` 服务"全部类型"，`(visibility, type, id)`、`(visibility, type, likes, id)` 服务单一类型，每一页都是一段有序的索引范围。不返回总数（P02），前端"加载更多"。
- **同一用户同一张图只有一个材质**：唯一索引 `(uploader_id, hash)`。上传前的去重查询与并发上传（双击、重放）之间有竞态，由这个约束裁决：冲突时回滚并返回已有材质（`duplicate = true`），只扣一次积分。它同时是"某用户的上传"的索引。
- **上传限流与超时**：每个用户 10 分钟内最多尝试上传 30 次（重复上传不扣分，但同样要解码与重新编码，解码槽只有 4 个）；上传接口的请求体读取超时为 2 分钟（Huma 默认 5 秒，慢速网络传不完几 MB）。文件部分的声明类型接受 `image/png` 与 `application/octet-stream`（系统不认识扩展名时浏览器会这样发），是否 PNG 由内容决定。
- **文件接口**：`/textures/{hash}`（及 `/csl/`、`/usm/` 前缀）不查数据库，直接从存储返回，`Cache-Control: public, max-age=31536000, immutable`、`ETag = hash`，`http.ServeContent` 处理 Range 与条件请求（P05）。`/raw/{tid}` 查材质并遵守可见性与 `texture.allow_download`；它不在 `/api` 下，私有材质时单独解析会话；私有材质的响应 `Cache-Control: private`。
- **私有材质的状态码**沿用 BS 的 `status_code_for_private`（`texture.private_status`，403 / 404）。
- **前端**：皮肤库首屏服务端渲染（筛选是普通链接与 GET 表单，不依赖 JavaScript，地址可分享）；卡片的 2D 正面图在浏览器里从材质文件绘制（旧版 64×32 皮肤先转换为新布局，自动识别纤细手臂）；详情页 3D 预览用 skinview3d（只在浏览器加载）。服务端渲染器（头像、预览图、OG 图片）在 M2 第四部分。
- 上传接口在 OpenAPI 里是 multipart 表单；生成的 TypeScript 客户端会把文字字段作为 Blob（即文件部分）发送，服务端读不到，所以前端用手写的 `uploadTextureFile`。

## 备选方案
- 先写数据库再写文件：事务提交后写文件失败会留下指向缺失文件的材质；先写文件只会留下可回收的孤儿文件。
- 上传不重新编码、只校验：保留了用户图片里的元数据（可能含隐私），也不能统一 16 位等少见格式。
- OFFSET 分页 + 总数：大库每页一次全量 COUNT（P02）；游标分页 + "加载更多"足够浏览。
- 服务端先做 2D 预览图：需要渲染缓存与尺寸白名单等整套设计（第四部分），浏览器绘制零服务端成本，先满足列表展示。

## 影响
- 新表 `blobs`、`textures`、`closet_items`、`score_ledger`（迁移 `20261024050244_textures`）；`users` 删除时材质的上传者置空（公开材质保留，显示"已注销用户"），衣柜与流水级联删除。注销用户私有材质的清理随第三部分的删除流程处理。
- 新配置段 `[storage]`（冷键）；新设置 `texture.*`、`score.per_storage`、`score.private_per_storage`、`score.per_closet_item`、`score.award_per_texture`。
- `account.FieldError` / `RateLimitError` 移到 `internal/platform/validate`（account 保留别名），各模块共用同一套 HTTP 映射。
