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
	Name        string   `json:"name"`
	Tier        RiskTier `json:"tier"`
	Description string   `json:"description"`
	NativeOnly  bool     `json:"nativeOnly,omitempty"`
}

var permissionCatalog = map[string]PermissionDef{
	PermStorageKV:       {Name: PermStorageKV, Tier: RiskNormal, Description: "Read and write plugin-scoped key/value data."},
	PermStorageDocs:     {Name: PermStorageDocs, Tier: RiskNormal, Description: "Read and write plugin-scoped structured documents."},
	PermStorageFiles:    {Name: PermStorageFiles, Tier: RiskNormal, Description: "Read and write files in the plugin's private file space."},
	PermCron:            {Name: PermCron, Tier: RiskNormal, Description: "Schedule recurring background jobs."},
	PermAdminUI:         {Name: PermAdminUI, Tier: RiskNormal, Description: "Register custom pages under the admin panel."},
	PermUsersRead:       {Name: PermUsersRead, Tier: RiskNormal, Description: "Query basic account information."},
	PermPlayersRead:     {Name: PermPlayersRead, Tier: RiskNormal, Description: "Query player profiles and worn textures."},
	PermTexturesRead:    {Name: PermTexturesRead, Tier: RiskNormal, Description: "Read skin and cape metadata or image bytes."},
	PermNotifySend:      {Name: PermNotifySend, Tier: RiskNormal, Description: "Send in-site notifications or announcements."},
	PermHTTPFetch:       {Name: PermHTTPFetch, Tier: RiskSensitive, Description: "Make outbound HTTP requests to declared http.allow hosts."},
	PermMailSend:        {Name: PermMailSend, Tier: RiskSensitive, Description: "Send emails via the host mail queue."},
	PermPlayersWrite:    {Name: PermPlayersWrite, Tier: RiskSensitive, Description: "Create, rename, equip, or delete players."},
	PermTexturesWrite:   {Name: PermTexturesWrite, Tier: RiskSensitive, Description: "Create, edit, or delete textures."},
	PermClosetWrite:     {Name: PermClosetWrite, Tier: RiskSensitive, Description: "Modify user closets."},
	PermScoreWrite:      {Name: PermScoreWrite, Tier: RiskSensitive, Description: "Credit or debit user score balances through the ledger."},
	PermUsersWrite:      {Name: PermUsersWrite, Tier: RiskSensitive, Description: "Update limited user profile fields or preferences."},
	PermRoutesPublic:    {Name: PermRoutesPublic, Tier: RiskSensitive, Description: "Mount public routes at site root patterns."},
	PermYggdrasilExtend: {Name: PermYggdrasilExtend, Tier: RiskSensitive, Description: "Extend Yggdrasil profiles and metadata."},
	PermCDNProvider:     {Name: PermCDNProvider, Tier: RiskSensitive, Description: "Register a CDN cache purger provider."},
	PermCaptchaProvider: {Name: PermCaptchaProvider, Tier: RiskSensitive, Description: "Register a CAPTCHA verification provider."},
	PermStorageSQL:      {Name: PermStorageSQL, Tier: RiskSensitive, Description: "Access the host-allocated isolated SQL database space (plg_<id>_ prefix).", NativeOnly: true},
	PermHTTPStream:      {Name: PermHTTPStream, Tier: RiskSensitive, Description: "Expose a local HTTP/WebSocket streaming reverse proxy endpoint.", NativeOnly: true},
	PermProviderSearch:  {Name: PermProviderSearch, Tier: RiskSensitive, Description: "Register a custom full-text texture search engine provider.", NativeOnly: true},
	PermProviderRender:  {Name: PermProviderRender, Tier: RiskSensitive, Description: "Register a custom 2D/3D avatar and texture preview render provider.", NativeOnly: true},
	PermUsersBan:        {Name: PermUsersBan, Tier: RiskHighRisk, Description: "Ban or unban user accounts."},
	PermAuthProvider:    {Name: PermAuthProvider, Tier: RiskHighRisk, Description: "Register a login / sign-in identity provider."},
	PermAuthPassword:    {Name: PermAuthPassword, Tier: RiskHighRisk, Description: "Register an external password verification algorithm."},
	PermProviderStorage: {Name: PermProviderStorage, Tier: RiskHighRisk, Description: "Register a custom texture object storage driver provider.", NativeOnly: true},
	PermNativeExec:      {Name: PermNativeExec, Tier: RiskHighRisk, Description: "Execute as a native OS subprocess outside the WASM sandbox.", NativeOnly: true},
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
