package plugin

import (
	"errors"
	"fmt"
)

// RiskTier classifies plugin permission severity for installation UI and policy checks.
type RiskTier string

const (
	RiskNormal    RiskTier = "normal"
	RiskSensitive RiskTier = "sensitive"
	RiskHighRisk  RiskTier = "high_risk"
)

const (
	// Normal permissions
	PermStorageKV    = "storage.kv"
	PermStorageDocs  = "storage.docs"
	PermStorageFiles = "storage.files"
	PermCron         = "cron"
	PermAdminUI      = "admin.ui"
	PermUsersRead    = "users.read"
	PermPlayersRead  = "players.read"
	PermTexturesRead = "textures.read"
	PermNotifySend   = "notify.send"

	// Sensitive permissions
	PermHTTPFetch       = "http.fetch"
	PermMailSend        = "mail.send"
	PermPlayersWrite    = "players.write"
	PermTexturesWrite   = "textures.write"
	PermClosetWrite     = "closet.write"
	PermScoreWrite      = "score.write"
	PermUsersWrite      = "users.write"
	PermRoutesPublic    = "routes.public"
	PermYggdrasilExtend = "yggdrasil.extend"
	PermCDNProvider     = "cdn.provider"
	PermCaptchaProvider = "captcha.provider"
	PermStorageSQL      = "storage.sql"
	PermHTTPStream      = "http.stream"
	PermProviderSearch  = "provider.search"
	PermProviderRender  = "provider.render"

	// High-risk permissions
	PermUsersBan        = "users.ban"
	PermAuthProvider    = "auth.provider"
	PermAuthPassword    = "auth.password"
	PermProviderStorage = "provider.storage"
	PermNativeExec      = "native.exec"
)

// PermissionDef describes a known capability permission and its risk tier.
type PermissionDef struct {
	Name         string        `json:"name"`
	Tier         RiskTier      `json:"tier"`
	Description  string        `json:"description"`
	Descriptions LocalizedText `json:"-"`
	NativeOnly   bool          `json:"nativeOnly,omitempty"`
}

// DescriptionForLocale returns the localized human-readable permission description.
func (d PermissionDef) DescriptionForLocale(locale string) string {
	if len(d.Descriptions) > 0 {
		if v := d.Descriptions.Resolve(locale); v != "" {
			return v
		}
	}
	return d.Description
}

