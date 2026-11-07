# 0036 BS 导入器：站点设置、翻译、OAuth 应用与签名密钥

- 状态：已接受（2026-11-05）
- 相关：设计文档 02 §2.6、§4、§6；ADR 0026、0031、0034、0035

## 背景
- ADR 0035 把 M5 分成五部分。第 1 部分导入账号、角色、材质、衣柜、举报和通知。站点设置、翻译覆盖、OAuth 应用、Yggdrasil 签名密钥留给第 2 部分：在它们导入之前，站长要重新填写设置，用 authlib-injector 的游戏服务器要在切换后重启一次。
- 02 §6 还要求两类测试：密码兼容向量（第 3 条），以及用脏数据库做迁移回归（第 4 条）。
- BS 的设置分在两处：
  - `options` 表，由 `option()` 读取，缺省值在 `config/options.php`；
  - `.env`，里面有邮件设置。
- BS 的多语言选项把每种语言的副本存为 `名字_<locale>`（如 `site_name_en`），没有副本的语言显示 `名字`。BS 的界面语言固定为 `zh_CN`。

## 决策
- **设置走 SkinLoom 的设置注册表。**
  - 每个映射出的值都用目标设置自己的校验器检查并规范化（`settings.Def.Canonical`），与后台保存时一样。不合格的值保留 SkinLoom 的默认值，并写一条警告。
  - 等于默认值的不写行，与设置服务的约定一致。
  - 只报告站点改过的选项：值与 BS 默认值相同的选项，即使 SkinLoom 没有对应的设置，也不出现在报告里。例外是 `register_with_player_name`：BS 默认在注册时要求填角色名，SkinLoom 不要求，用户看得到这个变化，所以总是报告。
  - 缺少的选项按 `option()` 取 `config/options.php` 的默认值。其中公告的默认值还带着 `{version}` 占位符，与 BS 安装时填好版本号的公告一样不导入。多语言设置没有一种语言有文字时，保留 SkinLoom 的默认值，不写成空的（否则 BS 的默认简介会把 SkinLoom 的简介清空）。
  - 新包 `internal/sitesettings` 把注册表的组装从 `app.Core` 中提出来，服务器和导入器用的是同一份定义。
- **PHP 的取值规则照搬。**
  - `option()` 把 `true`、`(true)` 读成真，`false` 读成假。
  - 整数按 PHP 的 `(int)` 取前导数字。所以 BS 默认的 `score_per_storage = 'true'` 是 1，`sign_gap_time = 'a day'` 是 0。这种取值与 BS 实际的行为一致，但值与字面不同时记为已修正。
- **主要的映射**（02 §4）：

  | BS | SkinLoom | 说明 |
  |---|---|---|
  | `site_name`、`site_description`、`announcement`、`content_policy`、`meta_keywords` | `site.name` 等多语言设置 | BS 的 `zh_CN` 副本与基础值进 `zh-CN`，`en` 副本进 `en`。其它语言的副本丢弃并报告。BS 安装时填入的默认简介与公告不导入 |
  | `site_url` | `site.url` | 去掉结尾的 `/` 与 `/index.php` |
  | `copyright_text` | `site.copyright` | BS 的是 HTML，SkinLoom 的是 Markdown：链接、粗体、斜体、换行转成 Markdown，其它标签去掉；`{site_name}`、`{site_url}` 填成实际值（已修正）。BS 的默认版权文字不导入 |
  | `favicon_url` | `site.favicon` | 只接受 http(s) 地址，相对路径指向的是 BS 自己的文件 |
  | `require_verification`、`regs_per_ip` | `auth.*` | |
  | `recaptcha_sitekey` + `recaptcha_secretkey` | `captcha.provider = recaptcha` | 两个都填了才切换。只填一个时 BS 的验证码本来就不能用，保留图片验证码并警告。`recaptcha_invisible` 不再区分（已修正） |
  | `player_name_rule`、`custom_player_name_regexp`、长度 | `player.*` | 长度限制在 1～32，上下颠倒时交换（已修正） |
  | 签到、积分各项 | `score.*` | `sign_score = "a,b"` 拆成最小、最大值 |
  | `max_upload_file_size`、`max_texture_width` | `texture.max_size_kb`、`texture.max_width` | 超出 SkinLoom 的范围时截到上限（已修正） |
  | `texture_name_regexp` | `texture.name_regexp` | |
  | `allow_downloading_texture`、`status_code_for_private` | `texture.*` | |
  | `custom_css` | `theme.custom_css` | |
  | `ygg_*` | `yggdrasil.*` | `ygg_uuid_algorithm` 为空但有 `uuid` 表时按 v4（与第 1 部分生成 UUID 的规则一致） |
  | 时区（`--timezone`） | `site.timezone` | |

- **BS 的正则表达式**（角色名、材质名）是 PCRE，SkinLoom 用 RE2。
  - 导入时去掉分隔符。标志 `i`、`m`、`s` 转成 RE2 的写法，`u`、`D` 忽略。
  - 带其它标志，或 RE2 编译不了（反向引用、环视），就保留 SkinLoom 的默认规则并警告。
- **没有对应设置的选项丢弃并说明去处**：
  - `custom_js`：等 CSP 与自定义 JS（M5 第 5 部分）。ADR 0039 起导入为 `theme.custom_js`，并警告运行方式不同。
  - `meta_extras`、`meta_description`、`home_pic_url`、BS 的导航栏颜色等布局选项、`cache_expire_time`、`cdn_address`、`auto_del_invalid_texture`、`register_with_player_name`、`take_back_scores_after_deletion = false`、`copyright_prefer`。
