# 更新日志 / Changelog

本文件记录所有对用户、站长与插件开发者可见的变化。格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。v0.1 发布后由 release-please 根据 Conventional Commits 自动维护；人写的发布说明见文档站"发布说明"。

## [Unreleased]

### 安全（M4 全量审查，ADR 0034）
- 恢复备份时，所有"目标已有站点"的检查都在写入之前完成；以前被拒绝的恢复已经覆盖了配置和管理员令牌。名为 `secrets/..` 的条目会写到数据目录之外，现在拒绝。
- 已保存的 SMTP 密码只发往它被填写时的服务器：改主机、端口或加密方式时必须重新填写。社交登录的密钥只在提供商类型、issuer、tenant 都没变时保留。
- 开启"需要验证邮箱"后，未验证账号的启动器令牌不能再刷新，也不能通过 Yggdrasil 上传或重置材质（以前可以一直刷新下去）。
- 生成的 systemd 单元拒绝含换行等控制字符的路径与用户名（会注入指令）。
- CDN Webhook 不跟随重定向，失败时不再回显响应内容。

### 修复（M4 全量审查，ADR 0034）
- PostgreSQL 的错误信息不是英文时（装在中文系统上的默认情况），邮箱已被使用、角色名已被占用等冲突会返回 500 而不是对应的提示：改为按 SQLSTATE 错误码识别。
- 一个带未闭合引号、括号或结尾反斜杠的主题变量值会吞掉其后所有变量和暗色模式，现在前后端都拒绝。
- 翻译覆盖用 ICU 解析器校验：格式错误的文字不再让页面显示键名，合法的复数写法不再被误拒；英文没有的文字不再显示无效的英文输入框；保存后继续输入的内容不会被重新加载覆盖。
- 设置页里，存储值带首尾空白的文本、或含编辑器之外语言的多语言值，不再显示为"未保存"，保存也不会丢掉其它语言。
- 首页编辑器保存时保留未知类型区块的属性，丢弃没有链接的按钮。
- MySQL 上两个管理员同时第一次保存带密钥的设置会死锁：设置保存改为先锁配置版本行。
- 积分比率有了上限，过大的比率不会再让上传"花费"溢出成加分。
- 系统状态不再获取迁移锁（另一节点迁移时会卡住），并整体限时；首页统计在数据库变慢时不再让请求排队。首页区块不再随每个页面发送到浏览器，渲染结果按配置版本缓存。
- 备份先复制数据库再列材质；配置写 `sqlite3` 的站点也能恢复；超大或内容与名字不符的材质条目被拒绝，不再截断。
- 只有提供服务的进程写日志文件，CLI 命令不再轮换服务器正在写的文件；启动失败时日志文件句柄被关闭。
- CDN 配置不完整时不再监听玩家事件（以前每个事件都会失败重试约 80 分钟）。
- 系统服务：`ReadWritePaths` 里的 `$` 不再被写成 `$$`；程序在 `/home`、`/root` 下时服务能启动；Windows 服务能读取用户目录里的程序；启动失败的原因写入系统日志。
- Helm：拒绝 `persistence.enabled=false` 搭配 SQLite 或本地材质存储；split 模式的后端挂载可写 `/tmp`，上传较大的材质不再失败（Kustomize 的 split 清单同样修正）；关闭 Pod 的服务环境变量注入；单副本不创建 PDB。
- 发行包 tar.gz 的文件属主为 root；部署冒烟测试与 CI 里几处不会真正失败的检查已修正。
- 文档：Kubernetes 页先建命名空间再建 Secret；发行包页补充升级和恢复后把目录交还给服务用户、命令要加 sudo、数据目录按当前目录解析。

### 新增（M4 部署，ADR 0033）
- `skinloom service install`：一条命令注册为系统服务，开机启动、无需登录、崩溃自动重启。Linux 是带沙箱选项的 systemd 单元（以 `skinloom` 用户运行），macOS 是 launchd 守护进程，Windows 是以 LOCAL SERVICE 运行的 Windows 服务。另有 `uninstall`、`start`、`stop`、`restart`、`status`。
- Helm Chart（`deploy/helm/skinloom`）：单 Pod 或前后端分离、SQLite 或外部数据库（DSN 放在 Secret 里）、Ingress、数据卷、前端 PDB 与 HPA；互相矛盾的参数在安装时报错。
- 配置 `log.file`：同时把日志写到文件。以服务运行时默认写 `data/logs/skinloom.log`，一次性安装链接也在里面。

