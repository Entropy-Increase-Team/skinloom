# 0041 终端日志：给人看的格式与 NOTICE 级别

- 状态：已接受（2026-11-09）
- 相关：ADR 0033（日志文件与系统服务）

## 背景

终端里的日志用的是 slog 的 `key=value` 格式，可读性差：

- 每行开头是 `time=2026-11-09T15:39:33.541+08:00 level=INFO msg=`，占了半行；
- 级别没有颜色，ERROR、WARN、INFO 看起来一样；
- 新站点首次启动先输出 11 行"migration applied"，安装链接埋在中间；
- 前端进程的输出原样转发：每个请求一行，`- Local: http://127.0.0.1:17547` 让人以为要访问内部端口；开了 `FORCE_COLOR` 时，Next.js 的颜色码也被写进日志文件。

## 决策

**两种去处，两种格式。** 输出到终端时用给人看的格式：

```text
16:03:29 WARN  [web] frontend exited  code=1
```

依次是短时间、五列宽的级别（ERROR 红、NOTE 绿、WARN 黄、INFO 青、DEBUG 灰）、方括号里的组件、消息，以及两个空格之后的属性。键名淡显，`err` 的值标红。写到日志文件、管道（Docker、systemd）时仍是 slog 的 `key=value` 加完整时间戳，方便 grep 和日志工具；`log.format = "json"` 一律输出 JSON。同时有日志文件和终端时，两个 handler 经 `slog.NewMultiHandler` 并行输出，文件排在前面。

是否为终端：Windows 看能否取得控制台模式（并开启 ANSI 转义支持），其它系统看是否为字符设备。遵守 `NO_COLOR`；`FORCE_COLOR` 在非终端也使用易读格式。

**NOTICE 级别**（`slog.LevelWarn + 2`）用于运维者必须看到的信息：站点地址、首次安装链接、正在关闭。它排在 WARN 和 ERROR 之间，所以 `log.level = warn` 时仍然显示。终端里写作 `NOTE`，文件和 JSON 里写作 `NOTICE`（而不是 slog 默认的 `WARN+2`）。

**减少噪音。**

- 迁移只输出一行汇总，逐个文件的记录降到 debug。
- 前端输出按内容分级：请求记录降到 debug，与后端的请求日志一致；以 `⨯`、`Error:` 开头的行是 error；去掉 `- Local:`、`- Network:` 两行；分级之前先去掉 ANSI 转义码。

**启动横幅。** 在终端里启动站点时，先输出一个纯 ASCII 的 SKINLOOM 字符画（61 列，80 列的终端放得下）和版本号。判断方式与上面相同，所以日志文件、Docker 和 systemd 里没有它，不会每次重启都重复一大块。

## 备选方案

- **引入 zap、zerolog 或现成的彩色 slog handler**：功能远超需要。自己写的 handler 不到 200 行，没有新依赖。
- **让日志文件也用易读格式**：文件是给 grep 和日志采集用的，完整时间戳和稳定的 `key=value` 更重要。
- **NOTICE 放在 INFO 和 WARN 之间**：那样 `log.level = warn` 会把安装链接藏起来。

## 影响

- 依赖日志文本的脚本不受影响：文件和管道的格式没有变，`grep setup` 在两种格式下都能取到链接；只有级别名多了 `NOTICE`。
- 日志包的接口改为 `logging.New(logging.Options{…})`，删除了 `logging.Tee`。
