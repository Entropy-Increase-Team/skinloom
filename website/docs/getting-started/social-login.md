---
title: 第三方登录
sidebar_position: 8
---

# 第三方登录（GitHub、Microsoft、OpenID Connect）

用户可以用 GitHub、Microsoft 账号，或任何支持 OpenID Connect 的服务（Google、Keycloak、Authentik、企业的统一登录等）登录皮肤站。不需要装插件，在设置里加上提供方即可。

## 开启

1. 先设置站点地址 `site.url`（安装向导会自动填写）。回调地址只用它生成，没有它就不显示第三方登录。
2. 在提供方那里创建一个应用（OAuth App），回调地址填：

   ```text
   https://skins.example.com/api/v1/auth/social/<id>/callback
   ```

   `<id>` 是下面配置里你给这个提供方起的名字，例如 `github`。
3. 把应用的 Client ID 和 Client Secret 写进设置 `auth.social.providers`：

```bash
skinloom settings set auth.social.providers '[
  {"id": "github", "type": "github", "clientId": "Ov23li…", "clientSecret": "…"},
  {"id": "microsoft", "type": "microsoft", "clientId": "…", "clientSecret": "…", "tenant": "consumers"},
  {"id": "google", "type": "oidc", "name": "Google", "issuer": "https://accounts.google.com", "clientId": "…", "clientSecret": "…"}
]'
```

运行中的站点几秒内生效，登录页和注册页会出现"使用 GitHub 登录"等按钮。删掉一项就不再显示，已有的绑定保留。

| 字段 | 说明 |
|---|---|
| `id` | 小写字母、数字、`-`，最长 32 个字符。出现在回调地址里，绑定记录也记在它上面：**定下来后不要改**，否则所有人的绑定都会失效 |
| `type` | `github`、`microsoft` 或 `oidc` |
| `clientId` / `clientSecret` | 应用的凭据。密钥只保存在服务端，不会发给浏览器 |
| `name` | 按钮上显示的名字；不填时是 GitHub、Microsoft 或 `id` |
| `issuer` | `oidc` 必填：提供方的 issuer 地址，站点会读取 `<issuer>/.well-known/openid-configuration`。必须是 https |
| `tenant` | `microsoft` 可选：`common`（默认，个人和工作 / 学校账号）、`consumers`（只允许个人账号）、`organizations`（只允许工作 / 学校账号）或某个租户 ID |
| `scopes` | `oidc` 可选：除 `openid email profile` 外还要申请的 scope |

各家创建应用的位置：

- **GitHub**：Settings → Developer settings → OAuth Apps → New OAuth App，"Authorization callback URL" 填上面的回调地址。
- **Microsoft**：Azure 门户 → Microsoft Entra ID → 应用注册 → 新注册，平台选"Web"并填回调地址；在"证书和密码"里创建客户端密码。
- **Google**：Google Cloud 控制台 → API 和服务 → 凭据 → 创建 OAuth 客户端 ID（Web 应用），"已获授权的重定向 URI" 填回调地址；`issuer` 是 `https://accounts.google.com`。

## 用户第一次登录

点按钮后跳到提供方登录、同意，再回到皮肤站：

- 已经绑定过的账号直接登录。
- 提供方验证过的邮箱，与本站某个**已验证邮箱**的账号相同时，视为同一个人，自动绑定并登录。从 Blessing Skin 迁移过来、以前用第三方登录的用户就是这样找回账号的。
- 其它情况进入"欢迎"页面，可以：
  - **创建账号**：邮箱和昵称已经预填，可以修改。保留提供方验证过的邮箱，新账号的邮箱直接视为已验证；换成别的邮箱则照常发送验证邮件。关闭注册（`auth.registration = closed`）时不能创建，只能绑定已有账号。
  - **登录并绑定**：已有账号的人用邮箱和密码登录，登录后自动绑定。

只要有一边的邮箱没有验证过，就不会自动绑定：否则有人先用你的邮箱注册一个账号，等你用 GitHub 登录时就和你共用了账号；有的提供方（如 Microsoft 的工作账号）允许管理员把邮箱填成任意地址。Blessing Skin 只按邮箱认人，这两种情况都能被用来登录别人的账号。

## 已绑定的账号

个人资料页的"已绑定的账号"列出绑定的第三方账号，可以绑定其它提供方，也可以解除绑定。每个第三方账号只能绑定一个本站账号。

通过第三方登录创建的账号**没有密码**：

- 不能用邮箱和密码登录，也不能在游戏启动器（外置登录）里登录。在个人资料页"设置密码"后两者都可以用。
- 设置第一个密码、修改邮箱、注销账号时不需要输入当前密码。
- 没有密码时，最后一个绑定不能解除，否则账号就再也登不进去了。站长删掉的提供方的绑定不算，可以随时解除。
