# 更新日志 / Changelog

本文件记录所有对用户、站长与插件开发者可见的变化。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.1 发布后由 release-please 根据 Conventional Commits 自动维护；人写的发布说明见文档站"发布说明"。

## [Unreleased]

### 安全（依赖）
- 前端工作区强制 undici ≥ 7.30.1（`overrides` 与 `pnpm.overrides`）：API 客户端生成工具 orval 经 `@scalar/json-magic@0.15.x` 锁定了受 GHSA-xjj6-f33q-929c（拒绝服务）影响的 undici 7.29.1，0.15 系列没有修复版本。只影响开发时生成客户端，运行中的站点不受影响；等 orval 升级依赖后可以移除这条覆盖。

### 新增（M2 材质与角色 · 第一部分：上传、皮肤库与衣柜）
- **上传材质** `/skinlib/upload`：PNG 皮肤（经典 / 纤细）与披风，自动识别类型并预览，提前显示所需积分。接口 `POST /api/v1/textures`（multipart），上传规则 `GET /api/v1/textures/rules`。
- **皮肤库** `/skinlib`：按类型筛选、按名称搜索、按最新或最受欢迎排序，"加载更多"翻页；首屏服务端渲染，筛选结果的地址可以分享。接口 `GET /api/v1/textures`。
- **材质页** `/skinlib/show/{tid}`：可旋转的 3D 预览、上传者、收藏数、大小、上传时间，下载原图。接口 `GET /api/v1/textures/{tid}`。
- **我的衣柜** `/user/closet`：皮肤、披风分栏，按名称搜索。接口 `GET /api/v1/me/closet`。页头增加"衣柜"入口。
- **材质文件**：`/textures/{hash}`（以及 `/csl/textures/`、`/usm/textures/`）可被永久缓存，支持 ETag 与断点续传；`/raw/{tid}` 下载原图。
- **积分流水**：每次积分变化（注册赠送、上传、管理员修改）都记录变化量、余额与操作人。
- 新设置 `texture.max_size_kb`、`texture.max_width`、`texture.name_regexp`、`texture.allow_download`、`texture.private_status`、`texture.content_policy`，以及上传费用 `score.per_storage`、`score.private_per_storage`、`score.per_closet_item`、`score.award_per_texture`；新配置段 `[storage]`（材质文件目录，默认数据目录下 `textures/`）。
- 数据库新增 `blobs`、`textures`、`closet_items`、`score_ledger` 表（迁移 `20261024050244_textures`）。文档站新增"皮肤库与材质"页面；ADR 0017。

### 安全与正确性（M2 第一部分）
- 上传先读 PNG 头部校验尺寸，再解码整张图片，伪造尺寸的解压炸弹在解码前被拒绝；图片重新编码后保存，元数据不会留下。
- 同一张图的多个材质共用一个文件并计数引用，删除时不会误删别人的材质文件（修 Blessing Skin C06）。
- 积分扣减是一条带条件的原子更新，并发上传不会把积分扣成负数（修 C03）；上传奖励只给公开材质（为公开↔私有切换的对称计费做准备，C12）。
- 皮肤库只接受固定的排序方式（修 S07）；游标分页，不再每页全量计数（P02）。皮肤库只列公开材质（私有材质在上传者的衣柜里），每一页都能沿索引顺序读取。
- 同一用户同时上传两次同一张图（双击、重放）只会生成一个材质、只扣一次积分（唯一索引裁决并发）。
- 每个用户 10 分钟内最多上传 30 次；上传的请求体读取时间放宽到 2 分钟，慢速网络也能传完；系统不认识扩展名、以 `application/octet-stream` 发送的 PNG 也能上传（以内容判断）。
- `[storage] path` 支持绝对路径；材质文件地址支持 `HEAD` 请求。