### 变更（ADR 0033）
- 发行包不再附带 `service/` 目录（手写的 systemd 单元、launchd plist 与 Windows 计划任务脚本），改用 `skinloom service install`。
- `/readyz` 在前端由本进程启动时（web.mode managed / dev）会等前端就绪，避免刚启动时的几秒里探针已通过、页面却返回 502。

### 修复（ADR 0033）
- 在没有 `zip` 的 Windows 上打出的发行包，zip 里的路径分隔符是 `\`（PowerShell `Compress-Archive` 的行为），`unzip` 与 macOS 会解压出错；现在改用系统自带的 `tar.exe`。

### 新增（M4 CDN 刷新与备份，ADR 0032）
- CDN 刷新：角色创建、改名、换装或被删除时，自动让 CDN 丢掉该角色的资料与头像缓存，游戏里立即看到新皮肤。支持 Cloudflare、阿里云 CDN、腾讯云 CDN，其它 CDN 可以用 Webhook 接入（可选 HMAC 签名）。在后台"设置 → CDN 刷新"配置，不用 CDN 时没有任何额外开销。失败的刷新由任务队列重试。
- `POST /api/v1/admin/cdn/purge`：手动刷新指定角色，也可以用来检查 CDN 凭据。
- `skinloom backup`：把 SQLite 数据库（站点运行中也能备份）、配置、密钥和材质文件打包成一个 `.tar.gz`，`--keep N` 只保留最新 N 份，`--no-textures` 跳过材质。MySQL/PostgreSQL 的数据库请用各自的导出工具。
- `skinloom restore <备份文件>`：恢复到一个数据目录。已有站点时需要 `--force`；材质会写入恢复后配置的存储，本地与 S3 可以互相恢复。

### 新增（M4 翻译覆盖与系统状态，ADR 0031）
- 后台"翻译"页（`/admin/translations`）：按键名或文字搜索界面上的任何文字，分语言改写，保存后所有页面立即生效（BS 的 `language_lines`）。改写的文字必须保留原文的占位符。设置键为 `i18n.overrides`。
- 后台"系统状态"页（`/admin/status`）与接口 `GET /api/v1/admin/status`：版本、运行环境、数据库类型与版本、迁移、材质存储、后台任务积压与失败、邮件与站点地址，并列出需要处理的问题。不显示数据库连接串或用户名（BS 会显示数据库用户名）。

### 新增（M4 外观，ADR 0030）
- 后台"外观"页（`/admin/appearance`）：一键切换主题；在主题之上调整亮色、暗色的常用颜色与圆角；编辑首页区块（添加、排序、删除、改属性），可以随时改回主题自带的首页；设置 Logo、浏览器图标、页脚文字和搜索关键词。
- 第二个内置主题"织锦"：暖色配色、分栏的登录页、更紧凑的皮肤库，首页展示最新材质与站点数据。
- 新的首页区块：站点数据、最新材质、热门材质、文字（Markdown）、图片、按钮组。区块的文字可以按语言分别填写。
- 主题可以选择登录页的布局（卡片或分栏）与皮肤库的疏密，并设置 3D 预览的背景。
- 自定义 CSS（`theme.custom_css`，BS 的 `custom_css`）：只有超级管理员能修改，作为单独的样式表加载在主题样式之后。
- 新接口 `GET /api/v1/site/stats`（站点数据，缓存一分钟）与 `GET /api/v1/admin/blocks`（首页区块的种类与属性）。

### 变更（ADR 0030）
- `home.blocks` 保存时按区块目录检查：未知的区块或属性、不安全的链接、超出范围的数量会被拒绝。`theme.overrides` 里含 `; { } < >` 或注释的值，以前保存后渲染时被悄悄丢掉，现在保存时直接拒绝。
- 默认主题清单的 `layout` 只保留实际支持的 `auth` 与 `skinlib`。主题清单里的首页区块与 token 在加载时检查。

### 新增（M4 设置中心，ADR 0029）
- 后台"设置"页（`/admin/settings`）：所有运行时设置按分组排列，每组单独保存、立即生效。选项是下拉框，数字带上下限，多语言设置每种语言一个输入框；填错时直接提示，不会保存。主题与首页区块在后续的外观页面里设置。
- 新设置 `auth.require_email_verification`（BS 的 `require_verification`，默认关闭）：开启后，没有验证邮箱的账号不能管理角色、上传或修改材质、举报、创建 OAuth 应用与个人访问令牌，也不能在启动器登录。页头会告诉这些用户被限制了什么。

### 安全
- 设置接口不再返回 SMTP 密码、验证码私钥和第三方登录提供方的 `clientSecret`：密钥只写不读，后台只显示"已设置"；提供方里已保存的 `clientSecret` 显示为 `********`，原样保存就沿用原来的。两位管理员同时保存时，新密钥不会被另一位的编辑改回旧值。

### 变更
- 设置的可选值与数值范围改为在定义里声明，管理接口随设置一起返回（`enum`、`min`、`max`、`secret`），校验信息统一为 `want one of [...]`、`must be X to Y`、`must be X or more`。

### 安全（全量审查）
- 登录、启动器（Yggdrasil）登录与个人资料页的密码确认，可以用同一时刻发出的一批请求绕过失败次数限制：先检查、做完密码哈希才计数，200 个并发请求全部被校验（限额 10）。现在检查和计数一步完成，成功的登录不计入；找回密码、重发验证邮件、注册、上传、举报、BS 资源 API、第三方登录发起、验证码图片的限流也改为同样的方式。

### 修复（并发，ADR 0027）
- 同时发出几次"改为私有"会重复扣差价（6 个请求扣 6 次）；改类型同理按旧类型判断。材质的编辑、收藏、管理员放入衣柜、删除、设为头像、提交举报，现在都在锁住材质之后再判断可见性、上传者和类型。
- 收藏与上传者改为私有同时发生时，别人的私有材质可能进入自己的衣柜。
- 给角色穿上材质与把它移出衣柜同时发生时，角色会穿着不在衣柜里的材质（之后材质改为私有，它的 hash 就会经 `/{name}.json` 暴露）。穿戴与管理员转移角色现在先锁住材质再检查衣柜。
- 删除角色与移出它穿着的材质同时发生时会死锁（PostgreSQL / MySQL）：删除角色现在先锁主人。
- 删除用户与他人收藏该用户的材质同时发生时会死锁，收藏还可能返回 500。删号前的清理挪进删号事务，按"材质 → 用户"的顺序加锁；注销清理时账号若已恢复，清理一并回滚。
- 两位管理员同时保存有重叠键的设置会死锁：设置按键名顺序写入。
- 处理举报时，若举报人的账号刚被删除（例如处理方式是封禁、而举报人在此期间被删），会返回 500；现在在加锁之后重读举报。
- 删除一张被超过 3 万（SQLite）或 6.5 万（PostgreSQL）个角色穿着的材质会失败（单条语句参数过多）；现在不再列出所有角色 id。

### 修复（数据）
- 删除用户后，他收藏过的材质的被收藏数没有减少。
- 服务每天校准一次被收藏数（按衣柜条数逐个重算），兜住极少数仍可能漂移的情况和导入的数据。
- 删除材质后，上传者自己角色的 `Last-Modified` 不变，`/{name}.json` 一直回 304 和已删除的材质。
- SQLite 的 `database.dsn` 带参数时（如 `skinloom.db?mode=rwc`）外键被关闭，级联删除与置空全部失效。现在保留自己写的参数，补上缺少的 pragma，外键始终开启。

### 变更（ADR 0028）
- 领域事件：用户、角色、材质、衣柜、举报共 15 个事件（`user.registered`、`player.updated`、`texture.deleted`……），在变更的事务里写进任务队列，提交后投递，至少一次。没有监听者时不写入；供第二阶段的插件与 M4 的 CDN 刷新使用。
- 管理操作的审计记录与变更在同一事务里写入，不会再出现"操作成功、审计丢失"。管理员同时改名和转移角色改为一个事务。命令行 `skinloom settings set` 也写审计（`meta.via = "cli"`）。

### 修复（并发与死锁）
- 统一事务的加锁顺序：先锁材质，再按 id 从小到大锁住所有会改积分的用户，然后才写入（ADR 0026）。修复两个在 PostgreSQL 上同样会发生的死锁：两个人同时收藏对方的材质；两位管理员同时处理同一材质的几条举报。处理举报时，别人刚处理过的同材质举报会跳过，不再让整次删除失败。

### 修复（MySQL）
- 同一用户同时提交举报、创建角色、上传材质，或多人同时收藏同一材质时，MySQL 会报死锁（Error 1213）：插入引用用户的行时，外键检查会给用户行加共享锁，两个事务随后都要排他锁去扣积分。现在先锁住要改的行（材质、用户），并发请求排队执行。PostgreSQL 与 SQLite 不受影响。
- MySQL 把超出微秒的时间四舍五入后存进 `datetime(6)`，过期时间可能比算出的晚不到 1 微秒，令牌在到期那一刻仍然有效。连接 MySQL 时改为先截断到微秒再发送，与 PostgreSQL 一致，不依赖 MySQL 版本或 `sql_mode`。

### 安全与正确性（M3 收尾检查）
- 应用主人被封禁或注销后，应用已经签发给其他用户的访问令牌也立即失效（原来要等令牌过期，最长 1 天）；解封后恢复。
- 兼容 BS 的资源 API 只为自己的路径回应跨域预检。原来 `/api/*` 的通配规则也会给 `/api/v1` 的预检返回 `Access-Control-Allow-Origin: *`，浏览器仍会拒绝（不允许 `X-SkinLoom-Request` 头、不带 Cookie），但 CSRF 防护只剩一层；新增回归测试。
- Yggdrasil 元数据的 `skinDomains` 只在没有设置 `site.url` 时才使用请求的地址，不再把请求里的 `Host` 加进去。
- `/oauth/token` 的内部错误写入日志，不再只返回 `server_error`。
- 应用和个人访问令牌的名称按字符计（最多 64 个），22 个汉字以上的名称不再返回 500。
- 兼容 BS 的分页接口对极大的 `page` 不再溢出（原来会回到第一页，PostgreSQL 上报错）。
- 没有密码的账号不能解除最后一个仍在配置里的提供方的绑定（已删除的提供方的绑定不算登录方式）；两个同时的解绑不会把绑定全部删掉。
- 第三方登录的 `next` 过长时忽略它，而不是让登录失败；`/api/admin/reports` 的筛选值无效（如 `status:9`）时返回空列表，不再返回全部举报。

### 新增（M3 外置登录与开放平台 · 第四部分：第三方登录）
- **第三方登录**：用 GitHub、Microsoft 或任何 OpenID Connect 提供方（Google、Keycloak、Authentik 等）登录，不需要插件。提供方写在新设置 `auth.social.providers` 里（设置 `site.url` 后生效），回调地址是 `<site.url>/api/v1/auth/social/<id>/callback`。登录页和注册页显示"使用 … 登录"按钮。
- **第一次登录**：已绑定的直接登录；提供方和本站都验证过的同一邮箱自动绑定（从 BS 迁移过来的第三方登录用户这样找回账号）；其余进入 `/auth/social`，创建新账号（邮箱、昵称预填）或"登录并绑定"已有账号。
- **已绑定的账号**：个人资料页可以查看、绑定其它提供方、解除绑定。通过第三方登录创建的账号没有密码，可以在个人资料页设置第一个密码。
- 接口 `POST /api/v1/auth/social/{id}/start`、`GET /api/v1/auth/social/{id}/callback`、`GET /api/v1/auth/social/pending`、`POST /api/v1/auth/social/pending/register`、`POST /api/v1/auth/social/pending/link`、`GET /api/v1/me/social`、`POST/DELETE /api/v1/me/social/{id}`；`UserView` 增加 `hasPassword`，站点 bootstrap 增加 `auth.social`。数据库新增 `social_accounts`、`social_flows` 表（迁移 `20261030040409_social`）。文档站新增"第三方登录"页面；ADR 0025。

### 安全与正确性（M3 第四部分）
- 只有提供方和本站**都**验证过的邮箱才会自动绑定（BS 只按邮箱认人：先用别人的邮箱注册、或用可随意填写邮箱的 Microsoft 工作账号，都能登录别人的账号）。GitHub 只认已验证的主邮箱，Microsoft 只认个人账号或已验证域名的邮箱（防 nOAuth）。
- 每次登录都带 PKCE 和 state（与浏览器 cookie 绑定、只能用一次），OpenID Connect 还校验 nonce、issuer、audience 与有效期；提供方地址必须是 https，回调地址只用 `site.url` 生成。
- 绑定时浏览器里已经换了账号，绑定作废；一个第三方账号只能绑定一个本站账号。
- 没有密码的账号不能用密码或启动器登录，最后一个绑定不能解除；新账号同样受 `auth.registration` 与 `auth.registrations_per_ip` 限制。绑定、解绑写入审计日志，登录和注册记录登录方式。

### 新增（M3 外置登录与开放平台 · 第三部分：兼容 BS 的管理类接口）
- **管理类资源 API**：`/api/admin/users`（列表、改邮箱、切换邮箱验证、改昵称、改密码、改积分、改权限、删除）、`/api/admin/players`（列表、改名、转移、换材质、删除）、`/api/admin/closet/{uid}`（查看、放入、取出）、`/api/admin/reports`（列表、处理）、`POST /api/admin/notifications`，与 Blessing Skin 的路径和格式相同，需要管理员授予的管理类 scope（应用令牌或个人访问令牌）。至此 BS 的 OAuth 资源 API 全部可用。
- 列表支持 `page` 翻页和 `q` 搜索（普通文字，或 `uid:N`、`status:0` 这样的第一个条件）。ADR 0024。

### 安全与正确性（M3 第三部分）
- 管理类接口与后台页面走同一套服务和权限规则：管理员只能管理普通用户，谁都不能改自己的权限；操作写入同样的审计记录（修 BS A01：管理端与 OAuth API 各写一份、行为不一致）。
- 给角色换材质要求材质在主人的衣柜里、类型正确（修 C07）；发通知返回结果而不是跳转到 `/admin`（修 C15）。
- 令牌主人不再是管理员时，令牌立即不能再调用管理类接口；封禁用户会吊销他的全部令牌。

### 新增（M3 外置登录与开放平台 · 第二部分：OAuth 2.0、个人访问令牌与兼容 API）
- **OAuth 2.0 提供方**：其它网站和程序可以"用本站账号登录"。授权码流程（`/oauth/authorize`、`/oauth/token`），支持 PKCE、刷新令牌、吊销（`/oauth/revoke`）与元数据（`/.well-known/oauth-authorization-server`）；与 Blessing Skin（Laravel Passport）的客户端兼容，scope 名称相同。
- **授权确认页** `/user/oauth/authorize`：显示应用、创建者和它要的每项权限；已经允许过的应用不再询问。
- **应用与令牌** `/user/oauth/manage`：
  - 注册应用（服务端应用有密钥，只显示一次，可以重置；浏览器 / 桌面 / 手机应用无密钥、必须用 PKCE），修改名称和回调地址，删除；
  - 查看和撤销已授权的应用；
  - 创建个人访问令牌（选权限和有效期），给自己的脚本调用接口，显示最近使用时间。
  - 接口 `GET/POST /api/v1/me/oauth/clients`、`PATCH/DELETE /api/v1/me/oauth/clients/{id}`、`POST /api/v1/me/oauth/clients/{id}/secret`、`GET /api/v1/me/oauth/grants`、`DELETE /api/v1/me/oauth/grants/{id}`、`GET/POST /api/v1/me/tokens`、`DELETE /api/v1/me/tokens/{id}`、`GET /api/v1/oauth/scopes`、`GET/POST /api/v1/oauth/authorize`。
- **兼容 Blessing Skin 的资源 API**：`/api`、`/api/user`、`/api/user/notifications`、`/api/players`、`/api/closet` 及其读写操作，响应格式与 BS 相同，应用的令牌和个人访问令牌都能用。管理类接口（`/api/admin/*`）将在第三部分提供。
- 用户中心增加"应用与令牌"入口。新设置 `oauth.access_token_ttl_seconds`（默认 1 天）、`oauth.refresh_token_ttl_seconds`（默认 30 天）；数据库新增 `oauth_clients`、`oauth_grants`、`oauth_codes`、`oauth_tokens` 表（迁移 `20261029155749_oauth`）。文档站新增"开放平台"页面；ADR 0023。

### 安全与正确性（M3 第二部分）
- 应用密钥、授权码、访问与刷新令牌、个人访问令牌都只存哈希（BS 明文保存并一直显示应用密钥）。令牌带前缀（`slo_`、`slr_`、`slp_`），便于密钥扫描工具发现泄露。
- 衣柜写权限 `Closet.ReadWrite` 现在可以授予，旧的拼写 `Closet.ReadWrtie` 也接受（修 C13：BS 的第三方应用永远拿不到）。
- 改密码、找回密码、封禁、注销、"退出其它所有设备"时，应用的令牌和个人访问令牌一并失效（03 S02）；撤销授权或删除应用会删掉它的全部令牌。
- 回调地址必须与登记的完全一致；`client_id` 或回调地址不对时错误显示在本站，不跳转。授权码只能用一次，同时兑换也只有一个成功；没通过检查的授权码同样作废。刷新令牌每次使用后更换。
- 公开客户端必须使用 PKCE。管理类权限只有管理员能授予，使用时还要求令牌主人仍是管理员。应用主人被封禁或注销后应用停用。
- 资源 API：读不存在或别人的通知返回 404（修 C15：BS 500）；衣柜每页最多 100 条；每个用户每分钟最多 60 次请求。
- 访问令牌默认 1 天有效（BS 一年），需要时可以调长。

### 新增（M3 外置登录与开放平台 · 第一部分：Yggdrasil）
- **外置登录**：内置 authlib-injector 的 Yggdrasil API（`/api/yggdrasil`），与 Blessing Skin 的 yggdrasil-api 插件协议相同。HMCL、PCL2、BakaXL 等启动器添加外置登录后，用站点邮箱或角色名和密码登录、进服，游戏服务器用 authlib-injector 校验，皮肤由站点提供；也可以在启动器里直接上传皮肤和披风。
- **启动器引导**：首页"启动器"区块与个人资料页给出接口地址、复制按钮和可以直接拖进启动器的链接；所有页面带 `X-Authlib-Injector-API-Location` 头，启动器里只填站点地址也行。
- **游戏启动器管理**：个人资料页显示有几个启动器登录着账号，可以一键全部退出。接口 `GET /api/v1/me/yggdrasil`、`POST /api/v1/me/yggdrasil/signout`。
- 新设置 `yggdrasil.token_valid_seconds`、`yggdrasil.token_refresh_seconds`、`yggdrasil.tokens_per_user`、`yggdrasil.search_profile_max`、`yggdrasil.skin_domains`、`yggdrasil.enable_ali`；数据库新增 `site_secrets`、`ygg_tokens`、`ygg_sessions` 表（迁移 `20261029123138_yggdrasil`）。文档站新增"外置登录"页面；ADR 0022。

### 安全与正确性（M3 第一部分）
- 启动器令牌存在数据库里（只存哈希），重启、升级、清缓存都不会让玩家掉线（修插件 Y01）；并发登录不会丢令牌，每人最多保留 `yggdrasil.tokens_per_user` 个（Y08）。
- 令牌按用户 ID 归属：邮箱含大写字母时刷新出的令牌能正常使用（修插件 #212），用角色名或任意大小写的邮箱 signout 都会吊销全部令牌（Y02）。令牌过期时间按服务器记录检查。
- 用角色名登录会绑定该角色（修 #123）；改名不影响已登录的启动器（#231）；返回的角色名是库内写法（#232）。
- 修改或重置密码、封禁、注销账号、退出其它设备时，启动器一并退出（03 S02）。管理员把角色转给别人后，旧主人的启动器不能再用它进服。
- 登录限流不再只按请求里的用户名计：按地址、地址 + 账号、账号三个维度，单一来源刷不掉别人的登录（修 Y04）。被封禁的账号得到明确的提示。
- 材质上传接口只接受角色主人的令牌（插件接受任何人的有效令牌），照常收积分、校验尺寸。
- join 之后角色被删或主人被封禁时 hasJoined 返回 204（修 Y05：插件 500）；批量查询角色名只用一次查询，数量受 `yggdrasil.search_profile_max` 限制。
- 签名私钥（RSA 4096）与令牌密钥保存在数据库，多个实例共用，启动时加载一次（修 Y06、Y09）；签名结果缓存，hasJoined 不必每次做 RSA 运算。

### 新增（M2 材质与角色 · 第五部分：渲染器、站内通知与 S3 存储；M2 完成）
- **头像与预览图**：`/avatar/player/{角色名}`、`/avatar/user/{用户 ID}`、`/avatar/{材质编号}`、`/avatar/hash/{哈希}`、`/preview/{材质编号}`、`/preview/hash/{哈希}`，地址与 Blessing Skin 相同。头像支持 `size` 和立体的 `3d`，预览图是皮肤正背面并排或披风；默认无损 WebP，`?png` 或客户端不支持时输出 PNG。像素保持清晰（BS 会插值模糊），旧版 64×32 皮肤按 Minecraft 的方式补出左手左腿。
- **头像**：衣柜里的皮肤可以"设为头像"，页头与个人资料页显示；个人资料页可以移除。接口 `PUT /api/v1/me/avatar`。
- **站内通知**：页头铃铛显示未读数，`/user/notifications` 查看，点开即已读、可全部标为已读；正文支持 Markdown。管理员在后台概览发送给所有用户、普通用户或某一个用户（按 ID 或邮箱）。接口 `GET /api/v1/me/notifications`、`GET /api/v1/me/notifications/unread`、`POST /api/v1/me/notifications/{id}/read`、`POST /api/v1/me/notifications/read-all`、`POST /api/v1/admin/notifications`。
- **公告按 Markdown 显示**：首页与用户中心的站点公告渲染为 HTML（站点信息与用户中心接口新增 `announcementHtml`）。
- **S3 兼容存储**：`[storage] driver = "s3"`，支持 Amazon S3、阿里云 OSS、腾讯云 COS、七牛、Cloudflare R2、MinIO 等；可设置 `public_url`，让材质文件直接由 CDN 发出。
- **链接预览**：公开材质页带 `og:image`（预览图），需要设置 `site.url`。
- 数据库新增 `announcements`、`notifications` 表，`users` 新增 `avatar_texture_id`、`announcements_seen`（迁移 `20261024112658_notifications_avatars`）。ADR 0021。至此 M2（材质与角色）全部完成。

### 安全与正确性（M2 第五部分）
- 头像与预览图的尺寸取固定档位，缓存键包含所有参数（修 Blessing Skin C14：预览图缓存键漏了高度）；取决于访问者的响应（私有材质）标记为 `private`，不会被 CDN 缓存后发给别人。
- 头像所用的皮肤之后变成别人的私有材质时，头像显示灰色占位图，不泄露材质。
- Markdown 在服务端渲染：原始 HTML 不输出，`javascript:` 链接被丢弃。
- 读不存在或别人的通知返回 404（修 C15：BS 直接 500）。群发只写一条公告，不再给每个用户各写一行。
- 防止 SQLite 迁移误删数据：Atlas 为 SQLite 生成的"重建表"迁移在事务中执行时会级联删除子表数据，本次改为 `ADD COLUMN`，并新增测试阻止以后出现此类迁移。
- 依赖：`golang.org/x/net` 升到 v0.62.0（GO-2026-6866、GO-2026-6860；代码未调用到受影响的部分）。

### 新增（M2 材质与角色 · 第四部分：举报、后台概览与后台角色管理）
- **举报**：材质页的"举报"填写理由后提交给管理员（`score.reporter_modification` 为负数时收取举报费、成立后退还，为正数时直接奖励）。在用户中心的"我的举报" `/user/reports` 查看处理结果。接口 `POST /api/v1/textures/{id}/reports`、`GET /api/v1/me/reports`。
- **后台举报管理** `/admin/reports`：按状态查看，删除材质、封禁上传者或驳回；举报成立时举报者获得 `score.reporter_reward`。接口 `GET /api/v1/admin/reports`、`POST /api/v1/admin/reports/{id}/review`。
- **后台概览** `/admin`：用户、角色、材质、衣柜条目、存储、待处理举报的总数，以及最近 31 天每天的注册与上传柱状图（可切换为表格）。接口 `GET /api/v1/admin/overview`。
- **后台角色管理** `/admin/players`：按角色名开头、UUID 或 ID 搜索，查看某个用户的全部角色（用户管理里每行有入口）；改名、转移给其他用户、换皮肤与披风、删除。接口 `GET /api/v1/admin/players`、`PATCH/DELETE /api/v1/admin/players/{id}`、`PUT /api/v1/admin/players/{id}/textures`。
- **后台衣柜管理接口**：`GET/POST /api/v1/admin/users/{id}/closet`、`DELETE /api/v1/admin/users/{id}/closet/{textureId}`，管理员放进去的材质免费。
- 后台导航增加"概览""角色""举报"，页头的管理员徽章进入概览。新设置 `score.reporter_modification`、`score.reporter_reward`；数据库新增 `reports` 表与 `users`、`textures` 的 `created_at` 索引（迁移 `20261024100910_reports`）。ADR 0020。

### 安全与正确性（M2 第四部分）
- 修复：站点把衣柜条目改为免费后，同一用户反复收藏、取消同一个材质，每次都会再退还一次早已退过的旧收藏费（被收藏奖励同理会被重复收回）。现在每个衣柜条目只结算它自己的费用。
- 举报只能针对自己看得见的材质，不能举报自己的；同一材质只能举报一次，同时提交多次也只算一次；每人每小时最多 20 次。
- 举报处理"删除材质"走统一的删除流程，与结案在同一个事务里：收藏者退费，同一文件的其它材质不受影响（修 Blessing Skin C06）。同一材质的其它待处理举报一并结案，只有被处理的那一条获得奖励。
- 两名管理员同时处理同一条举报，只有一个生效；封禁上传者遵守"管理员只能管理普通用户"的规则，没有权限时举报保持待处理。
- 后台给角色换材质同样校验类型、只能用主人衣柜里的材质（修 C07）；转移角色后，新主人衣柜里没有的材质自动脱下。
- 后台概览在数据库里计数，日界按站点时区计算，不再把一个月的记录读进内存。

### 新增（M2 材质与角色 · 第三部分：收藏、材质编辑、签到与用户中心）
- **收藏**：材质页的"收藏"把材质加入衣柜（消耗 `score.per_closet_item`，上传者得 `score.award_per_like`），"取消收藏"移出并退还。我的衣柜里可以给材质改一个自己的名字，或移出衣柜（显示退还的积分）。接口 `POST /api/v1/me/closet`、`PATCH/DELETE /api/v1/me/closet/{textureId}`。
- **管理材质**（上传者与管理员，在材质页）：改名、改类型、设为公开 / 私有（提前显示要付或退还的积分）、删除。接口 `PATCH/DELETE /api/v1/textures/{id}`；管理员的修改与删除记入审计日志。
- **用户中心** `/user`：积分、角色数、衣柜数、上传数与占用空间、公告，以及常用入口；页头增加"用户中心"入口。接口 `GET /api/v1/me/dashboard`。
- **签到**：在用户中心签到，随机获得 `score.sign_min`–`score.sign_max` 积分（默认 10–100）；每 `score.sign_gap_hours` 小时一次（默认 24），或开启 `score.sign_daily` 后每天零点刷新（按新设置 `site.timezone`，默认 `Asia/Shanghai`）。接口 `POST /api/v1/me/sign`。
- 新设置 `score.award_per_like`、`score.sign_min`、`score.sign_max`、`score.sign_gap_hours`、`score.sign_daily`、`site.timezone`；`score.return_on_delete` 现在也控制移出衣柜和删除材质时的退还。数据库新增 `users.last_sign_at`（迁移 `20261024081729_sign_in`）。ADR 0019。

### 安全与正确性（M2 第三部分）
- 签到是一条带条件的原子更新，同时点多次只算一次（修 Blessing Skin C03）；收藏数原子增减（C04）。
- 取消收藏、删除材质时的退还与收回奖励都按积分流水原样冲正，不按现在的价格重算；收藏自己的材质不再在取消时被扣分，上传者已注销也不会出错（修 C05）。
- 公开 ↔ 私有切换对称计费：设为私有付差额，设为公开退还同样的差额，来回切换不再持续丢分（修 C12）。
- 改类型会按图片尺寸重新校验，皮肤与披风之间切换时，正在穿它的角色会自动脱下（修 C08）。设为私有时，它会从别人的衣柜和角色上移除，并退还他们的积分。
- 材质文件只在没有任何材质引用、且 1 小时宽限期内没有被重新写入时才删除，与重新上传同一张图并发也不会误删；每天清理一次没有记录的孤儿文件。访问页面从不删除数据（修 C06、C10）。
- 用户被永久删除前，先删除他的私有材质（其他人本就看不到）；公开材质保留，上传者显示为"已注销用户"（修 C09）。

### 新增（M2 材质与角色 · 第二部分：角色与游戏接口）
- **角色** `/user/player`：创建（消耗 `score.per_player`，默认 100 积分）、改名、删除（`score.return_on_delete` 开启时退还当时支付的积分）、从衣柜里挑选皮肤和披风、脱下、复制 UUID。材质页可以把衣柜里的材质直接穿到角色上；页头增加"角色"入口。接口 `GET/POST /api/v1/me/players`、`PATCH/DELETE /api/v1/me/players/{id}`、`PUT /api/v1/me/players/{id}/textures`、`GET /api/v1/players/rules`。
- **游戏皮肤接口**：CustomSkinLoader `/{角色名}.json` 与 `/csl/{角色名}.json`、UniSkinMod `/usm/{角色名}.json`、Legacy `/skin/{角色名}.png` 与 `/cape/{角色名}.png`，格式与 Blessing Skin 及其插件一致。
- **UUID**：每个角色创建时就分配 UUID（`yggdrasil.uuid_algorithm`：`v3` 与离线服一致，默认；`v4` 随机），供 M3 的外置登录使用。
- 新设置 `player.name_rule`、`player.name_regexp`、`player.name_length_min`、`player.name_length_max`、`yggdrasil.uuid_algorithm`、`delivery.profile_cache_seconds`、`delivery.missing_player_status`、`score.per_player`、`score.return_on_delete`；数据库新增 `players` 表（迁移 `20261024075025_players`）。文档站新增"角色与游戏客户端"页面（含 CustomSkinLoader 配置示例）；ADR 0018。

### 安全与正确性（M2 第二部分）
- 角色名由唯一索引判重，不区分大小写：同时抢注同一个名字只有一个人成功，也不会再出现只是大小写不同的两个角色（修 Blessing Skin C01）；按名字查找走索引（P01）。
- 皮肤只能穿皮肤、披风只能穿披风（修 C07）；只能穿自己衣柜里的材质。
- 删除角色退还的是当时实际支付的积分（从积分流水冲正），不按现在的价格重算。
- 名字不能包含空格和 `/ \ ? # %`，任何规则下都能拼出可用的皮肤地址。
- 游戏接口按名字查找不区分大小写并返回库内写法（修 BS #232）；角色主人被封禁返回 403，已注销视为不存在；默认缓存 60 秒而不是一年。

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
