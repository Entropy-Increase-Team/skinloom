# 0002 Go 模块化单体：chi + Huma，不用 Kratos

- 状态：已接受（2026-10-23）
- 相关：设计文档 04 §2.3

## 背景
项目由社区站长自托管，贡献者以业余开发者为主；维护者所在团队的其它平台采用 Kratos 微服务。

## 决策
- 模块化单体：领域模块之间只经 Service 接口调用；同一二进制按 `server.role` 拆面扩容（M0 支持 `all`、`api`）。
- HTTP：chi v5（标准库 Handler 生态）+ Huma v2（代码即 OpenAPI 3.1、请求校验、RFC 9457 错误）。
- 不使用 Kratos / go-zero：proto-first、注册中心、gRPC 依赖对自托管和社区贡献都是负担。

## 影响
- OpenAPI 文档由 `go generate ./api` 生成并提交，前端客户端由 orval 生成；CI 校验两者未漂移。
- Huma 不读取嵌入结构体上的参数标签，需要的头部字段要在每个输入结构上直接声明（M0 已踩坑）。