### 新增（M1 账号 · 第四部分：个人资料、设备与后台用户管理）
- **个人资料** `/user/profile`（点击页头的昵称进入）：修改昵称与语言偏好（也决定邮件语言）、修改密码、修改邮箱（向旧地址发通知、向新地址发验证链接）。接口 `PATCH /api/v1/me`、`POST /api/v1/me/password`、`POST /api/v1/me/email`。`/.well-known/change-password` 跳转到这里，密码管理器可以直接带你来改密码。
- **登录设备**：查看账号在哪些设备上登录（浏览器、地址、最近活跃），可以单独退出某个设备或退出其它所有设备。接口 `GET /api/v1/me/sessions`、`DELETE /api/v1/me/sessions/{id}`、`POST /api/v1/me/sessions/revoke-others`。
- **注销账号**：需要当前密码，立即退出登录；30 天内用原密码登录即可恢复，之后账号被永久删除、邮箱可以重新注册。管理员需先被降级才能注销。接口 `POST /api/v1/me/delete`。
- **后台用户管理** `/admin/users`（点击页头的角色徽章进入）：按邮箱、昵称或 ID 搜索，按角色、状态筛选；修改昵称、邮箱、积分、验证状态、密码、角色（仅超级管理员），封禁与解封，恢复已注销的账号，删除用户。接口 `GET/PATCH/DELETE /api/v1/admin/users/{id}`、`GET /api/v1/admin/users`。
- **审计日志** `/admin/audit`：登录、注册、改密码、改邮箱、注销、后台修改与设置变更都有记录，可按动作前缀筛选。接口 `GET /api/v1/admin/audit`。
- 设计记录：ADR 0016。

### 安全（M1 账号 · 第四部分）
- 改密码、改邮箱后，除当前设备外的所有登录立即失效（修 Blessing Skin S08）；修改前都要验证当前密码，15 分钟内最多错 5 次。
- 语言偏好只接受站点提供的语言，非法值返回 422，不会写进数据库（修 S04：BS 中一个带 `?lang=` 的链接就能让账号永久报 500）。
- 管理权限统一检查：管理员只能管理普通用户，只有超级管理员能改角色；不能修改自己的角色或状态；最后一个超级管理员不能被降级或封禁（并发操作也不行）。
- 封禁或由管理员重设密码后，该用户在所有设备上立即退出；管理员给用户换了邮箱，新地址需要重新验证。

### 安全（M1 发布前审查）
- **邮件链接只用 `site.url` 生成**：此前未设置 `site.url` 时，链接取自请求的 `Host` 头，攻击者可以让站点向受害者发出一封真实的重置密码邮件、链接却指向自己的服务器（密码重置投毒）。现在未设置 `site.url` 时，邮箱验证与找回密码不可用（站点 bootstrap 的 `auth.mail` 为 false，接口返回 503 并说明原因）。安装向导会自动填写 `site.url`。
- 修改邮箱（用户本人或管理员）后，发往旧邮箱、尚未使用的验证链接与重置链接立即作废；修改密码后，尚未使用的重置链接作废。
- "最后一个超级管理员"的保护在 PostgreSQL / MySQL 上存在并发窗口（两名超级管理员同时互相降级可能都成功），现已串行化处理。
- 后台任务的租约从认领时刻起算：此前一批任务里排在后面的任务，可能在租约已过期时才开始执行，多副本下同一封邮件可能被发送两次。
- 注册：已被占用的邮箱在计算密码哈希之前就被拒绝；同一地址每小时最多尝试 30 次（`auth.registrations_per_ip = 0` 时不限），避免无验证码的站点被大量注册请求占满密码哈希资源、拖慢所有人的登录。
- 图形验证码：同一客户端 10 分钟内最多获取 300 张，避免大量请求消耗 CPU、挤掉其他访客尚未作答的验证码。
- 修改邮箱每个账号每小时最多 10 次，"邮箱已被注册"的提示不能被用来批量探测邮箱。

