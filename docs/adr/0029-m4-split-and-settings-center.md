# 0029 M4 的拆分；设置中心：声明式校验与只写的密钥

- 状态：已接受（2026-10-30）
- 相关：设计文档 02 §1.4（F35–F40）、§4；03 S16；04 §13（M4）；ADR 0016、0025、0027

## 背景
- 04 §13 的 M4 是"后台、主题与运维"：设置中心、默认主题按主题标准重写与首页区块、翻译覆盖、系统状态、CDN 刷新、备份、Compose、Helm、系统服务。验收是四种部署形态按文档从零部署成功、后台切换主题无需重启。
- M1–M3 的模块一共注册了约 50 个设置，`/api/v1/admin/settings` 可以读写，但后台没有设置页面，站长只能用命令行或 API。
- 列表接口把 SMTP 密码、验证码私钥和第三方登录的 `clientSecret` 原样返回给任何一位管理员（BS 的后台也把 reCAPTCHA 私钥放在普通文本框里）。
- 校验散落在各模块：各写各的 `oneOf`、`between`、`nonNegative` 闭包，后台无从知道一个设置有哪些选项、范围是多少，只能显示文本框。
- 02 §4 映射了 BS 的 `require_verification`，一直没有实现。

## 决策
- **M4 分五部分**，每部分一期开发日志：
  1. 设置中心（本 ADR）。
  2. 外观：默认主题按 05 的主题标准重写、首页区块、后台切换主题与调整 token、个性化（F01、F35：首页图、favicon、版权文案、自定义 CSS 只给超级管理员，S16）、`seo.*`。
  3. 翻译覆盖（F39）与系统状态（F40）。
  4. CDN 刷新（F72）与备份（F78）。
  5. Helm、`skinloom service install`，四种部署形态按文档从零验证（F76）。
- **声明式的设置定义**：`settings.Def` 增加
  - `Enum`：可选值；
  - `Range`：整数范围，`settings.Between(lo, hi)` 或 `settings.AtLeast(lo)`；
  - `Secret`：只写，后台只知道有没有设置；
  - `Redact` / `Restore`：值里有部分是密钥时（`auth.social.providers`），显示前遮住、保存时取回。

  注册时先检查定义本身：默认值要满足 Enum 与 Range，`Secret` 不能同时 `Public`，`Redact` 与 `Restore` 必须成对。各模块的校验闭包删掉，错误信息统一为 `want one of [...]`、`must be X to Y`、`must be X or more`。`Validate` 只留给声明不了的检查（正则、地址、时区、提供方列表）。
- **管理接口**：`SettingView` 增加 `secret`、`enum`、`min`、`max`。
  - `Secret` 设置的值永远是 `null`，只有 `isSet`。请求里不带这个键就不改，`null` 清除。
  - `auth.social.providers` 里已保存的 `clientSecret` 显示为 `settings.Redacted`（`********`）。原样发回时按提供方 `id` 取回已保存的值；`id` 改了就没有可沿用的密钥，GitHub 这类必须有密钥的提供方会校验失败。
  - 命令行 `skinloom settings get` 在服务器上运行、直接读数据库，照常输出密钥。
- **取回已保存的密钥放在加锁之后**（ADR 0027）：`Set` 在事务里先用 `UPDATE settings SET updated_at` 锁住这一行，再读出、合并、校验、写入。不加锁时，一位管理员保存新密钥、同时另一位保存了没改密钥的编辑，后者会把旧密钥写回去；测试在 PostgreSQL 上 3–4 轮就复现。第一次保存时还没有这一行可锁，但也没有可沿用的旧密钥，影响可以忽略。
- **后台设置页** `/admin/settings`：
  - 按键名前缀分组，取最长的匹配：`auth.social.` 归"第三方登录"，其余 `auth.`、`captcha.` 归"注册与登录"。不认识的前缀（以后的插件）进"其它"。`theme.*` 与 `home.*` 留给第 2 部分的外观页。
  - 表单按类型生成：布尔是勾选框，有 Enum 的是下拉框，整数带上下限，多语言的每种语言一个输入框，JSON 是文本框，密钥是密码框。
  - 每组单独保存，只发改了的键。Enum、Range、整数与 JSON 先在前端检查，用页面的语言提示；服务端的其它校验信息原样显示在字段下。
  - 核心设置的标签与说明写在前端消息里；英文和插件的设置没有说明时显示服务端的 `Doc`。
- **`auth.require_email_verification`**（BS `require_verification`，默认关闭）：开启后，没有验证邮箱的账号不能
  - 管理角色：`/api/v1/me/players` 与兼容 BS 的 `/api/players` 的写操作；
  - 上传、修改或删除材质；
  - 提交举报；
  - 创建或修改 OAuth 应用、重置应用密钥、创建个人访问令牌；
  - 在启动器登录（Yggdrasil authenticate）。

  这和 BS 的 `verified` 中间件与 yggdrasil-api 插件的范围相同，另外加上了 BS 没管的 `/api/players`，否则用令牌就能绕过。衣柜、收藏与浏览不受影响；开启前签发的启动器令牌照常用到过期（BS 也是如此）。
  - `/api/v1` 返回 403，problem 的 `type` 是 `tag:skinloom.cn,2026:email-not-verified`（RFC 9457）。前端据此提示"请先验证邮箱"，不会和其它 403（例如举报自己的材质）混在一起。兼容 BS 的接口按 BS 的格式返回 `{"message"}`。
  - 站点 bootstrap 增加 `auth.requireVerification`。未验证的用户在页头看到被限制了什么；没有配置邮件时提示联系管理员。
  - 安装向导创建的超级管理员本来就标记为已验证（与 BS 相同），开启后不会挡住站长自己。

## 影响
- 没有新表与迁移。新增设置 `auth.require_email_verification`；`SettingView`、`AuthInfo` 增加字段，OpenAPI 文档与前端类型已重新生成。
- 模块注册设置时直接声明选项与范围；第二阶段插件的设置用同一套定义，不用改设置页就能显示。
- 后台再也看不到密钥；需要核对时只能在服务器上用命令行。
