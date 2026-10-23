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
trusted_proxies = ["127.0.0.1", "::1"]   # 代理在本机；在别的机器上就写它的 IP 或网段
```

或环境变量 `SKINLOOM_SERVER__TRUSTED_PROXIES=127.0.0.1,::1`。然后设置站点地址：

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
        proxy_buffering off;        # /events 是长连接推送：设置修改后已打开的页面自动刷新
        proxy_read_timeout 1h;
        client_max_body_size 10m;
    }
}
```

## CDN

放在 CDN 后面时，把 CDN 回源节点的网段加入 `trusted_proxies`（多数 CDN 会公布回源 IP 列表），并确认 CDN 不缓存 `/api/*` 与 `/events`。