### 新增（M1 账号 · 第三部分：邮件、邮箱验证与找回密码）
- **邮件**：`mail.driver`（none / smtp / log）、`mail.from`、`mail.smtp.*`；支持 465（TLS）、587（STARTTLS），PLAIN 与 LOGIN 认证；中英文邮件模板（纯文本 + HTML）。管理员可用 `POST /api/v1/admin/mail/test` 发测试邮件并看到 SMTP 错误。
- **后台任务队列**：新表 `jobs`（迁移 `20261023151731_jobs`），邮件在后台发送，失败自动重试（1 分钟、5 分钟、15 分钟、1 小时），多副本下同一封邮件只发一次。
- **邮箱验证**：注册后自动发送验证邮件；`/auth/verify/<令牌>` 点击确认后生效；未验证的用户在页面顶部看到提示，可重新发送。接口 `POST /api/v1/auth/email/verify`、`POST /api/v1/auth/email/resend`。
- **找回密码**：登录页"忘记密码？"→ `/auth/forgot` → 邮件 → `/auth/reset/<令牌>` 设置新密码。接口 `POST /api/v1/auth/password/forgot`、`POST /api/v1/auth/password/reset`。
- 站点 bootstrap 的 `auth.mail` 表示邮件是否可用；文档站"账号、注册、验证码与邮件"页面补充邮件配置示例（QQ 邮箱等）。

### 安全（M1 账号 · 第三部分）
- 重置密码后，该账号在所有设备上的登录立即失效（修 Blessing Skin S02）。重置链接 30 分钟、验证链接 24 小时有效，都只能使用一次，并且只有最新签发的一条有效（修 S02、S03）。
- 找回密码对任何邮箱都返回同样的结果，限流对未注册的邮箱同样计数，不泄露账号是否存在（S05）；需要验证码。
- SMTP 错误不再出现在给访客的响应里（S06），只进日志与管理员的测试接口。
- 验证链接需要在页面上点击确认（POST），邮件安全软件预先打开链接不会把它用掉。
- 后台任务完成后立即清除其中的邮件内容（含一次性链接）。

### 新增（M1 账号 · 第二部分：注册与验证码）
- **注册**：`/auth/register` 页面与 `POST /api/v1/auth/register`，注册后自动登录。新设置：`auth.registration`（open / closed，关闭后页头与首页的注册按钮自动隐藏）、`auth.registrations_per_ip`（同一地址 24 小时内的注册数，默认 3）、`score.initial`（初始积分，默认 1000）。站点完成安装前不开放注册。
- **验证码**：`captcha.provider` 可选内置图形验证码（默认，不依赖第三方、中国大陆可用）、Cloudflare Turnstile、Google reCAPTCHA（经 recaptcha.net）、hCaptcha，热切换；第三方的 site key / secret key 用 `captcha.<提供方>.site_key` / `.secret_key` 设置，secret 不会发给浏览器。新接口 `GET /api/v1/captcha/image`。
- **登录验证码**：同一客户端对同一账号失败 `auth.login_captcha_after` 次（默认 3）后要求验证码；登录页按需显示。
- 站点 bootstrap 增加 `auth`（注册是否开放、验证码类型与 site key）；登录页与注册页互相链接，页头增加"注册"按钮。
- 文档站新增"账号、注册与验证码"页面，配置页列出全部可用设置。

### 安全（M1 账号 · 第二部分）
- 图形验证码每个挑战只能尝试一次（对错都作废），5 分钟过期，答案只在服务端。
- 注册先校验字段、再校验验证码、最后才计算密码哈希；验证码服务不可用或密钥错误时拒绝请求并告警，不会放行。
- 启用验证码后，针对单个账号的分布式撞库只会让所有人需要验证码，不会锁定账号；按地址的硬上限不变。
- 注册数按精确地址（数据库，重启后仍有效）与 IPv6 /64（内存）双重计数。

