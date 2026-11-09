---
title: 第三方登录
sidebar_position: 6
---

# 第三方登录

让用户用 GitHub、Microsoft，或任何支持 OpenID Connect 的服务（Google、Keycloak、Authentik、企业统一登录等）登录。

## 开启

1. 确认已设置站点地址 `site.url`。
2. 在提供方那里创建一个 OAuth 应用，回调地址填：

   ```text
   https://skins.example.com/api/v1/auth/social/<id>/callback
   ```

   `<id>` 是你给这个提供方起的名字，例如 `github`。
3. 把 Client ID 和 Client Secret 写进设置：

   ```bash
   skinloom settings set auth.social.providers '[
     {"id": "github", "type": "github", "clientId": "Ov23li…", "clientSecret": "…"},
     {"id": "microsoft", "type": "microsoft", "clientId": "…", "clientSecret": "…", "tenant": "consumers"},
     {"id": "google", "type": "oidc", "name": "Google", "issuer": "https://accounts.google.com", "clientId": "…", "clientSecret": "…"}
   ]'
   ```

几秒内登录页就会出现对应的按钮。

| 字段 | 说明 |
|---|---|
| `id` | 小写字母、数字、`-`，最长 32 个字符。**定下来后不要改**，否则已有的绑定全部失效 |
| `type` | `github`、`microsoft` 或 `oidc` |
| `clientId` / `clientSecret` | 应用的凭据，密钥不会发给浏览器 |
| `name` | 按钮上的名字，可选 |
| `issuer` | `oidc` 必填，https 地址 |
| `tenant` | `microsoft` 可选：`common`（默认）、`consumers`（只限个人账号）、`organizations`（只限工作 / 学校账号）或租户 ID |
| `scopes` | `oidc` 可选：额外申请的 scope |

在哪里创建应用：

- **GitHub**：Settings → Developer settings → OAuth Apps → New OAuth App。
- **Microsoft**：Azure 门户 → Microsoft Entra ID → 应用注册，平台选"Web"；在"证书和密码"里创建客户端密码。
- **Google**：Google Cloud 控制台 → API 和服务 → 凭据 → 创建 OAuth 客户端 ID（Web 应用）。

## 账号怎么对应

- 绑定过的直接登录。
- 提供方验证过的邮箱与本站某个**已验证邮箱**的账号相同时，自动绑定并登录（从 Blessing Skin 迁移来的用户靠这个找回账号）。
- 其它情况让用户选择创建新账号或绑定已有账号。关闭注册时只能绑定。

只要有一边的邮箱没验证过就不会自动绑定，防止有人用别人的邮箱先注册来劫持账号。

删除一个提供方后按钮消失，已有的绑定保留，用户可以自行解除。
