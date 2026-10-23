---
title: 文档与日志
sidebar_position: 3
---

# 文档与日志怎么写

完整规则见 [设计文档 06](/design/06-docs-and-logs)。日常要记住的：

| 写什么 | 放哪 | 什么时候 |
|---|---|---|
| 用户、站长、插件开发者能感知的变化 | `CHANGELOG.md` 的 Unreleased 段 | 每个相关 PR |
| 开发过程、决策来龙去脉、踩坑、数据 | `website/devlog/YYYY-MM-DD-slug.md` | 每两周、每个里程碑结束 |
| 选型、跨模块接口、公共 API 的不兼容变化 | `docs/adr/NNNN-title.md` | 做出决定时 |
| 使用、部署、开发、插件文档 | `website/docs/` | 功能合并时同步更新 |

## 开发日志模板

```markdown
---
title: 第 N 期 · 一句话标题
slug: 英文短名
date: YYYY-MM-DD
tags: [devlog, M1]
---
## 本期完成
## 进行中
## 决策（附 ADR 链接）
## 问题与风险
## 数据（可选）
## 下期计划
```

## ADR 模板

```markdown
# NNNN 标题
- 状态：已接受（YYYY-MM-DD）
- 相关：issue / PR / 前置 ADR
## 背景
## 决策
## 备选方案与取舍
## 影响
```