### 新增（M1 账号 · 第一部分）
- **首次安装向导**：首次启动时控制台打印一次性安装链接（`/setup?token=…`，24 小时有效，`--open` 直接打开）。在向导里设置站点名称、站点地址并创建超级管理员，完成后自动登录、链接作废。`skinloom setup-link` 可随时生成新链接（容器内同样可用）。
- **登录与会话**：`/auth/login` 页面与 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me`；"记住我"保持 30 天，否则关闭浏览器即失效。页头显示当前用户与退出按钮。
- 管理 API 接受管理员会话（`admin.token` 保留给脚本使用）；设置修改记录修改人并写审计日志。
- 从 Blessing Skin 导入的密码哈希（BCRYPT、ARGON2I、MD5、SHA256、SHA512 及其 SALTED2* 变体）可以直接登录，首次登录后自动升级为 argon2id。
- 数据库新增 `users`、`sessions`、`auth_tokens`、`audit_logs` 表（迁移 `20261023095021_accounts`）。
- 站点 bootstrap 增加 `needsSetup`，未完成安装时所有页面顶部提示如何找到安装链接。

### 安全（M1 账号 · 第一部分）
- 安装向导只能凭控制台打印的一次性令牌进入，完成后永久关闭（修 Blessing Skin S11）；并发的两次安装只会有一次成功。
- 密码使用 argon2id，并发哈希数受限；长度规则统一为 8–128 个字符（S15）。
- 会话令牌在数据库中只存 SHA-256；Cookie 为 `HttpOnly`、`SameSite=Lax`，HTTPS 下加 `Secure`；被封禁用户的会话立即失效。
- 登录失败一律返回"邮箱或密码不正确"，未注册邮箱与错误密码耗时一致（S05）；失败次数按"客户端地址""地址 + 账号""账号"三层限制，超出返回 429（S01）。单个攻击者无法把真正的用户锁在门外。
- 带会话 Cookie 的写请求必须带 `X-SkinLoom-Request` 头，且 `Origin` 必须是本站（CSRF 防护）；登录后跳转只接受站内路径（防开放重定向）。

### 变更（部署与发布）
- **默认端口改为 17546**（前端内部端口 17547），避开 8080 / 3000 以及常用的四位数端口，也不落在系统临时端口与 Kubernetes NodePort 范围内。已有站点的 `config.toml` 保留原端口。见 ADR 0013。
- Docker Compose 成为推荐部署方式：`minimal`（一体镜像 + SQLite）、`standard`（前后端分离 + MariaDB）、新增 `postgres`；可用 `SKINLOOM_VERSION`、`SKINLOOM_PORT`、`TZ` 调整，拉不到镜像时自动用源码构建。
- 新增 Kubernetes / k3s 清单（Kustomize）：`deploy/kubernetes/all-in-one`（单 Pod + SQLite）与 `deploy/kubernetes/split`（后端 + 可扩容前端 + PostgreSQL），非 root、带探针，k3s 开箱即用。
- 发行包附带后台服务配置 `service/`：systemd 单元（带沙箱选项）、macOS launchd、Windows 计划任务安装脚本。
- 新增 `skinloom healthcheck` 命令；三个镜像都声明了 `HEALTHCHECK`。
- 发布工作流：推送 `v*` 标签直接发布 Release（带 `-` 的为预发布），附仅后端的单文件程序；镜像增加 `latest`（最新发布）与 `edge`（main 分支）标签；Go 改为原生交叉编译，镜像构建更快。
- 文档站通过 GitHub Pages 发布（`docs.yml`），自动适配自定义域名或 `/skinloom/` 子路径。
- README 重写：部署方式一览，Compose、docker run、Kubernetes / k3s、发行包后台常驻、反向代理与 HTTPS、端口说明；文档站新增对应的部署页面。

### 修复（M1 账号审查）
- 32 个汉字的昵称会让安装失败（数据库层按字节计长度，校验按字符）。
- 公开的安装接口在检查令牌之前就计算密码哈希，可被用来消耗 CPU 与内存，安装完成后依旧如此；现在先做廉价检查。
- 任一地址连续 10 次登录失败会把该账号对所有人锁定 15 分钟，攻击者可借此让超级管理员一直无法登录；改为三层计数（见上）。
- 客户端地址超过 45 个字符（带接口名的 IPv6）或超长 User-Agent 会让登录失败。
- 导入的 argon2 哈希最多可要求 1 GiB 内存、255 个线程；上限降为 256 MiB、16 个线程。
- 在同一浏览器再次登录（切换账号）后，旧会话在数据库里仍然有效。
- 安装完成后保存站点名称与地址时，若浏览器已断开会丢失设置。
- 旧密码升级为 argon2id 时在事务内计算哈希，延长了事务时间。
- 安装向导里的字段错误改为当前语言显示。
- 列表型配置（如 `trusted_proxies`）无法通过环境变量设置：现在用逗号分隔。

### 变更（2026-10-23 晚）
- 前端包管理器改为 **bun**（仓库只提交 `bun.lock`）；没有 bun 时可用 pnpm 或 npm 安装和运行全部脚本。`skinloom up --dev` 按 bun → pnpm → npm 自动选择，新增热加载配置 `web.package_manager`。见 ADR 0011。
- Node.js 统一为 24：发行包内置 Node 24.22.1，Docker 镜像基于 `node:24-alpine`，CI 使用 Node 24。
- GitHub Actions 升级到 Node 24 版本（checkout / setup-go / setup-node v7，docker v4–v7，action-gh-release v3）。

### 修复（2026-10-23 晚）
- CI "Generated files are up to date" 失败：迁移生成工具 `migdiff` 没有注册 MySQL / PostgreSQL 的 `database/sql` 驱动，漂移检查在这两种数据库上无法运行。

### 新增（M0 地基）
- `skinloom` 命令行：`up`（后端 + 前端 + 网关，单端口）、`serve`、`migrate [status]`、`settings get|set`、`admin token`、`openapi`、`version`。
- 启动器：监管 Next.js 子进程（`web.mode = managed | dev | external | off`），崩溃退避重启，日志合并，Windows 下终止整个进程树。
- 网关：后端保留路径、`/_next/static` 由后端直接提供（`immutable` 缓存）、其余页面反代前端；前端未就绪时显示占位页。
- 数据库：SQLite / MySQL / MariaDB / PostgreSQL；ent 建模、Atlas 生成迁移、goose 执行（带锁），SQLite 迁移前自动快照。
- 运行时设置：类型化定义、校验（逐字段错误）、`null` 重置为默认、配置版本号；跨进程 2 秒内生效。
- 热加载：设置/主题变化经 SSE（`/events`）推送到已打开的页面；配置文件热键即时生效（兼容编辑器原子保存），冷键提示重启。
- Hook 总线：Guard / Filter / Event，按贡献者原子替换，带失败策略与超时。
- 主题：主题包标准（`plugin.json`，`kind: theme`），内置默认主题；token 覆盖（`theme.overrides`）、首页区块编排（`home.blocks`）。
- REST API：`/api/v1/site/bootstrap`、`/api/v1/admin/settings`、`/api/v1/admin/themes`；OpenAPI 3.1（`/api/docs`）；前端客户端由 orval 生成。
- 前端：Next.js 16 App Router，主题 token 注入、明暗模式、中英双语、首页区块（hero、公告、特性、启动器配置）、关于页（鸣谢 Blessing Skin）。
- 发行：`deploy/packaging/package.sh` 构建含 Node 运行时的发行包与启动脚本；Docker（server / web / all-in-one）与 Compose 示例。

### 安全
- 可信代理：`server.trusted_proxies` 生效（此前只写在配置模板里）。只有来自可信代理的 `X-Forwarded-For/-Proto/-Host` 才被采信，客户端 IP 取最右侧的非可信地址；其余来源一律以连接对端为准。支持热加载。
- 网关先规范化路径再路由（`/a/../api`、`//api` 308 跳转到规范路径），无法再绕过路径归属。
- 所有响应默认带 `X-Content-Type-Options`、`Referrer-Policy`、`X-Frame-Options`（不覆盖前端自己设置的值）。
- 主题资源与静态文件只提供普通文件，不再列出目录。
- 首页区块中的链接与背景图只接受站内路径和 http(s) 地址（拒绝 `javascript:`、`data:`、`//host` 等）。
- 发行包打包时按 nodejs.org 官方 `SHASUMS256.txt` 校验下载的 Node 运行时。
- 管理员令牌文件改为原子创建：服务端与命令行同时首次启动时不再生成两个不同的令牌。
- `/events` 同时连接数上限（默认 5000），超出返回 503。
- 文档站构建依赖升级到已修复版本（serialize-javascript、uuid、tinypool、postcss-selector-parser）；`braces` 上游暂无修复版本，仅用于构建期匹配仓库内文件，记录为已知风险。

