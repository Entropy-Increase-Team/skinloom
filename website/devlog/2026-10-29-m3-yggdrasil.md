---
slug: m3-yggdrasil
authors: [maintainers]
title: 第 14 期 · M3 第一部分：外置登录
date: 2026-10-29
tags: [devlog, M3]
---

M3 从外置登录开始：云织内置了 authlib-injector 的 Yggdrasil API，启动器用站点账号登录、进服，游戏服务器校验后显示站点上的皮肤。线上协议与 Blessing Skin 的 yggdrasil-api 插件一致，原来的启动器和服务器换个地址就能用。

<!-- truncate -->

## 本期完成

- **Yggdrasil API**（`/api/yggdrasil`，`internal/yggdrasil`）：
  - 元数据、authserver 五个接口、join / hasJoined / profile、按名字查询、材质上传与清除、`publickeys`；
  - 错误格式与文案和 authlib-injector 文档一致，启动器能正确提示"密码错误""令牌失效"。
- **令牌进数据库**：
  - 只存 SHA-256 哈希，按用户 ID 归属；格式仍是 BS 的 HS256 JWT，但有效与否只看数据库；
  - 有效期 3 天、可刷新 7 天、每人最多 10 个，都可以设置。
- **签名**：RSA 4096 位私钥和令牌密钥放在新表 `site_secrets`，多实例共用、随数据库备份；签名结果按值缓存。
- **吊销**：改密码、重置密码、封禁、注销、退出其它设备时，启动器令牌和网页会话一起失效。
- **启动器引导**：
  - 首页区块和个人资料页给出接口地址、复制按钮、可以拖进启动器的链接；
  - 所有响应带 `X-Authlib-Injector-API-Location`，只填站点地址也行；
  - 个人资料页显示几个启动器登录着，可以一键全部退出。
- 设计记录：[ADR 0022 Yggdrasil 外置登录](https://github.com/Entropy-Increase-Team/skinloom/blob/main/docs/adr/0022-yggdrasil.md)；文档站新增[外置登录](/docs/getting-started/launchers)。

## 修掉的插件问题

- 令牌只在缓存里，清缓存全站掉线；并发登录丢令牌。
- 邮箱含大写字母时刷新出的令牌无法使用（#212）；用角色名 signout 什么都不吊销。
- 用角色名登录不绑定角色（#123）、改名后令牌出错（#231）、名字大小写不对（#232）。
- 登录节流只按请求里的用户名计，换个名字就绕过，也能刷别人的名字把人锁住。
- 材质上传接口接受任何人的有效令牌，可以改别人角色的皮肤。
- join 之后角色被删，hasJoined 报 500；批量查询每个名字一次查询；每次请求重新解析私钥。

## 开发中发现的问题

- **SQLite 自己等自己**：最早签发令牌时在事务里顺手"加载或生成密钥"，密钥用的是另一个连接写库。SQLite 同一时刻只有一个写者，事务没提交，生成密钥的写入就一直等，直到 `SQLITE_BUSY`。改为先加载密钥，再开事务。
- **角色转移后的旧令牌**：写文档时重读代码发现，管理员把角色转给别人以后，旧主人绑定这个角色的令牌还能用它进服。现在令牌所绑定的角色已不属于令牌主人时，按"未绑定"处理，进服和上传都会被拒，refresh 之后也不再带这个角色。补了测试，去掉修复后测试失败。

## 本地实测（`skinloom up --dev`）

用一个只依赖标准库的小程序，按 authlib-injector、启动器和游戏服务器的方式走完一遍：

1. 请求首页，响应头里有 `X-Authlib-Injector-API-Location: /api/yggdrasil`。
2. 读元数据：站点名、4096 位公钥、`skinDomains`。
3. 用角色名 `Steve` 和密码登录，返回的 `selectedProfile` 就是 Steve。
4. 用 Bearer 令牌以 multipart 上传一张纤细皮肤，204。
5. join 一个 `serverId`，再以游戏服务器的身份 hasJoined：两个属性的签名都能用元数据里的公钥验证通过；`textures` 里是纤细模型和材质地址，按这个地址下载到的就是那张 PNG。
6. refresh 得到新令牌：旧令牌 validate 返回 403，新令牌 204；invalidate 后新令牌也被拒。

## 测试

- 服务层（SQLite 与 PostgreSQL）：
  - 邮箱 / 角色名登录与绑定、只有一个角色时自动绑定、错误的密码与不存在的名字、封禁的账号、`clientToken`；
  - 登录限流；
  - refresh 选角色（不能选别人的、已绑定的不能再选）、过期与可刷新期、令牌数量上限、validate / invalidate / signout；
  - join / hasJoined：签名验证、名字大小写、IP、30 秒窗口、中途删角色；批量与单个查询（封禁用户的角色查不到）、数量上限；
  - 材质上传：只有角色主人的令牌可以、照常收积分、积分不足与尺寸不对的错误格式；
  - 密钥多实例共享，改密码与退出其它设备时令牌失效；角色转移后旧令牌失效。
- HTTP：全部接口走一遍，包括错误格式、带 Cookie 的请求不需要 CSRF 头、个人资料页的数量与一键退出。
- 网关：附加的响应头出现在所有响应上。
- 全量回归：
  - Go：`go test ./...`（含 PostgreSQL）、vet、staticcheck、govulncheck；
  - 生成代码无差异、迁移无漂移；
  - 前端：类型检查、单元测试、生产构建、`bun audit`。

## 问题与风险

- 只用自己写的程序按协议验证过，还没有用真实的启动器（HMCL、PCL2）和装了 authlib-injector 的服务器实际进一次服。
- 登录限流在每个实例的内存里，多实例时各算各的（与网页登录相同）。
- 按账号的登录上限（15 分钟 100 次）在启动器上没有验证码可以兜底，分布式猜密码时这个账号的启动器登录会被暂时拦住。

## 下期计划

M3 第二部分：OAuth2 提供方、与 Blessing Skin 兼容的资源 API、个人访问令牌。
