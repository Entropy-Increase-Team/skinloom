---
title: 注册、登录与邮件
sidebar_position: 3
---

# 注册、登录与邮件

## 注册

- 默认开放注册。关闭：`auth.registration = closed`，注册按钮随之隐藏。
- 同一地址 24 小时内最多注册 `auth.registrations_per_ip` 个账号（默认 3）。放在反向代理后面时务必配置[可信代理](../deployment/reverse-proxy.md)，否则所有人都会被算成代理的地址。
- 新账号获得 `score.initial` 积分（默认 1000）。
- 想只让验证过邮箱的人创建角色、上传材质、在启动器登录：开启 `auth.require_email_verification`（需要先配置好邮件）。

## 邮件

验证邮箱和找回密码都需要邮件，并且要设置站点地址 `site.url`：邮件里的链接只用它生成。

以 QQ 邮箱为例（先在邮箱设置里开启 SMTP、生成授权码）：

```bash
skinloom settings set mail.from '"云织 <你的号码@qq.com>"'
skinloom settings set mail.smtp.host '"smtp.qq.com"'
skinloom settings set mail.smtp.port 465
skinloom settings set mail.smtp.encryption '"tls"'
skinloom settings set mail.smtp.username '"你的号码@qq.com"'
skinloom settings set mail.smtp.password '"授权码"'
skinloom settings set mail.driver '"smtp"'
```

也可以在后台"设置 → 邮件"里填。其它邮箱同理：465 端口用 `tls`，587 端口用 `starttls`。

**测试**：用测试接口立即发一封信，失败时会返回邮件服务器的错误：

```bash
curl -X POST -H "Authorization: Bearer $(skinloom admin token)" -H 'Content-Type: application/json' \
  -d '{"to":"you@example.com"}' http://127.0.0.1:17546/api/v1/admin/mail/test
```

邮件在后台发送，失败会在 1 分钟、5 分钟、15 分钟、1 小时后重试。本地测试可以用 `mail.driver = log`，邮件内容写进日志而不真的发出去。

## 登录保护

不需要配置，默认就有：

- 登录失败只提示"邮箱或密码不正确"，不透露账号是否存在。
- 同一客户端失败 `auth.login_captcha_after` 次（默认 3）后要先完成验证码。
- 每 15 分钟内：同一地址最多失败 50 次，同一地址对同一账号最多 10 次，超过就要等。
- 同一账号在所有地址上合计失败 20 次后，所有人登录它都要验证码，但账号不会被锁，攻击者没法借此把真正的用户挡在门外。（关闭了验证码时，改为合计 100 次后暂时锁定。）
- 找回密码不透露邮箱是否注册过，并且有频率限制。

## 验证码

用在注册和多次登录失败后。默认是内置的图形验证码，不依赖任何外部服务。换成第三方：

```bash
skinloom settings set captcha.provider '"turnstile"'
skinloom settings set captcha.turnstile.site_key '"0x4AAAA…"'
skinloom settings set captcha.turnstile.secret_key '"0x4AAAA…"'
```

| `captcha.provider` | 说明 |
|---|---|
| `image`（默认） | 内置图形验证码 |
| `turnstile` | Cloudflare Turnstile，多数情况下无需操作 |
| `recaptcha` | Google reCAPTCHA v2，通过 `recaptcha.net` 访问 |
| `hcaptcha` | hCaptcha |
| `none` | 关闭（公网开放注册时不建议） |

内置图形验证码保存在后端进程的内存里：后端有多个副本时，请用第三方验证码。

## 第一个管理员

安装链接创建的是**超级管理员**。之后在"后台 → 用户"里把其他人设为管理员，见[后台管理](./management.md)。