### 修复
- 关闭服务时，已打开的页面（SSE 连接）会让关闭等满 15 秒；浏览器预连接但未发请求的连接会再拖 5 秒。现在关闭在毫秒级完成。
- 前端进程崩溃后，若其子进程仍占用输出管道，监管者会一直卡住、不再重启前端；现在 3 秒内识别并重启。
- `web.port` 被其它程序占用时，网关会把公网请求转发给那个程序；现在自动改用空闲端口并告警（`web.port = 0` 表示总是自动选择）。
- Windows：`skinloom` 被强制结束时前端子进程会残留并占住端口；现在前端放在作业对象里随主进程退出。Linux 使用父进程退出信号。
- 设置在并发重载时可能短暂回退到旧版本。
- 缺少管理员令牌时返回 401（此前是 422）。
- 前端在服务端请求后端时设 5 秒超时；后端返回非 JSON（如代理错误页）时给出明确错误。
- 已打开的页面改为整个生命周期只保持一条 SSE 连接，配置变化后在 0–1.5 秒内随机刷新，避免大站所有页面同时重渲染。
- Docker：`server` 镜像的 `/data` 归属非 root 用户（此前新卷不可写导致首次启动失败）；镜像构建会带上前端的 `.npmrc`。
- 仓库强制 LF 换行（`.gitattributes`），避免在 Windows 上打出的发行包里 `start.sh` / `start.command` 是 CRLF 而无法运行。

