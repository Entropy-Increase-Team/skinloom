# 0039 安全自查：内容安全策略与自定义 JavaScript，以及 ADR 0034 的遗留问题

- 状态：已接受（2026-11-05）
- 相关：设计文档 02 F35；03 S16；04 §8；ADR 0028、0030、0032、0034、0035

## 背景
- ADR 0030 把自定义 JavaScript 推迟到"和 CSP 一起做"：Next.js 的每个页面都要带 nonce，自定义脚本才能被允许，而注入的脚本不行（03 S16：BS 把自定义 JS/CSS 直接塞进页面，管理员账号被盗就等于全站存储型 XSS）。
- ADR 0034 记下了四个没有解决的问题：
  - 恢复时不检查站点是否已停止；
  - Windows 上备份文件与日志的权限继承父目录；
  - 封禁或删除用户不会触发 CDN 刷新；
  - 服务器端的翻译覆盖校验只检查花括号。
- 后端的安全响应头只有 `X-Content-Type-Options`、`Referrer-Policy`、`X-Frame-Options`。

## 决策
- **页面的 CSP 由 Next.js 的 proxy 生成**（`web/apps/web/src/proxy.ts`，策略本身在 `src/lib/csp.ts`）：
  - 每个页面请求生成一个 128 位的随机 nonce，写进请求和响应的 `Content-Security-Policy`。Next.js 从请求头里读出 nonce，加到自己的脚本上。所有页面本来就是按请求渲染的（布局用访客的 Cookie 取 bootstrap），没有缺少 nonce 的预渲染页面。
  - `script-src 'self' 'nonce-…' 'strict-dynamic'`：只有带 nonce 的脚本，以及由它们加载的脚本能运行。注入的 `<script>`、`onerror=` 之类的内联事件都不运行。验证码脚本由我们的代码动态加载，所以不用列出它们的域名。开发模式加 `'unsafe-eval'`（React 用它还原服务器端的错误堆栈）。
  - `style-src 'self' 'unsafe-inline' https:`、`img-src 'self' data: blob: https: http:`、`font-src 'self' data: https:`：主题和自定义 CSS 可以用任何地方的字体和图片，React 到处用行内样式；这些都执行不了代码。
  - `connect-src 'self' https:`：自定义 JavaScript 加载的统计代码用 `fetch` 或 `sendBeacon` 把数据发往它们的服务器（GA4、Umami、Plausible 等），hCaptcha 也从页面里请求它的服务器。图片本来就可以发往任意 HTTPS 地址，放开它并不多泄露什么。`document.write` 写出的脚本和 `eval` 仍然不能运行，外观文档写明了；`frame-src` 只有三家验证码服务（Turnstile、reCAPTCHA 的 recaptcha.net 与 google.com、hCaptcha）。
  - `object-src 'none'`、`base-uri 'self'`、`form-action 'self'`、`frame-ancestors 'self'`（与 `X-Frame-Options: SAMEORIGIN` 一致）。OAuth 授权和社交登录离开本站用的是 `location.assign`，不是表单提交；如果用表单，Chromium 会用 `form-action` 拦下表单之后的重定向。
  - 经由 HTTPS 访问时（网关根据受信任的代理设置 `X-Forwarded-Proto`）加 `upgrade-insecure-requests`。
  - next-themes 用一段行内脚本在渲染前选亮色或暗色，布局把 nonce 传给它。
- **后端自己的回答带一个什么都不允许的策略**：JSON、图片、材质文件、重定向都不是页面，所以是 `default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox`。行内样式是浏览器单独打开一张图片时自己用的。
  - API 参考 `/api/docs` 例外：它从 CDN 加载查看器，Huma 给它配了自己的 CSP。两条策略会同时生效，所以这里不再加后端的那条。
  - 前端没有启动时的占位页加 `default-src 'none'; style-src 'unsafe-inline'`。
- **自定义 JavaScript**（`theme.custom_js`，最多 64 KB）与自定义 CSS 的做法相同：
  - 只有超级管理员、管理员令牌和命令行能改；
  - 不放进页面，作为单独的文件 `/themes-assets/_custom/<内容哈希>.js` 提供（`text/javascript`、`nosniff`、可以永久缓存）；
  - bootstrap 的 `theme.js` 列出它，布局用带 nonce 的 `<script defer>` 加载。它可以再加载别的脚本（统计代码等）。
- **自定义 JavaScript 不排除后台页面。** 站点是单页应用，从首页进入后台时脚本已经在运行，只在直接打开后台时不加载并不能真正保护后台。写坏了就用 `skinloom settings set theme.custom_js null` 清空，帮助文字和文档都写明了这条命令。
- **BS 的 `custom_js` 现在导入**（以前丢弃），报告里警告运行方式不同：BS 每个页面都运行一次，SkinLoom 只在打开网站时运行一次。
- **恢复检查站点是否在运行**：
  - 运行中的站点对 `data/skinloom.lock` 持有共享锁，几个角色共用同一个数据目录时互不影响；
  - `skinloom restore` 不等待地申请独占锁，拿不到就拒绝，`--force` 也一样，并且在写入任何文件之前；
  - 站点启动时如果恢复正在进行，启动失败；
  - Unix 用 `flock`，Windows 用 `LockFileEx`。锁属于进程，进程崩溃时由操作系统释放，残留的锁文件不会挡住任何操作。
  - 卷不支持 `flock`（部分 CIFS、FUSE、网络存储卷返回 ENOLCK 或 EOPNOTSUPP）时按没有锁处理，与不支持文件锁的系统一样：这样的数据目录以前能用，不能因为这把锁就启动不了。
