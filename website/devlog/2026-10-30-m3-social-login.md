---
slug: m3-social-login
authors: [maintainers]
title: 第 17 期 · M3 第四部分：第三方登录
date: 2026-10-30
tags: [devlog, M3]
---

M3 的最后一部分：用 GitHub、Microsoft 或任何 OpenID Connect 提供方登录皮肤站。BS 要装一个核心插件再给每家装一个插件，云织在设置里加一项就行。到这里，M3（外置登录与开放平台）的功能全部完成。

<!-- truncate -->

## 本期完成

- **三种提供方**：`github`、`microsoft`、`oidc`（按 issuer 自动发现，Google、Keycloak、Authentik 等都能用），一起写在设置 `auth.social.providers` 里。设置 `site.url` 后，登录页和注册页出现"使用 … 登录"按钮。
- **第一次登录**：已绑定的直接登录；提供方和本站都验证过的同一邮箱自动绑定；其余进入"欢迎"页，创建新账号（邮箱、昵称预填）或"登录并绑定"已有账号。
- **已绑定的账号**：个人资料页查看、绑定、解除绑定。通过第三方登录创建的账号没有密码，可以在个人资料页设置第一个密码。
- 设计记录：[ADR 0025](https://github.com/Entropy-Increase-Team/skinloom/blob/main/docs/adr/0025-social-login.md)；文档站新增[第三方登录](/docs/getting-started/social-login)页面。

## 不只按邮箱认人

BS 的回调拿到邮箱就登录同邮箱的账号，这有两个漏洞：

- **预先劫持**：有人先用你的邮箱注册一个账号（没验证），等你用 GitHub 登录，你们就共用这个账号，他还知道密码。
- **nOAuth**：Microsoft 工作账号的邮箱由组织管理员填写，可以填成任何人的地址。

云织只在两边都验证过邮箱时才自动绑定：GitHub 看主邮箱的 `verified`，OpenID Connect 看 `email_verified`，Microsoft 只认个人账号或带 `xms_edov` 的邮箱。其余情况让本人选择：建新账号，或者用密码登录后绑定。

## 其它细节

- 每次登录带 PKCE 和 state。state 同时写进只发往 `/api/v1/auth/social` 的 cookie，回调时两者必须一致、只能用一次，别的网站不能把访客带到回调、登进攻击者的账号。
- ID Token 校验 issuer、audience、有效期和 nonce，不校验签名：令牌是服务端直接从提供方的令牌端点经 TLS 拿到的，OpenID Connect 规范允许这样做，也省了一个 JOSE 库。
- 没有密码的账号不能用密码或启动器登录；最后一个绑定在没有密码时不能解除，免得把自己锁在外面。设置第一个密码、改邮箱、注销账号不要求输入当前密码。
- 绑定开始后如果浏览器里换了账号，这次绑定作废。

## 开发中的小插曲

- PostgreSQL 上第一个测试之后，后面的测试都提示"安装令牌无效"：共用的测试库里留着"已完成安装"的标记，社交登录的测试在开始前把它清掉。
- 本地实测时，命令行的 `--data` 写成了 Git Bash 风格的 `/e/...`，Windows 程序把它当成当前盘的 `\e\...`，在 E 盘根目录下多出一个数据目录，已改用 `E:\...` 路径。

## 本地实测（`skinloom up --dev`）

用一个临时写的 OpenID Connect 提供方（本机 http），在浏览器里：

1. 登录页出现"使用 测试 SSO 登录"和"使用 GitHub 登录"。
2. 点测试 SSO → 提供方登录 → 回到"欢迎"页，邮箱提示"已由 测试 SSO 验证" → 创建账号，已登录。
3. 个人资料页显示"设置密码"（没有"当前密码"）和已绑定的账号，提示这是唯一的登录方式、不能解绑；设置密码后可以解绑。
4. 解绑后点"绑定 测试 SSO"，经提供方回到 `/user/profile?social_linked=dev`，显示"已绑定"。
5. 在提供方取消：回到登录页，显示"已取消登录。"
6. 用一个邮箱未验证、但和管理员邮箱相同的第三方账号登录：没有自动绑定，进入"欢迎"页；点"登录并绑定"，用管理员密码登录后自动绑定。
7. GitHub 按钮生成的授权地址正确（没有真的去 GitHub）。

审计日志里有绑定、解绑和改密码的记录，服务端日志没有警告或错误。

## 测试

- `internal/social`（SQLite 与 PostgreSQL）：
  - 新账号、预填、关闭注册、昵称为空、只能用一次、没有密码不能密码登录、再次登录、换邮箱后不算验证、邮箱被占用；
  - 封禁、宽限期内注销会恢复、彻底注销；
  - 自动绑定只在两边都验证时发生，`email_verified` 为字符串、从 userinfo 取邮箱；
  - 绑定别人的账号、同一提供方第二个账号、绑定中换了账号、登录后绑定、只能用一次、最后一个绑定不能解；
  - state 与 cookie 不一致、没有 cookie、用两次、别的提供方的回调、取消、过期、清理，以及篡改 issuer、audience、azp、过期时间、nonce、subject，密钥错误；
  - GitHub：主邮箱已验证、没有 `user:email` 权限、错误的 code；Microsoft：个人账号、工作账号、已验证域名、租户限制；
  - 配置校验与 `next` 地址。
- HTTP：bootstrap（没有 `site.url` 时没有提供方，不泄露密钥）、开始登录要 CSRF 头、cookie 属性、新账号、设置第一个密码、记住我、绑定与解绑、登录后绑定、失败时跳回登录页、关闭注册、审计。
- 全量回归：`go test ./...`（SQLite 与 PostgreSQL）、vet、staticcheck、govulncheck，生成代码与迁移无漂移；前端类型检查、测试与构建。

## 问题与风险

- 没有用真实的 GitHub、Microsoft、Google 应用实际登录过，只用了模拟的提供方。
- 后台还没有设置页面，提供方要用命令行或设置接口配置；后台的用户详情还不显示绑定。
- 不支持 OpenID Connect 的 BS 站点（如 LittleSkin）还不能作为提供方。

## 下期计划

M3 收尾：整体回归、修补发现的问题，然后进入 M4。
