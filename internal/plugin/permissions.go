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

	// High-risk permissions
	PermUsersBan     = "users.ban"
	PermAuthProvider = "auth.provider"
	PermAuthPassword = "auth.password"
)

// PermissionDef describes a known capability permission and its risk tier.
type PermissionDef struct {
	Name        string   `json:"name"`
	Tier        RiskTier `json:"tier"`
	Description string   `json:"description"`
}

var permissionCatalog = map[string]PermissionDef{
	PermStorageKV:       {PermStorageKV, RiskNormal, "Read and write plugin-scoped key/value data."},
	PermStorageDocs:     {PermStorageDocs, RiskNormal, "Read and write plugin-scoped structured documents."},
	PermStorageFiles:    {PermStorageFiles, RiskNormal, "Read and write files in the plugin's private file space."},
	PermCron:            {PermCron, RiskNormal, "Schedule recurring background jobs."},
	PermAdminUI:         {PermAdminUI, RiskNormal, "Register custom pages under the admin panel."},
	PermUsersRead:       {PermUsersRead, RiskNormal, "Query basic account information."},
	PermPlayersRead:     {PermPlayersRead, RiskNormal, "Query player profiles and worn textures."},
	PermTexturesRead:    {PermTexturesRead, RiskNormal, "Read skin and cape metadata or image bytes."},
	PermNotifySend:      {PermNotifySend, RiskNormal, "Send in-site notifications or announcements."},
	PermHTTPFetch:       {PermHTTPFetch, RiskSensitive, "Make outbound HTTP requests to declared http.allow hosts."},
	PermMailSend:        {PermMailSend, RiskSensitive, "Send emails via the host mail queue."},
	PermPlayersWrite:    {PermPlayersWrite, RiskSensitive, "Create, rename, equip, or delete players."},
	PermTexturesWrite:   {PermTexturesWrite, RiskSensitive, "Create, edit, or delete textures."},
	PermClosetWrite:     {PermClosetWrite, RiskSensitive, "Modify user closets."},
	PermScoreWrite:      {PermScoreWrite, RiskSensitive, "Credit or debit user score balances through the ledger."},
	PermUsersWrite:      {PermUsersWrite, RiskSensitive, "Update limited user profile fields or preferences."},
	PermRoutesPublic:    {PermRoutesPublic, RiskSensitive, "Mount public routes at site root patterns."},
	PermYggdrasilExtend: {PermYggdrasilExtend, RiskSensitive, "Extend Yggdrasil profiles and metadata."},
	PermCDNProvider:     {PermCDNProvider, RiskSensitive, "Register a CDN cache purger provider."},
	PermCaptchaProvider: {PermCaptchaProvider, RiskSensitive, "Register a CAPTCHA verification provider."},
	PermUsersBan:        {PermUsersBan, RiskHighRisk, "Ban or unban user accounts."},
	PermAuthProvider:    {PermAuthProvider, RiskHighRisk, "Register a login / sign-in identity provider."},
	PermAuthPassword:    {PermAuthPassword, RiskHighRisk, "Register an external password verification algorithm."},
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
