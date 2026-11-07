# 0037 契约测试：黄金文件、authlib-injector 端到端、渲染快照与站点对比

- 状态：已接受（2026-11-05）
- 相关：设计文档 02 §2、§6；ADR 0021、0035

## 背景
- 02 §6 列了五类契约测试。第 3、4 条（密码向量、迁移回归）在 ADR 0036 完成，剩下三条：
  1. 黄金文件：CSL、USM、Legacy、Yggdrasil 全部端点，覆盖存在、不存在、封禁、私有、无皮肤、slim、披风几种角色；
  2. authlib-injector 端到端：authenticate → join → hasJoined → 签名验证；
  5. 渲染快照：2D/3D 头像、全身预览、披风预览，按像素差阈值比较。
- 02 §6 第 1 条原本要求在真实的 BS 6 + 插件环境里录制。这里既没有 PHP，也没有一个可用的 BS 站点。

## 决策
- **规范化放在一个包里**（`internal/contract`）。响应被转成稳定的文本：
  - 状态行；
  - 属于契约的响应头：`Content-Type`、`Cache-Control`、`ETag`、`Last-Modified`（写成 `<date>`）、`Location`、`X-Authlib-Injector-API-Location`；
  - JSON 按键排序后缩进输出，不转义 `<`；
  - 每次不同的值换成标记：时间戳、令牌、版本号，公钥写成 `<RSA public key, N bits>`；
  - `textures` 属性先解开 base64，签名用站点公钥按 SHA1withRSA 验证，写成 `<signature valid>` 或 `<signature INVALID>`；
  - PNG 写成尺寸加 SHA-256。
- **黄金文件录的是 SkinLoom 自己的回答，逐条对照 BS 的源码审过**（`internal/httpapi/testdata/contract.golden`）。
  - 测试站点有五个角色：Steve（经典皮肤加披风）、Alex（slim）、Naked（什么都没穿）、Hidden（穿着私有皮肤）、Banned（主人被封禁），另有一个不存在的名字。
  - 共 45 个请求，覆盖 CSL、USM、Legacy、材质文件、`/raw`，以及 Yggdrasil 的元数据、批量查询、按名字和 UUID 查询、authenticate、validate、join、hasJoined、invalidate、refresh、signout。
  - 回答的任何变化都会显示为这个文件的 diff；确认后用 `go test -run TestContractGolden -update` 接受。
- **真实 BS 站点的对比做成命令**：`skinloom import compare --bs <url> --skinloom <url> <角色名>...`。
  - 它用启动器和游戏服务器的方式向两个站点提同样的问题：CSL、USM、旧版 PNG，以及从 CSL 拿到的材质文件；Yggdrasil 的按名字查询、批量查询、按 UUID 查询（带签名和不带签名两种）。
  - 每个站点的签名用它自己的公钥验证，站点地址替换成 `<site>`。第一行比较两站的签名公钥本身：导入没有带过来密钥时，签名各自有效，只有这一行会显示不同（ADR 0039 审查时加入）。
  - 只比客户端读的东西：状态、媒体类型、成功回答的正文。错误提示的措辞和缓存时间不比。
  - 迁移指南把它列为切换域名之前的一步，所以 02 §6 第 1 条在每次真实迁移时都会对着真实的 BS 跑一遍。它自己的测试拿同一个站点比自己，再改掉一个回答，必须正好找出这一处。
- **与 BS 有意不同之处**（审黄金文件时逐条确认）：
  - 角色名在所有数据库上都不区分大小写。BS 是否区分取决于数据库的排序规则。
  - CSL 和 USM 的回答带 `Cache-Control: public, max-age=60` 和 `Last-Modified`，CDN 可以缓存，换装时由 ADR 0032 的刷新清掉。
  - 封禁和不存在时的错误正文是 SkinLoom 自己的措辞，状态码与 BS 相同（403、404；USM 的 404 正文 `{"errno":1,"msg":"Player not found."}` 与 usm-api 相同）。
  - 主人被封禁的角色不出现在 Yggdrasil 的批量查询里，与按名字查询一致。
- **审黄金文件时改掉的一处差异**：yggdrasil-api 的 `textures` 属性带 `"isPublic": true`，SkinLoom 原来没有，现在加上了。
- **authlib-injector 端到端用一个最小的 Java 程序**（`internal/httpapi/testdata/authlib/Harness.java`，Java 11 以上用 `java Harness.java` 直接运行，不需要构建工具）。测试启动真实的 HTTP 服务器，程序依次检查：
  - 元数据里的公钥；
  - authenticate；有多个角色时用 refresh 选定角色；validate；
  - 按 Minecraft 的方式计算 serverId 的 join 和 hasJoined；
  - 每个属性的 SHA1withRSA 签名；
  - 材质地址在 `skinDomains` 白名单里（以 `.` 开头的项匹配子域名），文件的 SHA-256 与地址中的 hash 一致；
  - 按 UUID 查询（`unsigned=false`）、按名字查询（任意大小写）、invalidate。

  没有 Java 时跳过。CI 的 Linux 任务设置了 `SKINLOOM_TEST_JAVA=1`，缺 Java 就失败而不是跳过。
- **渲染快照对比的是 SkinLoom 自己审过的输出**（`internal/render/testdata/snapshots`）。
  - BS 的渲染器不在仓库里，而且 ADR 0021 有意用最近邻缩放代替 BS 的平滑。所以不追求与 BS 逐像素相同，而是固定下现在的输出，并另外检查 2D 头像在每个分辨率上与 BS 的取法一致（脸加帽子层，每个皮肤像素一个方块）。
  - 参考皮肤是合成的：每个像素颜色都不同，帽子层半透明。错位一个像素、取错一块、混合算错都会显出来。覆盖经典、旧版 64×32、高清三种皮肤，slim 预览和披风预览。
  - 阈值：差值超过 2（每个通道）的像素不超过 0.5%。arm64 的融合乘加会让边缘像素差 1。
  - 验证过测试有效：把 3D 头像的侧面明暗从 0.86 改成 0.80，三个 3D 快照全部失败。

## 影响
- 新包 `internal/contract`；新命令 `skinloom import compare`。
- Yggdrasil 的 `textures` 属性多了 `isPublic`。
- CI 的 Go 任务在 Linux 上要求有 Java（GitHub 的 Ubuntu 镜像自带）。
- 改动游戏接口的回答时，要同时更新黄金文件，并在审查中说明理由。