- **Windows 上的私密文件**：以下几处设为只允许运行 SkinLoom 的账户、SYSTEM 和 Administrators 访问，并且不再继承上级目录的权限。目录里之后新建的文件也继承这个设置。
  - `data/secrets`；
  - `data/backups`：里面有备份文件和备份时的 SQLite 临时副本；
  - 备份文件本身，因为可能写到别的目录；
  - 日志文件，里面有一次性的安装链接；
  - `skinloom import bs` 的报告，里面有邮箱和角色名（第二轮审查补上）。

  Unix 上照旧用权限位（0600、0700）。以服务账户运行时，查看日志和运行命令要用管理员终端。

  以下几种情况保持原样，不报错：
  - **已经不再继承的**（不管是谁设的）。`skinloom service install` 用 `icacls /T` 给 LOCAL SERVICE 加了明确的权限；如果管理员终端里运行的 `backup`、`admin token` 每次都重设一遍，就会把服务账户去掉，服务下次启动就读不到密钥。
  - **当前账户无权修改权限的**。例如先用 `start.bat` 试运行、再装成服务时，`data\secrets` 属于原来的账户，服务账户只有修改权限，没有改权限的权利。以前这会让服务启动失败。
  - **不支持权限的卷**（FAT）。
- **账号状态变化时通知它的每个角色**。`player.updated` 的定义本来就包括"主人变了，游戏客户端得到的东西不同了"，但以前没有地方发布。现在：
  - 封禁、解封、用户注销（进入宽限期，此后返回 404）、恢复（登录或管理员恢复）时，在同一个事务里为该账号的每个角色发布 `player.updated`；
  - 管理员删除账号、宽限期结束清除账号时，为每个角色发布 `player.deleted`。

  CDN 刷新本来就监听这两个事件，所以封禁之后 CDN 不会继续提供缓存的皮肤，解封之后也不会继续返回缓存的 403。没有人监听时不查询角色。新增的读取在同一事务里写 users 之后进行，符合 ADR 0026 的加锁顺序；清除时被恢复的账号整个回滚，事件也随之撤回。
- **服务器端按 ICU 语法校验翻译覆盖**（`settings.checkMessage`）：
  - 按 `@formatjs/icu-messageformat-parser` 的语法规则实现：占位符、number、date、time 的样式和骨架、plural、selectordinal、select（必须有 `other`）、标签、撇号转义。
  - 测试语料由 formatjs 自己判定：一百多条手写用例加两千多条随机字符串，Go 的判断必须与它完全一致。语料由 `internal/settings/testdata/icu-corpus.mjs` 用 bun 生成，formatjs 升级后重新生成。前端发布的全部文字也必须通过。
  - 是否保留默认文字的占位符，只能由有默认文字的翻译页检查，服务器不检查。
  - BS 导入器把带 `<`、`>` 的称呼当作非纯文本丢弃（与花括号一样），一条坏文字不会让整个覆盖设置都导入失败。

## 审查后的修正
同一轮的全量审查找出几处问题，都已修正，并留有回归测试：
- 上面私密文件的两种"保持原样"、`flock` 不受支持时按没有锁处理，以及 `connect-src` 放开到 HTTPS（统计代码要用），都是审查后改的。
- `skinloom import compare` 的第一行比较两站的签名公钥（ADR 0037）。
- BS 导入器：
  - `custom` 角色名规则配空正则，与 BS 一样表示任何名字都可以，以前会变成官方规则并警告；
  - SMTP 的加密方式只按端口决定（ADR 0036），`ssl` 配 587 以前被当成直接 TLS，连不上。
- `bench/run.sh` 在服务器退出或两分钟内没有就绪时报错退出，`Load test` 工作流限时 90 分钟。
- 用一个真实 BS 6.0.2 站点的数据库备份试导入（105 个用户，没有材质文件）时又发现两处：
  - 材质文件缺失时，头像和预览以前返回 503（当成渲染故障）。现在与 BS 一样：头像给默认头像，预览返回 404。这个回答不按文件的键缓存，文件放回后立刻显示真的渲染结果；只有渲染时找不到文件才走这条路，命中缓存的请求不多做一次存在检查。
  - 报告不提 BS 启用的插件：材质描述、正版验证、Yggdrasil Connect 和自写插件的数据都不声不响地留在了 BS。现在每个启用的插件各占一行，写明 SkinLoom 用什么代替，或者它的数据没有导入。数据库里导入器不读、有数据的表也逐个列出行数（最多数到 100000，大日志表不会拖慢导入），认识的令牌、日志表算作丢弃，Laravel 自己的表不提。留在 BS 的这些条目单独成一节放在报告开头，控制台完整显示，不受 40 行的限制。列表在读数据的事务提交之后再查，查不出来只记一条警告：PostgreSQL 上失败的语句会让整个事务作废。

## 影响
- 新设置 `theme.custom_js`；bootstrap 的 `theme` 多了 `js`；`/themes-assets/_custom/` 也提供 `.js`。
- 页面和后端的回答都带 `Content-Security-Policy`。在页面里注入脚本的插件或反向代理需要改为通过主题或自定义 JavaScript 加载。
- 新包 `internal/platform/datalock`、`internal/platform/fsperm`；数据目录多了 `skinloom.lock`。
- 事件目录：`player.updated` 与 `player.deleted` 的说明写明了账号变化的情况。
- 端到端验证（构建后的前端加后端）：
  - 页面上 17 个脚本都带 nonce，只发出一条策略；
  - 注入的 `onerror` 和一个外站 iframe 被拦下；
  - 自定义 JavaScript 运行，它动态加载的脚本也运行；
  - Turnstile、hCaptcha、reCAPTCHA 用各自的测试密钥都能显示；
  - 3D 皮肤预览、单独打开材质 PNG、皮肤库搜索表单都正常。