- **邮件设置从 `.env` 读**：
  - 读 `MAIL_MAILER`，没有时读旧的 `MAIL_DRIVER`。
  - `log` 照搬；`smtp` 以外的方式只警告。从没填过 `MAIL_HOST` 的不导入，Laravel 的缺省主机是个占位符。
  - 加密方式按 Laravel 10（Symfony Mailer）的行为换算：
    - 端口 465 用 TLS。Laravel 10 只按端口决定，`MAIL_ENCRYPTION=ssl` 配 587 端口实际走的也是 STARTTLS（ADR 0039 审查时改正，以前把 `ssl` 一律当作 TLS）；
    - 端口 25 且没写加密方式时不加密；
    - 其它情况用 STARTTLS。
  - 这里有一处不同：Symfony 在服务器不支持 STARTTLS 时会退回明文，SkinLoom 不会退回，发信失败时在后台改成"不加密"即可。这一项写进报告，提醒站长发一封测试邮件。
  - SMTP 密码按普通的密钥设置保存，此后适用 ADR 0034 的规则：改服务器要重新填写密码。
  - 不带 `--env` 时没有邮件设置，报告提醒到后台填写。
- **翻译覆盖**（`language_lines` → `i18n.overrides`，ADR 0031）：
  - SkinLoom 的界面文字是重写的。BS 的 824 个键里，按文字自动匹配能对上的只有几十个，而且多是巧合（同一个"确定"在两边的语境不同），所以不做自动匹配。
  - 改用一张人工维护的术语表：首页、皮肤库、用户中心、登录、衣柜、管理面板、积分、皮肤、披风。这些是站长最常改的词，比如把"积分"改成"金币"。一个 BS 键对应 SkinLoom 里所有只显示这个词的键。包含这个词的长句保留默认文字，并在报告里写明。
  - 其它改过的文字全部丢弃并逐条报告，提示到翻译页重新改。
  - 不导入的情况：不是 JSON 的文字、SkinLoom 没有的语言、含 `{}` 的文字（ICU 语法不同）、超过 200 字的文字。
  - 测试检查术语表里的键都在消息目录里，界面改了键名时测试会失败。
- **OAuth 应用**（02 §2.6）：
  - 保留 ID 与所有者。Passport 的个人访问客户端、密码授权客户端、已撤销的、所有者不存在的，一律不导入。
  - 回调地址按逗号拆开，逐个用 SkinLoom 的规则检查（`oauth.CheckRedirects`）：不合格的去掉并警告，去重后最多 10 个；一个也不剩的应用不导入。
  - 密钥：BS 用的 Passport 没有开启密钥哈希，存的是明文，导入时存它的 SHA-256。第三方应用不用改配置。
    - 空密钥按公开客户端导入，SkinLoom 要求它使用 PKCE。
    - 已经是 bcrypt/argon2 哈希的密钥无法换算，换成随机值并警告，应用的主人要在用户中心重新生成密钥。
  - 已签发的令牌永远不导入（ADR 0035），第三方应用的用户要重新授权一次。
- **Yggdrasil 签名密钥**：
  - 读 yggdrasil-api 插件的 `ygg_private_key`，PKCS#1 与 PKCS#8 都接受。
  - 必须是 RSA、能通过 `Validate()`、至少 2048 位。导入时转成 SkinLoom 存的 PKCS#8 PEM，覆盖目标库里已有的密钥（全新站点启动过就会有一把）。
  - 公钥不变，游戏服务器不用重启。
  - 读不出的密钥用新生成的，并警告：用 authlib-injector 的服务器要重启一次。
- **写入顺序与锁**（ADR 0026）：
  - 设置在同一个导入事务里写入，排在 OAuth 应用之后、安装标记之前。
  - 导入的第一个锁仍是 `import.bs.running`。之后先更新配置版本行，再按键的顺序删除、插入设置行，与设置服务的顺序相同。
  - 站点在导入期间是停止的；即使开着，后台保存设置也会在配置版本行上等待导入完成。
- **测试**：
  - **密码兼容向量**：BS 的十种密码方法（包括 PHP 可能把 `PASSWORD_DEFAULT` 改成的 ARGON2ID），分别配空盐、普通盐、带逗号和中文的盐。哈希按 BS 的方式算，PHP 的 `$2y$` 与 libargon2 的编码也照搬。每种都要能登录、要求升级到 argon2id、拒绝错误的密码、不产生警告。
  - **迁移回归库**：第 1 部分的脏数据站点，加上设置、翻译和 OAuth 应用，整份报告与 `testdata/regression-report.golden` 比较。导入器行为的任何变化都以这个文件的 diff 出现，审查后用 `go test -update` 接受。
  - 02 §6 要的是一个真实 BS 库的匿名副本，拿到之前先用这份。
  - PostgreSQL 与 MySQL 源的测试加上了 `language_lines` 与 `oauth_clients`。`group`、`key` 在 MySQL 里是保留字，读它们时按方言加引号。

## 备选方案
- 按文字自动匹配翻译：错配会悄悄改掉不相关的界面文字，比不导入更糟。
- 把翻译按 BS 的键原样存下、以后再映射：SkinLoom 不会读它们，站长会以为已经生效。
- 把 OAuth 密钥原样存成明文：SkinLoom 只存哈希，为导入的应用开一个例外不值得。
- 导入时让管理员确认每个丢弃的设置：导入是非交互的，dry-run 的报告已经起到同样的作用。

## 影响
- `skinloom import bs` 现在导入站点设置、邮件设置、翻译中的常用术语、OAuth 应用和签名密钥。报告多了 `options`、`.env`、`language_lines`、`oauth_clients` 的条目。
- 新包 `internal/sitesettings`；`settings.Def.Canonical`、`oauth.CheckRedirects`、`oauth.HashSecret` 导出。
- 文档"从 Blessing Skin 迁移"去掉了"这一版还没有"一节，改为说明设置的去向。