### 变更
- 源码仓库为 [Entropy-Increase-Team/skinloom](https://github.com/Entropy-Increase-Team/skinloom)，Go 模块路径改为 `github.com/Entropy-Increase-Team/skinloom`；官方域名 **skinloom.cn**（官网、文档站、插件清单 JSON Schema）；容器镜像为 `ghcr.io/entropy-increase-team/{skinloom,server,web}`。
- 移除未实现的配置键 `server.public_url`；站点公开地址统一使用运行时设置 `site.url`。
- 配置文件中的未知键（拼写错误）会在启动和热加载时告警；`web.port`、`log.format`、`server.trusted_proxies` 增加校验。
- MySQL 时间列改为 `DATETIME(6)`（此前为 `TIMESTAMP`，2038 年溢出且受会话时区影响）。
- 日志：组件日志器缓存已构建的处理器，每条日志从 8 次内存分配降为 0 次（约 1050 → 700 ns）。
- 数据目录权限改为 0750。

### 测试
- 新增端到端测试（真实端口启动、SSE、经管理 API 热更新、带打开连接的快速关闭）、启动器监管测试（以测试程序自身作为假前端，含"孙进程占用管道"场景）、网关/代理头/路径规范化、主题资源、密钥并发、配置校验、发布订阅关闭、日志热切换与基准；前端新增 vitest（URL 过滤、主题样式注入过滤、请求超时与错误处理）。
- CI 增加 `go test -race`（Linux）、staticcheck、govulncheck、前端单测与依赖审计。

### 说明
- 管理 API 在 M1（账号与角色）之前使用 `data/secrets/admin.token` 临时令牌。