var permissionCatalog = map[string]PermissionDef{
	PermStorageKV: {
		Name: PermStorageKV, Tier: RiskNormal,
		Description:  "Read and write plugin-scoped key/value data.",
		Descriptions: LocalizedText{"zh-CN": "读写插件专属的键值（KV）存储数据。", "en": "Read and write plugin-scoped key/value data."},
	},
	PermStorageDocs: {
		Name: PermStorageDocs, Tier: RiskNormal,
		Description:  "Read and write plugin-scoped structured documents.",
		Descriptions: LocalizedText{"zh-CN": "读写插件专属的结构化文档集合数据。", "en": "Read and write plugin-scoped structured documents."},
	},
	PermStorageFiles: {
		Name: PermStorageFiles, Tier: RiskNormal,
		Description:  "Read and write files in the plugin's private file space.",
		Descriptions: LocalizedText{"zh-CN": "在插件隔离私有文件目录中读写文件。", "en": "Read and write files in the plugin's private file space."},
	},
	PermCron: {
		Name: PermCron, Tier: RiskNormal,
		Description:  "Schedule recurring background jobs.",
		Descriptions: LocalizedText{"zh-CN": "注册并执行周期性后台定时任务。", "en": "Schedule recurring background jobs."},
	},
	PermAdminUI: {
		Name: PermAdminUI, Tier: RiskNormal,
		Description:  "Register custom pages under the admin panel.",
		Descriptions: LocalizedText{"zh-CN": "在管理后台注册自定义管理页面与导航入口。", "en": "Register custom pages under the admin panel."},
	},
	PermUsersRead: {
		Name: PermUsersRead, Tier: RiskNormal,
		Description:  "Query basic account information.",
		Descriptions: LocalizedText{"zh-CN": "查询站点用户的基本账号信息。", "en": "Query basic account information."},
	},
	PermPlayersRead: {
		Name: PermPlayersRead, Tier: RiskNormal,
		Description:  "Query player profiles and worn textures.",
		Descriptions: LocalizedText{"zh-CN": "查询游戏角色资料及当前穿戴的皮肤/披风。", "en": "Query player profiles and worn textures."},
	},
	PermTexturesRead: {
		Name: PermTexturesRead, Tier: RiskNormal,
		Description:  "Read skin and cape metadata or image bytes.",
		Descriptions: LocalizedText{"zh-CN": "读取皮肤与披风的元数据或图像内容。", "en": "Read skin and cape metadata or image bytes."},
	},
	PermNotifySend: {
		Name: PermNotifySend, Tier: RiskNormal,
		Description:  "Send in-site notifications or announcements.",
		Descriptions: LocalizedText{"zh-CN": "向用户发送站内通知或系统提醒。", "en": "Send in-site notifications or announcements."},
	},
	PermHTTPFetch: {
		Name: PermHTTPFetch, Tier: RiskSensitive,
		Description:  "Make outbound HTTP requests to declared http.allow hosts.",
		Descriptions: LocalizedText{"zh-CN": "向清单 http.allow 声明的白名单域名发起出站 HTTP 请求。", "en": "Make outbound HTTP requests to declared http.allow hosts."},
	},
	PermMailSend: {
		Name: PermMailSend, Tier: RiskSensitive,
		Description:  "Send emails via the host mail queue.",
		Descriptions: LocalizedText{"zh-CN": "通过站点邮件队列向用户发送电子邮件。", "en": "Send emails via the host mail queue."},
	},
	PermPlayersWrite: {
		Name: PermPlayersWrite, Tier: RiskSensitive,
		Description:  "Create, rename, equip, or delete players.",
		Descriptions: LocalizedText{"zh-CN": "创建、重命名、换装或删除用户的游戏角色。", "en": "Create, rename, equip, or delete players."},
	},
	PermTexturesWrite: {
		Name: PermTexturesWrite, Tier: RiskSensitive,
		Description:  "Create, edit, or delete textures.",
		Descriptions: LocalizedText{"zh-CN": "上传、编辑或删除皮肤与披风材质。", "en": "Create, edit, or delete textures."},
	},
	PermClosetWrite: {
		Name: PermClosetWrite, Tier: RiskSensitive,
		Description:  "Modify user closets.",
		Descriptions: LocalizedText{"zh-CN": "添加、重命名或移除用户衣柜中的材质。", "en": "Modify user closets."},
	},
	PermScoreWrite: {
		Name: PermScoreWrite, Tier: RiskSensitive,
		Description:  "Credit or debit user score balances through the ledger.",
		Descriptions: LocalizedText{"zh-CN": "通过积分账本增加或扣除用户的积分余额。", "en": "Credit or debit user score balances through the ledger."},
	},
	PermUsersWrite: {
		Name: PermUsersWrite, Tier: RiskSensitive,
		Description:  "Update limited user profile fields or preferences.",
		Descriptions: LocalizedText{"zh-CN": "修改受限的用户资料字段或语言偏好。", "en": "Update limited user profile fields or preferences."},
	},
	PermRoutesPublic: {
		Name: PermRoutesPublic, Tier: RiskSensitive,
		Description:  "Mount public routes at site root patterns.",
		Descriptions: LocalizedText{"zh-CN": "在站点根路径下挂载公开访问的 HTTP 路由。", "en": "Mount public routes at site root patterns."},
	},
	PermYggdrasilExtend: {
		Name: PermYggdrasilExtend, Tier: RiskSensitive,
		Description:  "Extend Yggdrasil profiles and metadata.",
		Descriptions: LocalizedText{"zh-CN": "扩展外置登录（Yggdrasil）角色属性与服务端元数据。", "en": "Extend Yggdrasil profiles and metadata."},
	},
	PermCDNProvider: {
		Name: PermCDNProvider, Tier: RiskSensitive,
		Description:  "Register a CDN cache purger provider.",
		Descriptions: LocalizedText{"zh-CN": "注册自定义 CDN 缓存刷新提供方。", "en": "Register a CDN cache purger provider."},
	},
	PermCaptchaProvider: {
		Name: PermCaptchaProvider, Tier: RiskSensitive,
		Description:  "Register a CAPTCHA verification provider.",
		Descriptions: LocalizedText{"zh-CN": "注册自定义人机验证码校验提供方。", "en": "Register a CAPTCHA verification provider."},
	},
	PermStorageSQL: {
		Name: PermStorageSQL, Tier: RiskSensitive, NativeOnly: true,
		Description:  "Access the host-allocated isolated SQL database space (plg_<id>_ prefix).",
		Descriptions: LocalizedText{"zh-CN": "访问宿主分配的插件专属隔离 SQL 数据库空间（plg_<id>_ 前缀）。", "en": "Access the host-allocated isolated SQL database space (plg_<id>_ prefix)."},
	},
	PermHTTPStream: {
		Name: PermHTTPStream, Tier: RiskSensitive, NativeOnly: true,
		Description:  "Expose a local HTTP/WebSocket streaming reverse proxy endpoint.",
		Descriptions: LocalizedText{"zh-CN": "暴露本地 HTTP/SSE/WebSocket 流式反向代理接口。", "en": "Expose a local HTTP/WebSocket streaming reverse proxy endpoint."},
	},
	PermProviderSearch: {
		Name: PermProviderSearch, Tier: RiskSensitive, NativeOnly: true,
		Description:  "Register a custom full-text texture search engine provider.",
		Descriptions: LocalizedText{"zh-CN": "注册自定义全文材质检索引擎提供方。", "en": "Register a custom full-text texture search engine provider."},
	},
	PermProviderRender: {
		Name: PermProviderRender, Tier: RiskSensitive, NativeOnly: true,
		Description:  "Register a custom 2D/3D avatar and texture preview render provider.",
		Descriptions: LocalizedText{"zh-CN": "注册自定义 2D/3D 头像与材质预览渲染提供方。", "en": "Register a custom 2D/3D avatar and texture preview render provider."},
	},
	PermUsersBan: {
		Name: PermUsersBan, Tier: RiskHighRisk,
		Description:  "Ban or unban user accounts.",
		Descriptions: LocalizedText{"zh-CN": "封禁或解封站点用户账号。", "en": "Ban or unban user accounts."},
	},
	PermAuthProvider: {
		Name: PermAuthProvider, Tier: RiskHighRisk,
		Description:  "Register a login / sign-in identity provider.",
		Descriptions: LocalizedText{"zh-CN": "注册第三方登录或身份认证提供方。", "en": "Register a login / sign-in identity provider."},
	},
	PermAuthPassword: {
		Name: PermAuthPassword, Tier: RiskHighRisk,
		Description:  "Register an external password verification algorithm.",
		Descriptions: LocalizedText{"zh-CN": "注册外部历史密码校验算法。", "en": "Register an external password verification algorithm."},
	},
	PermProviderStorage: {
		Name: PermProviderStorage, Tier: RiskHighRisk, NativeOnly: true,
		Description:  "Register a custom texture object storage driver provider.",
		Descriptions: LocalizedText{"zh-CN": "注册自定义材质对象存储后端驱动。", "en": "Register a custom texture object storage driver provider."},
	},
	PermNativeExec: {
		Name: PermNativeExec, Tier: RiskHighRisk, NativeOnly: true,
		Description:  "Execute as a native OS subprocess outside the WASM sandbox.",
		Descriptions: LocalizedText{"zh-CN": "以独立操作系统原生子进程在 WASM 沙箱之外执行。", "en": "Execute as a native OS subprocess outside the WASM sandbox."},
	},
}

