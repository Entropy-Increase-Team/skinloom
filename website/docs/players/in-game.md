---
title: 在游戏里使用
sidebar_position: 4
---

# 在游戏里使用

让游戏显示你在皮肤站上的皮肤，有两种方式，选一种就行：

| 方式 | 适合 | 需要 |
|---|---|---|
| **外置登录**（推荐） | 进服务器、多人游戏 | 支持 authlib-injector 的启动器；服务器也要配置 |
| **皮肤 Mod** | 单机、离线服 | 安装 CustomSkinLoader 等 Mod |

下面的 `https://skins.example.com` 换成你所用皮肤站的地址。

## 外置登录

1. 在 HMCL、PCL2、BakaXL 等启动器里找到"添加外置登录 / 认证服务器"。
2. 把皮肤站首页或个人资料页的"拖到启动器"拖进去；或者粘贴地址 `https://skins.example.com/api/yggdrasil`（只填站点地址通常也行）。
3. 用皮肤站的**邮箱**或**角色名**和密码登录：
   - 用角色名登录，就以这个角色进游戏；
   - 用邮箱登录，账号有多个角色时启动器会让你选。
4. 进游戏后显示的就是这个角色在皮肤站上穿的皮肤。在站点换了皮肤，重新进服就能看到。

部分启动器支持直接上传皮肤：上传的材质会作为私有材质放进你的衣柜，并穿到当前角色上。

改密码或在个人资料页"退出所有启动器"后，启动器需要重新登录。

## 皮肤 Mod

**CustomSkinLoader**：编辑 `.minecraft/CustomSkinLoader/CustomSkinLoader.json`，在 `loadlist` 的最前面加一项：

```json
{
  "name": "SkinLoom",
  "type": "CustomSkinAPI",
  "root": "https://skins.example.com/"
}
```

**UniSkinMod**：添加根地址 `https://skins.example.com/usm/`。

**只能填图片地址的客户端**：皮肤 `https://skins.example.com/skin/<角色名>.png`，披风 `https://skins.example.com/cape/<角色名>.png`。

游戏里的名字必须和皮肤站上的角色名一致（不区分大小写）。

## 服主：让服务器使用外置登录

1. 下载 [authlib-injector](https://github.com/yushijinhun/authlib-injector/releases)，放到服务器目录。
2. 启动参数加上（放在 `-jar` 之前）：

   ```bash
   java -javaagent:authlib-injector.jar=https://skins.example.com/api/yggdrasil -jar server.jar
   ```

3. `server.properties` 里设置 `online-mode=true`。

用 BungeeCord、Velocity 等代理时，authlib-injector 加在**代理**上并开启正版验证，后端服务器按代理的要求关闭正版验证。

## 常见问题

- **皮肤不显示**：先在皮肤站确认角色穿上了皮肤；再确认游戏里的名字和角色名一致。客户端可能缓存约一分钟。
- **启动器提示账号被封禁**：请联系站点管理员。
- **从 Blessing Skin 迁移过来的站点**：启动器里需要重新登录一次，角色的 UUID 不变。
