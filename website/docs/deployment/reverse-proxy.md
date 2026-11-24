---
title: 反向代理与 HTTPS
sidebar_position: 6
---

# 反向代理与 HTTPS

把整站反代到 SkinLoom 的 17546 端口即可，不需要区分路径。

## 先让 SkinLoom 信任代理

只有来自 `server.trusted_proxies` 的 `X-Forwarded-For`、`X-Forwarded-Proto`、`X-Forwarded-Host` 才会被采信。不配置时（默认）一律以直连的对端为准：这是安全的默认值，但站点会把代理的地址当成客户端 IP（登录限流、审计日志都会不准），并认为协议是 http（会话 Cookie 不会带 `Secure`）。

```toml
# data/config.toml，修改即时生效
[server]
# 代理与 SkinLoom 同机原生部署时填本机回环；
# 若 SkinLoom 运行在 Docker 容器中、代理运行在宿主机，容器内看到的是 Docker 网桥网关，需一并加入 172.16.0.0/12
trusted_proxies = ["127.0.0.1", "::1", "172.16.0.0/12"]
```

或通过环境变量配置：`SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,::1,172.16.0.0/12`。若使用 Docker Compose 且代理在本机，建议同时在 `.env` 设置 `SKINLOOM_BIND=127.0.0.1`，避免公网绕过 HTTPS 直连 `17546` 端口。随后设置站点地址：

```bash
skinloom settings set site.url '"https://skins.example.com"'
```

## Caddy（自动申请证书）

```caddyfile
skins.example.com {
    reverse_proxy 127.0.0.1:17546
}
```

## Nginx（宝塔、1Panel 的"反向代理"同理）

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    http2 on;
    server_name skins.example.com;
    # ssl_certificate / ssl_certificate_key …

    location / {
        proxy_pass http://127.0.0.1:17546;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;  # 支持插件 WebSocket 流代理
        proxy_buffering off;                              # 支持 /events 及迁移进度等 SSE 长连接推送
        proxy_read_timeout 1h;
        client_max_body_size 160m;                        # 兼容原生 .slp 插件离线包上传（最大 150 MiB）；仅需上传材质时可收紧为 16m
    }
}
```

> **宝塔面板 Docker 网站提示**：宝塔 v11 的「Docker → 网站 → 反代容器」会锁定主配置文件，如需调大上传限制或延长市场大包下载超时，在 **设置 → 配置文件 → 自定义配置文件（`server块`）** 中添加 `client_max_body_size 160m;` 与 `proxy_read_timeout 600s;` 保存即可。SkinLoom 的 OIDC / Yggdrasil Connect 发现端点位于 `/oauth/.well-known/openid-configuration`，不会被宝塔默认的 `location /.well-known` 证书验证规则拦截，无需额外修改反代配置。

## CDN

放在 CDN 后面时，把 CDN 回源节点的网段加入 `trusted_proxies`（多数 CDN 会公布回源 IP 列表），并确认 CDN 不缓存 `/api/*` 与 `/events`。