// ErrPermissionDenied indicates a plugin invoked a capability it lacks permission for.
var ErrPermissionDenied = errors.New("permission denied")

// LookupPermission resolves a declared permission constant and its risk tier.
func LookupPermission(name string) (PermissionDef, bool) {
	p, ok := permissionCatalog[name]
	return p, ok
}

// AllPermissions returns a copy of all recognized permission definitions.
func AllPermissions() map[string]PermissionDef {
	out := make(map[string]PermissionDef, len(permissionCatalog))
	for k, v := range permissionCatalog {
		out[k] = v
	}
	return out
}

// PermissionSet tracks both declared (manifest.permissions) and runtime-granted
// permissions for a plugin instance.
type PermissionSet struct {
	declared map[string]bool
	granted  map[string]bool
}

// NewPermissionSet creates a PermissionSet. If granted is nil, declared
// permissions default to granted (local/auto-approved development loading);
// otherwise runtime requires perm in BOTH declared and granted.
func NewPermissionSet(declared, granted []string) *PermissionSet {
	dMap := make(map[string]bool, len(declared))
	for _, p := range declared {
		dMap[p] = true
	}
	gMap := make(map[string]bool, len(declared))
	if granted == nil {
		for _, p := range declared {
			gMap[p] = true
		}
	} else {
		for _, p := range granted {
			gMap[p] = true
		}
	}
	return &PermissionSet{declared: dMap, granted: gMap}
}

// Check verifies perm is both declared and granted, returning ErrPermissionDenied if not.
func (ps *PermissionSet) Check(perm string) error {
	if ps == nil || !ps.declared[perm] || !ps.granted[perm] {
		return fmt.Errorf("%w: %s", ErrPermissionDenied, perm)
	}
	return nil
}

// Has reports whether perm is enabled for this plugin.
func (ps *PermissionSet) Has(perm string) bool {
	return ps != nil && ps.declared[perm] && ps.granted[perm]
}
