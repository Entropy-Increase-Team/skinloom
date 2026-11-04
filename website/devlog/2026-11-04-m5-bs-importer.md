---
slug: m5-bs-importer
authors: [maintainers]
title: 第 26 期 · M5 从 Blessing Skin 导入
date: 2026-11-04
tags: [devlog, M5]
---

M5 开始了。第一部分是 `skinloom import bs`：把一个 Blessing Skin 站点的账号、角色、材质、衣柜、举报和未读通知搬进全新的 SkinLoom，密码、UUID、皮肤链接都不变。

<!-- truncate -->

## M5 的安排（ADR 0035）

M5 是"迁移与发布"，分五部分：

1. BS 导入器：账号、角色与 UUID、材质与文件、衣柜、举报、通知（本期）。
2. BS 导入器：站点设置、翻译覆盖、OAuth 应用、Yggdrasil 签名密钥，以及一份脏数据迁移回归库。
3. 契约测试全量：黄金文件、authlib-injector 端到端、渲染快照。
4. k6 压测。
5. 安全自查：CSP 与自定义 JS，以及 M4 审查记下的遗留问题。

之后找一个真实的 BS 站做 beta，稳定运行两周后发布 v1.0。

## 怎么用

```bash
skinloom import bs --env /var/www/blessing-skin/.env --dry-run   # 先试
skinloom import bs --env /var/www/blessing-skin/.env             # 再导
```

从 BS 的 `.env` 读出数据库、表前缀、密码算法和盐，材质从 `storage/textures` 读。MySQL、PostgreSQL、SQLite 都支持。完整的步骤见文档[从 Blessing Skin 迁移](/docs/deployment/from-blessing-skin)。

## 几个决定

- **只导入到全新的站点，整个导入是一个事务。** 试运行就是同一个导入最后撤销，所以试运行的报告和正式导入完全一样，连写库时才会暴露的约束冲突都能提前看到。失败的导入什么也不留，修好原因直接重跑。导入成功后不能再导一次：那时站点可能已经有了新数据，和 BS 合并没有安全的默认做法。
- **ID 保持不变。** 用户、角色、材质、举报都沿用 BS 的 ID，`/skinlib/show/123` 这样的链接继续有效。PostgreSQL 的自增序列在同一个事务里、提交之前往前移。
- **源库只读。** SQLite 用只读方式打开，测试会比较导入前后的数据库文件。MySQL、PostgreSQL 在一个只读事务里读，BS 没停也能读到一致的快照。
- **邮箱只差大小写的两个账号会阻止导入。** BS 的邮箱区分大小写，SkinLoom 不区分。自动改掉其中一个，那个人就用原来的邮箱登录不了，所以交给站长决定。
- **其余的脏数据按默认规则处理，并写进报告。** 重名的角色（BS 并发注册的产物）加 `_2` 后缀，并给主人发一条站内通知。同一用户重复上传的材质合并。指向已删除材质的皮肤清空。衣柜里的重复行去掉。
- **材质文件逐个检查。** 每个文件都算一遍 SHA-256、读一遍 PNG 头。内容和文件名对不上的，按内容的 hash 存。缺失的文件不影响导入：材质照样导入，访问时和 BS 一样返回 404。

## 密码

BS 支持的算法都认得：`password_hash()` 的结果按前缀识别，十六进制摘要按长度区分 MD5、SHA256、SHA512，是否加盐看 `PWD_METHOD`。启用了 password-algorithm-transition 插件的站点同时有几种格式，导入器按插件的 `PASSWORD_METHODS` 和对应的盐逐个识别。首次登录成功后，统一升级为 argon2id。

## 验证

- 测试用一个故意弄脏的 BS 站：重名角色、重复邮箱、重复上传、悬空引用、缺失和被改过的文件、重复的衣柜行和举报。先导入 SkinLoom 的 SQLite 和 PostgreSQL，再分别从 PostgreSQL 和 MySQL 的 BS 库读一遍（MySQL 在 CI 里跑）。
- 端到端：编译好的程序导入一个 SQLite 的 BS 站，启动后端。之后：
  - 用 BS 的加盐 MD5 密码登录成功（邮箱换了大小写），密码随即升级为 argon2id；
  - CSL 和 Yggdrasil 返回原来的材质 hash 和 UUID，材质文件能正常下载；
  - 改名的角色主人收到了通知。

## 审查后的修正

导入器写完后做了一次完整审查，下面这些都补了回归测试：

- 启用 password-algorithm-transition 插件的站点，旧方法的加盐密码以前全部按不加盐存，这些用户登不上。现在按 `PASSWORD_METHODS` 和拆开的 `SALT` 配对识别。
- 材质目录与 BS 共用时，内容被改过的文件以前不复制，材质一导入就是 404。现在这些文件按内容的 hash 复制进去。文档改为推荐复制：共用之后，定期清理会删掉目录里没有材质用到的 BS 文件。
- 角色的 UUID 以前逐个匹配，前面只差大小写的角色会抢走后面角色精确匹配的 UUID，名字带首尾空格的角色拿不到原来的 UUID。现在先全部精确匹配，再做不区分大小写的匹配，最后才生成。
- 文字里的 NUL 和无效 UTF-8 以前会让 PostgreSQL 拒绝整个导入，现在会被清理并记入报告。
- `.env` 的读法与 Laravel 对齐：不带引号的值在 `#` 处截断，`null`、`(empty)` 等都当作空。
- 读可选表时，只有数据库明确回答"表不存在"才当作没有，权限或网络错误会让导入停下。
- PostgreSQL 的序列以前在提交之后才移，那一步失败时站点已经导入，新注册的账号会撞上已有 ID。现在挪进了事务。
- 同时运行两个导入，以前可能都通过"站点为空"的检查。现在事务先占一行锁，第二个等它结束，然后拒绝。
- 复制或写库失败时报告照样写出；`--report` 指向已有文件时权限也改成 0600；连接串和盐可以放在 `SKINLOOM_BS_DSN`、`SKINLOOM_BS_SALT` 里。

## 还没有的

- 站点设置、翻译覆盖、OAuth 应用、Yggdrasil 签名密钥在下一期。在那之前，用 authlib-injector 的游戏服务器在切换后要重启一次，才会取到新的签名公钥。
- 其它插件的表（邀请码、正版验证等）以后按插件逐个提供转换。

## 下期计划

M5 第二部分：导入站点设置、翻译覆盖、OAuth 应用和 Yggdrasil 密钥，并整理一份脱敏的迁移回归库。
