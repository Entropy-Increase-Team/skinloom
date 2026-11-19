package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplugin "github.com/Entropy-Increase-Team/skinloom/ent/plugin"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

const (
	// DefaultRegistryCacheTTL is the default in-memory and disk cache duration for index.v1.json.
	DefaultRegistryCacheTTL = 15 * time.Minute
	// DefaultRegistryPatrolInterval is the background check interval for blocked plugins.
	DefaultRegistryPatrolInterval = 6 * time.Hour
	// MaxRegistryIndexBytes caps index.v1.json payload size (16 MiB).
	MaxRegistryIndexBytes = 16 * 1024 * 1024
	// MaxRegistrySigBytes caps index.v1.json.sig payload size (4 KiB).
	MaxRegistrySigBytes = 4 * 1024
)

var (
	// ErrRegistrySignatureInvalid indicates index.v1.json failed Ed25519 signature verification.
	ErrRegistrySignatureInvalid = errors.New("registry index signature verification failed")
	// ErrMarketPackageNotFound indicates the requested plugin/theme or version was not found in the market.
	ErrMarketPackageNotFound = errors.New("market package not found")
	// ErrMarketPackageBlocked indicates the requested plugin/theme version is on the registry blocklist.
	ErrMarketPackageBlocked = errors.New("market package is blocked by security policy")
	// ErrMarketPackageIncompatible indicates the requested package is incompatible with the host environment.
	ErrMarketPackageIncompatible = errors.New("market package is incompatible with host environment")
)

// RegistryIndex models the signed index.v1.json document published by a SkinLoom market registry.
type RegistryIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	GeneratedAt   string            `json:"generatedAt"`
	RegistryID    string            `json:"registryId"`
	Mirrors       []string          `json:"mirrors,omitempty"`
	Packages      []RegistryPackage `json:"packages"`
	Blocked       []RegistryBlocked `json:"blocked,omitempty"`
}

// CachedRegistryIndex holds a verified registry index alongside fetch telemetry.
type CachedRegistryIndex struct {
	Source       settings.RegistrySource `json:"source"`
	Index        *RegistryIndex          `json:"index"`
	RawBytes     []byte                  `json:"-"`
	Signature    string                  `json:"signature,omitempty"`
	Verified     bool                    `json:"verified"`
	Official     bool                    `json:"official"`
	ActiveMirror string                  `json:"activeMirror"`
	FetchedAt    time.Time               `json:"fetchedAt"`
}

type diskCachedRegistryMeta struct {
	ActiveMirror string    `json:"activeMirror"`
	FetchedAt    time.Time `json:"fetchedAt"`
	Verified     bool      `json:"verified"`
	Official     bool      `json:"official"`
}

// MarketHostEnv summarizes the running SkinLoom server's environment for market filtering.
type MarketHostEnv struct {
	OS                    string `json:"os"`
	Arch                  string `json:"arch"`
	Platform              string `json:"platform"`
	SkinloomVersion       string `json:"skinloomVersion"`
	GoVersion             string `json:"goVersion"`
	APIVersion            int    `json:"apiVersion"`
	NativeProtocolVersion int    `json:"nativeProtocolVersion"`
	AllowUnsignedNative   bool   `json:"allowUnsignedNative"`
}

// MarketRegistryStatus reports the live status of one configured market registry source.
type MarketRegistryStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Official     bool   `json:"official"`
	Enabled      bool   `json:"enabled"`
	Verified     bool   `json:"verified"`
	ActiveMirror string `json:"activeMirror,omitempty"`
	FetchedAt    string `json:"fetchedAt,omitempty"`
	PackageCount int    `json:"packageCount"`
	Error        string `json:"error,omitempty"`
}

// MarketVersionView summarizes one published version of a market package and its compatibility.
type MarketVersionView struct {
	Version               string            `json:"version"`
	PublishedAt           string            `json:"publishedAt,omitempty"`
	Yanked                bool              `json:"yanked,omitempty"`
	Changelog             string            `json:"changelog,omitempty"`
	APIVersion            int               `json:"apiVersion"`
	NativeProtocolVersion int               `json:"nativeProtocolVersion,omitempty"`
	Engines               EnginesSpec       `json:"engines,omitempty"`
	Permissions           []string          `json:"permissions,omitempty"`
	Dependencies          map[string]string `json:"dependencies,omitempty"`
	OptionalDependencies  map[string]string `json:"optionalDependencies,omitempty"`
	Platforms             []string          `json:"platforms,omitempty"`
	Compatible            bool              `json:"compatible"`
	Status                string            `json:"status"`
	Issues                []CompatIssue     `json:"issues,omitempty"`
}

// MarketPackageView describes a plugin or theme in the market with resolved host compatibility.
type MarketPackageView struct {
	RegistryID              string               `json:"registryId"`
	RegistryName            string               `json:"registryName"`
	ID                      string               `json:"id"`
	Kind                    string               `json:"kind"`
	Runtime                 string               `json:"runtime"`
	Category                string               `json:"category,omitempty"`
	Official                bool                 `json:"official"`
	Name                    string               `json:"name"`
	RawName                 LocalizedText        `json:"rawName"`
	Description             string               `json:"description,omitempty"`
	RawDescription          LocalizedText        `json:"rawDescription,omitempty"`
	Authors                 []string             `json:"authors,omitempty"`
	License                 string               `json:"license,omitempty"`
	Homepage                string               `json:"homepage,omitempty"`
	Repository              string               `json:"repository,omitempty"`
	Icon                    string               `json:"icon,omitempty"`
	Latest                  string               `json:"latest"`
	Installed               bool                 `json:"installed"`
	InstalledVersion        string               `json:"installedVersion,omitempty"`
	InstalledStatus         string               `json:"installedStatus,omitempty"`
	UpgradeAvailable        bool                 `json:"upgradeAvailable"`
	Compatibility           PackageCompatSummary `json:"compatibility"`
	RecommendedArtifact     *RegistryArtifact    `json:"recommendedArtifact,omitempty"`
	RecommendedPlatform     string               `json:"recommendedPlatform,omitempty"`
	SupportedPlatforms      []string             `json:"supportedPlatforms,omitempty"`
	Permissions             PermissionGroups     `json:"permissions"`
	RequiresHighRiskConfirm bool                 `json:"requiresHighRiskConfirm"`
	Versions                []MarketVersionView  `json:"versions"`
}

// MarketFilter specifies search and filter criteria for listing market packages.
type MarketFilter struct {
	Query          string
	Kind           string // "" | "all" | "plugin" | "theme"
	Runtime        string // "" | "all" | "js" | "native"
	Category       string // "" | "all" | "recommended" | "official" | "security" | "score" | "migration" | "theme" | ...
	CompatibleOnly bool
	Locale         string
	ForceRefresh   bool
}

// MarketListResult contains the host environment, registry statuses, and filtered market packages.
type MarketListResult struct {
	HostEnv    MarketHostEnv          `json:"hostEnv"`
	Registries []MarketRegistryStatus `json:"registries"`
	Packages   []MarketPackageView    `json:"packages"`
}

// BlockedPluginAction records an installed plugin that was automatically disabled due to a registry block rule.
type BlockedPluginAction struct {
	PluginID   string `json:"pluginId"`
	Version    string `json:"version"`
	Reason     string `json:"reason"`
	RegistryID string `json:"registryId"`
}

// MarketRefreshResult returns the refreshed market state and any blocked plugins that were hot-disabled.
type MarketRefreshResult struct {
	HostEnv        MarketHostEnv          `json:"hostEnv"`
	Registries     []MarketRegistryStatus `json:"registries"`
	Packages       []MarketPackageView    `json:"packages"`
	BlockedActions []BlockedPluginAction  `json:"blockedActions,omitempty"`
}

// MarketDependencyInspectItem summarizes a transitive dependency that will be cascade-installed.
type MarketDependencyInspectItem struct {
	ID                      string           `json:"id"`
	Name                    string           `json:"name"`
	Version                 string           `json:"version"`
	Runtime                 string           `json:"runtime"`
	Official                bool             `json:"official"`
	Permissions             PermissionGroups `json:"permissions"`
	RequiresHighRiskConfirm bool             `json:"requiresHighRiskConfirm"`
}

// MarketInspectRequest identifies a market package and optional version to pre-inspect.
type MarketInspectRequest struct {
	RegistryID string `json:"registryId,omitempty"`
	ID         string `json:"id"`
	Version    string `json:"version,omitempty"`
}

// MarketInspectResult extends PackageInspectResult with market compatibility and cascade dependency info.
type MarketInspectResult struct {
	*PackageInspectResult
	RegistryID            string                        `json:"registryId"`
	OfficialRegistry      bool                          `json:"officialRegistry"`
	Compatibility         VersionCompatResult           `json:"compatibility"`
	DependencyPlan        *DependencyPlan               `json:"dependencyPlan,omitempty"`
	DependenciesToInstall []MarketDependencyInspectItem `json:"dependenciesToInstall,omitempty"`
}

// MarketInstallRequest specifies a market package to download, verify, and install (with optional cascade dependencies).
type MarketInstallRequest struct {
	RegistryID          string   `json:"registryId,omitempty"`
	ID                  string   `json:"id"`
	Version             string   `json:"version,omitempty"`
	GrantedPermissions  []string `json:"grantedPermissions,omitempty"`
	GrantAll            bool     `json:"grantAll,omitempty"`
	AllowUnsigned       bool     `json:"allowUnsigned,omitempty"`
	Disabled            bool     `json:"disabled,omitempty"`
	InstallDependencies *bool    `json:"installDependencies,omitempty"`
}

// MarketInstallResult returns the installed target plugin and any cascade-installed dependencies.
type MarketInstallResult struct {
	Plugin                *PluginInfo   `json:"plugin"`
	InstalledDependencies []*PluginInfo `json:"installedDependencies,omitempty"`
}

// RegistryClient manages multi-source index fetching, Ed25519 signature verification,
// 15-minute memory/disk caching, 5-dimensional compatibility filtering, blocked-plugin
// enforcement, and failover artifact downloading.
type RegistryClient struct {
	mgr        *Manager
	httpClient *http.Client
	cacheTTL   time.Duration
	patrolInt  time.Duration

	mu                      sync.RWMutex
	memCache                map[string]*CachedRegistryIndex
	officialPubKeySpec      string
	customRegistriesForTest []settings.RegistrySource

	patrolMu   sync.Mutex
	patrolStop chan struct{}
	patrolWg   *sync.WaitGroup
}

// NewRegistryClient creates a RegistryClient bound to mgr.
func NewRegistryClient(mgr *Manager) *RegistryClient {
	return &RegistryClient{
		mgr: mgr,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Host == "" {
					return fmt.Errorf("disallowed redirect URL scheme in %v", req.URL)
				}
				return nil
			},
		},
		cacheTTL:           DefaultRegistryCacheTTL,
		patrolInt:          DefaultRegistryPatrolInterval,
		memCache:           make(map[string]*CachedRegistryIndex),
		officialPubKeySpec: OfficialPublicKeyPEM,
	}
}

// Registry returns the manager's RegistryClient, initializing it lazily if needed.
func (m *Manager) Registry() *RegistryClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registry == nil {
		m.registry = NewRegistryClient(m)
	}
	return m.registry
}

// SetHTTPClient overrides the HTTP client used for registry and artifact requests.
func (c *RegistryClient) SetHTTPClient(hc *http.Client) {
	if hc == nil {
		return
	}
	c.mu.Lock()
	c.httpClient = hc
	c.mu.Unlock()
}

// SetCacheTTL overrides the index cache TTL (default 15 minutes).
func (c *RegistryClient) SetCacheTTL(ttl time.Duration) {
	c.mu.Lock()
	c.cacheTTL = ttl
	c.mu.Unlock()
}

// SetPatrolInterval overrides the periodic blocked-plugin patrol interval (default 6 hours).
func (c *RegistryClient) SetPatrolInterval(d time.Duration) {
	c.mu.Lock()
	c.patrolInt = d
	c.mu.Unlock()
}

// SetOfficialPublicKey overrides the built-in official Ed25519 public key (used by unit/E2E tests).
func (c *RegistryClient) SetOfficialPublicKey(pubKeySpec string) {
	c.mu.Lock()
	if strings.TrimSpace(pubKeySpec) == "" {
		c.officialPubKeySpec = OfficialPublicKeyPEM
	} else {
		c.officialPubKeySpec = pubKeySpec
	}
	c.memCache = make(map[string]*CachedRegistryIndex)
	c.mu.Unlock()
}

// SetRegistriesForTest overrides the configured registry sources when settings.Service is not wired.
func (c *RegistryClient) SetRegistriesForTest(sources []settings.RegistrySource) {
	c.mu.Lock()
	c.customRegistriesForTest = append([]settings.RegistrySource(nil), sources...)
	c.memCache = make(map[string]*CachedRegistryIndex)
	c.mu.Unlock()
}

func (c *RegistryClient) officialPublicKey() (ed25519.PublicKey, error) {
	c.mu.RLock()
	spec := c.officialPubKeySpec
	c.mu.RUnlock()
	if spec == "" {
		spec = OfficialPublicKeyPEM
	}
	return ParseEd25519PublicKey(spec)
}

// ConfiguredRegistries returns the list of configured registry sources from site settings
// (falling back to DefaultPluginRegistries).
func (c *RegistryClient) ConfiguredRegistries() []settings.RegistrySource {
	c.mu.RLock()
	testRegs := c.customRegistriesForTest
	c.mu.RUnlock()
	if len(testRegs) > 0 {
		return append([]settings.RegistrySource(nil), testRegs...)
	}
	if c.mgr != nil && c.mgr.settings != nil {
		snap := c.mgr.settings.Current()
		if snap != nil {
			if decoded := settings.Decode[[]settings.RegistrySource](snap, "plugin.registries"); len(decoded) > 0 {
				if parsed, err := settings.ParsePluginRegistries(decoded); err == nil && len(parsed) > 0 {
					return parsed
				}
			}
			if raw := settings.Decode[any](snap, "plugin.registries"); raw != nil {
				if parsed, err := settings.ParsePluginRegistries(raw); err == nil && len(parsed) > 0 {
					return parsed
				}
			}
		}
	}
	return settings.DefaultPluginRegistries()
}

// HostEnv builds the 5-dimensional HostEnv snapshot including all installed plugins/themes.
func (c *RegistryClient) HostEnv(ctx context.Context) (HostEnv, map[string]string) {
	env, installedStatus, _ := c.hostEnvWithRows(ctx)
	return env, installedStatus
}

func (c *RegistryClient) hostEnvWithRows(ctx context.Context) (HostEnv, map[string]string, map[string]*ent.Plugin) {
	installedVer := map[string]string{}
	installedStatus := map[string]string{}
	rowsByID := map[string]*ent.Plugin{}
	if c.mgr != nil && c.mgr.db != nil {
		if rows, err := c.mgr.db.Plugin.Query().All(ctx); err == nil {
			for _, r := range rows {
				installedVer[r.ID] = r.Version
				installedStatus[r.ID] = string(r.Status)
				rowsByID[r.ID] = r
			}
		}
	} else if c.mgr != nil {
		c.mgr.mu.RLock()
		for id, sb := range c.mgr.stored {
			if sb != nil && sb.manifest != nil {
				installedVer[id] = sb.manifest.Version
				installedStatus[id] = "disabled"
			}
		}
		for id, rt := range c.mgr.active {
			if rt != nil && rt.manifest != nil && !rt.closed.Load() {
				installedVer[id] = rt.manifest.Version
				installedStatus[id] = "enabled"
			}
		}
		c.mgr.mu.RUnlock()
	}

	env := CurrentHostEnv(installedVer)
	if c.mgr != nil && c.mgr.hostPlatformOverride != "" {
		if osPart, archPart, ok := strings.Cut(c.mgr.hostPlatformOverride, "/"); ok && osPart != "" && archPart != "" {
			env.OS = osPart
			env.Arch = archPart
		}
	}
	return env, installedStatus, rowsByID
}

// MarketHostEnvSummary returns the serializable host environment summary for API/UI responses.
func (c *RegistryClient) MarketHostEnvSummary(ctx context.Context) MarketHostEnv {
	env, _ := c.HostEnv(ctx)
	return c.marketHostEnvFrom(env)
}

func (c *RegistryClient) marketHostEnvFrom(env HostEnv) MarketHostEnv {
	allowUnsigned := false
	if c.mgr != nil {
		allowUnsigned = c.mgr.AllowUnsignedNative()
	}
	return MarketHostEnv{
		OS:                    env.OS,
		Arch:                  env.Arch,
		Platform:              env.Platform(),
		SkinloomVersion:       env.SkinloomVersion,
		GoVersion:             env.GoVersion,
		APIVersion:            env.APIVersion,
		NativeProtocolVersion: env.NativeProtocolVersion,
		AllowUnsignedNative:   allowUnsigned,
	}
}

// FetchSourceIndex fetches, verifies, and caches index.v1.json for a single RegistrySource.
func (c *RegistryClient) FetchSourceIndex(ctx context.Context, src settings.RegistrySource, forceRefresh bool) (*CachedRegistryIndex, error) {
	c.mu.RLock()
	ttl := c.cacheTTL
	cached := c.memCache[src.ID]
	c.mu.RUnlock()

	now := time.Now().UTC()
	if !forceRefresh && cached != nil && now.Sub(cached.FetchedAt) < ttl {
		return cached, nil
	}

	if !forceRefresh {
		if diskEntry, err := c.loadDiskCache(src); err == nil && diskEntry != nil && now.Sub(diskEntry.FetchedAt) < ttl {
			c.mu.Lock()
			c.memCache[src.ID] = diskEntry
			c.mu.Unlock()
			return diskEntry, nil
		}
	}

	if len(src.URLs) == 0 {
		return nil, fmt.Errorf("registry %q has no configured mirror URLs", src.ID)
	}

	var errs []string
	for _, mirror := range src.URLs {
		baseURL := strings.TrimRight(strings.TrimSpace(mirror), "/")
		if baseURL == "" {
			continue
		}
		entry, err := c.fetchAndVerifyFromMirror(ctx, src, baseURL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", baseURL, err))
			continue
		}
		c.mu.Lock()
		c.memCache[src.ID] = entry
		c.mu.Unlock()
		c.saveDiskCache(src.ID, entry)
		return entry, nil
	}

	return nil, fmt.Errorf("all mirrors failed for registry %q (%s)", src.ID, strings.Join(errs, "; "))
}

func (c *RegistryClient) fetchAndVerifyFromMirror(ctx context.Context, src settings.RegistrySource, baseURL string) (*CachedRegistryIndex, error) {
	indexURL := baseURL + "/index.v1.json"
	if strings.HasSuffix(baseURL, "/index.v1.json") {
		indexURL = baseURL
		baseURL = strings.TrimSuffix(baseURL, "/index.v1.json")
	}
	sigURL := indexURL + ".sig"

	rawIndex, err := c.httpGetBytes(ctx, indexURL, MaxRegistryIndexBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch index.v1.json: %w", err)
	}

	isOfficial := src.Official || src.ID == "official"
	requiresSig := isOfficial || strings.TrimSpace(src.PublicKey) != ""

	var sigText string
	rawSig, sigErr := c.httpGetBytes(ctx, sigURL, MaxRegistrySigBytes)
	if sigErr != nil {
		if requiresSig {
			return nil, fmt.Errorf("%w: fetch index.v1.json.sig: %v", ErrRegistrySignatureInvalid, sigErr)
		}
	} else {
		sigText = strings.TrimSpace(string(rawSig))
	}

	verified, officialVerified, err := c.verifyIndexSignature(src, rawIndex, sigText)
	if err != nil {
		return nil, err
	}

	var idx RegistryIndex
	if err := json.Unmarshal(rawIndex, &idx); err != nil {
		return nil, fmt.Errorf("decode index.v1.json: %w", err)
	}
	if idx.SchemaVersion != 0 && idx.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported registry index schemaVersion %d", idx.SchemaVersion)
	}
	if idx.RegistryID == "" {
		idx.RegistryID = src.ID
	}

	return &CachedRegistryIndex{
		Source:       src,
		Index:        &idx,
		RawBytes:     rawIndex,
		Signature:    sigText,
		Verified:     verified,
		Official:     officialVerified,
		ActiveMirror: baseURL,
		FetchedAt:    time.Now().UTC(),
	}, nil
}

func decodeEd25519Sig(sigText string) ([]byte, error) {
	s := strings.TrimSpace(sigText)
	if len(s) == ed25519.SignatureSize*2 {
		if raw, err := hex.DecodeString(s); err == nil && len(raw) == ed25519.SignatureSize {
			return raw, nil
		}
	}
	raw, err := decodeBase64Flexible(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid Ed25519 signature length %d", len(raw))
	}
	return raw, nil
}

func (c *RegistryClient) verifyIndexSignature(src settings.RegistrySource, rawIndex []byte, sigText string) (verified bool, official bool, err error) {
	isOfficial := src.Official || src.ID == "official"
	if isOfficial {
		if sigText == "" {
			return false, false, fmt.Errorf("%w: official registry index is missing signature", ErrRegistrySignatureInvalid)
		}
		sigBytes, err := decodeEd25519Sig(sigText)
		if err != nil {
			return false, false, fmt.Errorf("%w: %v", ErrRegistrySignatureInvalid, err)
		}
		pub, err := c.officialPublicKey()
		if err != nil {
			return false, false, fmt.Errorf("%w: parse official public key: %v", ErrRegistrySignatureInvalid, err)
		}
		if !ed25519.Verify(pub, rawIndex, sigBytes) {
			return false, false, fmt.Errorf("%w: official signature mismatch", ErrRegistrySignatureInvalid)
		}
		return true, true, nil
	}

	if strings.TrimSpace(src.PublicKey) != "" {
		if sigText == "" {
			return false, false, fmt.Errorf("%w: registry %q requires signature", ErrRegistrySignatureInvalid, src.ID)
		}
		sigBytes, err := decodeEd25519Sig(sigText)
		if err != nil {
			return false, false, fmt.Errorf("%w: %v", ErrRegistrySignatureInvalid, err)
		}
		pub, err := ParseEd25519PublicKey(src.PublicKey)
		if err != nil {
			return false, false, fmt.Errorf("%w: invalid registry publicKey: %v", ErrRegistrySignatureInvalid, err)
		}
		if !ed25519.Verify(pub, rawIndex, sigBytes) {
			return false, false, fmt.Errorf("%w: signature mismatch for registry %q", ErrRegistrySignatureInvalid, src.ID)
		}
		officialPub, oErr := c.officialPublicKey()
		isOffKey := oErr == nil && bytesEqualConstantTime(pub, officialPub)
		return true, isOffKey, nil
	}

	// Optional signature verification against trusted keys for custom registries without a pinned key.
	if sigText != "" {
		if sigBytes, err := decodeEd25519Sig(sigText); err == nil {
			for _, tk := range c.trustedKeysForVerification("") {
				if ed25519.Verify(tk.PublicKey, rawIndex, sigBytes) {
					return true, tk.Official, nil
				}
			}
		}
	}
	return false, false, nil
}

func bytesEqualConstantTime(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func (c *RegistryClient) trustedKeysForVerification(extraKey string) []TrustedKey {
	var specs []string
	if strings.TrimSpace(extraKey) != "" {
		specs = append(specs, strings.TrimSpace(extraKey))
	}
	if c.mgr != nil {
		specs = append(specs, c.mgr.allTrustedKeys()...)
	}
	keys := ResolveTrustedKeys(specs)
	if pub, err := c.officialPublicKey(); err == nil {
		fp := PublicKeyFingerprint(pub)
		found := false
		for i := range keys {
			if keys[i].Official {
				keys[i].PublicKey = pub
				keys[i].Fingerprint = fp
				found = true
				break
			}
		}
		if !found {
			keys = append([]TrustedKey{{
				Fingerprint: fp,
				Label:       "SkinLoom Official",
				Official:    true,
				PublicKey:   pub,
			}}, keys...)
		}
	}
	return keys
}

func (c *RegistryClient) httpGetBytes(ctx context.Context, targetURL string, maxBytes int64) ([]byte, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid registry http(s) URL %q", targetURL)
	}
	c.mu.RLock()
	hc := c.httpClient
	c.mu.RUnlock()
	if hc == nil {
		hc = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SkinLoom-RegistryClient/1.0")
	req.Header.Set("Accept", "application/json, application/octet-stream, text/plain, */*")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, targetURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response from %s exceeds %d byte limit", targetURL, maxBytes)
	}
	return data, nil
}

func (c *RegistryClient) cacheDirFor(registryID string) string {
	if c.mgr == nil || c.mgr.dataDir == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(registryID)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	safeID := b.String()
	if safeID == "" {
		safeID = "default"
	}
	return filepath.Join(c.mgr.dataDir, "cache", "registry", safeID)
}

func (c *RegistryClient) saveDiskCache(registryID string, entry *CachedRegistryIndex) {
	dir := c.cacheDirFor(registryID)
	if dir == "" || entry == nil || len(entry.RawBytes) == 0 {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "index.v1.json"), entry.RawBytes, 0o644)
	if entry.Signature != "" {
		_ = os.WriteFile(filepath.Join(dir, "index.v1.json.sig"), []byte(entry.Signature), 0o644)
	} else {
		_ = os.Remove(filepath.Join(dir, "index.v1.json.sig"))
	}
	metaBytes, err := json.Marshal(diskCachedRegistryMeta{
		ActiveMirror: entry.ActiveMirror,
		FetchedAt:    entry.FetchedAt,
		Verified:     entry.Verified,
		Official:     entry.Official,
	})
	if err == nil {
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0o644)
	}
}

func (c *RegistryClient) loadDiskCache(src settings.RegistrySource) (*CachedRegistryIndex, error) {
	dir := c.cacheDirFor(src.ID)
	if dir == "" {
		return nil, errors.New("no disk cache directory")
	}
	rawIndex, err := os.ReadFile(filepath.Join(dir, "index.v1.json"))
	if err != nil {
		return nil, err
	}
	rawMeta, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var meta diskCachedRegistryMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		return nil, err
	}
	var sigText string
	if rawSig, sErr := os.ReadFile(filepath.Join(dir, "index.v1.json.sig")); sErr == nil {
		sigText = strings.TrimSpace(string(rawSig))
	}
	verified, officialVerified, err := c.verifyIndexSignature(src, rawIndex, sigText)
	if err != nil {
		return nil, err
	}
	var idx RegistryIndex
	if err := json.Unmarshal(rawIndex, &idx); err != nil {
		return nil, err
	}
	return &CachedRegistryIndex{
		Source:       src,
		Index:        &idx,
		RawBytes:     rawIndex,
		Signature:    sigText,
		Verified:     verified,
		Official:     officialVerified,
		ActiveMirror: meta.ActiveMirror,
		FetchedAt:    meta.FetchedAt,
	}, nil
}

// FetchAllIndexes fetches all configured and enabled registries, returning their statuses,
// merged catalog, and verified blocklist rules.
func (c *RegistryClient) FetchAllIndexes(ctx context.Context, forceRefresh bool) (
	entries []*CachedRegistryIndex,
	statuses []MarketRegistryStatus,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
	blockedSource map[string]string,
) {
	sources := c.ConfiguredRegistries()
	catalog = make(map[string]RegistryPackage)
	blocked = make(map[string]RegistryBlocked)
	blockedSource = make(map[string]string)

	for _, src := range sources {
		st := MarketRegistryStatus{
			ID:       src.ID,
			Name:     src.Name,
			Official: src.Official || src.ID == "official",
			Enabled:  src.Enabled,
		}
		if !src.Enabled {
			statuses = append(statuses, st)
			continue
		}
		entry, err := c.FetchSourceIndex(ctx, src, forceRefresh)
		if err != nil {
			st.Error = err.Error()
			statuses = append(statuses, st)
			continue
		}
		st.Verified = entry.Verified
		st.ActiveMirror = entry.ActiveMirror
		st.FetchedAt = entry.FetchedAt.UTC().Format(time.RFC3339)
		if entry.Index != nil {
			st.PackageCount = len(entry.Index.Packages)
			for _, pkg := range entry.Index.Packages {
				// Official or earlier registry takes precedence in cross-registry catalog lookup.
				if _, exists := catalog[pkg.ID]; !exists {
					catalog[pkg.ID] = pkg
				}
			}
			// Only signature-verified (official or pinned-key) registries may contribute blocklist rules.
			if entry.Verified {
				for _, b := range entry.Index.Blocked {
					if b.ID == "" {
						continue
					}
					if prev, exists := blocked[b.ID]; exists {
						blocked[b.ID] = mergeRegistryBlockedRule(prev, b)
					} else {
						blocked[b.ID] = b
						blockedSource[b.ID] = src.ID
					}
				}
			}
		}
		entries = append(entries, entry)
		statuses = append(statuses, st)
	}
	return entries, statuses, catalog, blocked, blockedSource
}

func mergeRegistryBlockedRule(prev, next RegistryBlocked) RegistryBlocked {
	pv := strings.TrimSpace(prev.Versions)
	nv := strings.TrimSpace(next.Versions)
	var mergedVer string
	if pv == "" || pv == "*" || nv == "" || nv == "*" {
		mergedVer = "*"
	} else if pv == nv {
		mergedVer = pv
	} else {
		mergedVer = pv + " || " + nv
	}
	mergedReason := strings.TrimSpace(prev.Reason)
	nextReason := strings.TrimSpace(next.Reason)
	if nextReason != "" && !strings.Contains(mergedReason, nextReason) {
		if mergedReason == "" {
			mergedReason = nextReason
		} else {
			mergedReason = mergedReason + "; " + nextReason
		}
	}
	return RegistryBlocked{
		ID:       prev.ID,
		Versions: mergedVer,
		Reason:   mergedReason,
	}
}

// ListPackages fetches market indexes and evaluates 5-dimensional compatibility and filters.
func (c *RegistryClient) ListPackages(ctx context.Context, filter MarketFilter) (*MarketListResult, error) {
	entries, statuses, catalog, blocked, _ := c.FetchAllIndexes(ctx, filter.ForceRefresh)
	env, installedStatus, rowsByID := c.hostEnvWithRows(ctx)
	hostSummary := c.marketHostEnvFrom(env)

	locale := strings.TrimSpace(filter.Locale)
	if locale == "" {
		locale = "zh-CN"
	}

	var views []MarketPackageView
	for _, entry := range entries {
		if entry.Index == nil {
			continue
		}
		for _, pkg := range entry.Index.Packages {
			view := c.buildMarketPackageView(ctx, entry, pkg, env, installedStatus, rowsByID, catalog, blocked, locale)
			if !matchesMarketFilter(view, filter) {
				continue
			}
			views = append(views, view)
		}
	}

	// Sort packages: official first, compatible first, then by ID.
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Official != views[j].Official {
			return views[i].Official
		}
		if views[i].Compatibility.Compatible != views[j].Compatibility.Compatible {
			return views[i].Compatibility.Compatible
		}
		return views[i].ID < views[j].ID
	})

	if views == nil {
		views = []MarketPackageView{}
	}
	if statuses == nil {
		statuses = []MarketRegistryStatus{}
	}

	return &MarketListResult{
		HostEnv:    hostSummary,
		Registries: statuses,
		Packages:   views,
	}, nil
}

func (c *RegistryClient) buildMarketPackageView(
	ctx context.Context,
	entry *CachedRegistryIndex,
	pkg RegistryPackage,
	env HostEnv,
	installedStatus map[string]string,
	rowsByID map[string]*ent.Plugin,
	catalog map[string]RegistryPackage,
	blocked map[string]RegistryBlocked,
	locale string,
) MarketPackageView {
	compat := SelectBestVersion(pkg, env, catalog, blocked)
	instVer := env.Installed[pkg.ID]
	instStat := installedStatus[pkg.ID]
	isInstalled := instVer != ""

	var recArt *RegistryArtifact
	var recPlat string
	var targetPermissions []string
	if compat.Recommended != nil {
		recArt = compat.Recommended.Artifact
		recPlat = compat.Recommended.ArtifactKey
	}

	// Build version views sorted descending by SemVer.
	sortedVers := append([]RegistryVersion(nil), pkg.Versions...)
	sort.SliceStable(sortedVers, func(i, j int) bool {
		vi, errI := ParseSemver(sortedVers[i].Version)
		vj, errJ := ParseSemver(sortedVers[j].Version)
		if errI == nil && errJ == nil {
			return vi.Compare(vj) > 0
		}
		return sortedVers[i].Version > sortedVers[j].Version
	})

	versionViews := make([]MarketVersionView, 0, len(sortedVers))
	for _, v := range sortedVers {
		vCompat := CheckVersionCompatWithBlocked(pkg, v, env, catalog, blocked)
		if len(targetPermissions) == 0 && (v.Version == compat.RecommendedVersion || v.Version == pkg.Latest) {
			targetPermissions = append([]string(nil), v.Permissions...)
		}
		plats := make([]string, 0, len(v.Artifacts))
		for k := range v.Artifacts {
			plats = append(plats, k)
		}
		sort.Strings(plats)
		versionViews = append(versionViews, MarketVersionView{
			Version:               v.Version,
			PublishedAt:           v.PublishedAt,
			Yanked:                v.Yanked,
			Changelog:             v.Changelog,
			APIVersion:            v.APIVersion,
			NativeProtocolVersion: v.NativeProtocolVersion,
			Engines:               v.Engines,
			Permissions:           append([]string(nil), v.Permissions...),
			Dependencies:          maps.Clone(v.Dependencies),
			OptionalDependencies:  maps.Clone(v.OptionalDependencies),
			Platforms:             plats,
			Compatible:            vCompat.Compatible,
			Status:                vCompat.Status,
			Issues:                vCompat.Issues,
		})
	}
	if len(targetPermissions) == 0 && len(sortedVers) > 0 {
		targetPermissions = append([]string(nil), sortedVers[0].Permissions...)
	}
	if pkg.EffectiveRuntime() == RuntimeNative && !slices.Contains(targetPermissions, PermNativeExec) {
		targetPermissions = append(targetPermissions, PermNativeExec)
	}

	var prevDeclared, prevGranted []string
	if isInstalled && c.mgr != nil {
		if row, ok := rowsByID[pkg.ID]; ok && row != nil {
			prevGranted = append([]string(nil), row.GrantedPermissions...)
			if mf := c.mgr.manifestForRow(ctx, pkg.ID); mf != nil {
				prevDeclared = append([]string(nil), mf.EffectivePermissions()...)
			}
		}
	}
	permGroups, needsHighRisk := ClassifyPermissions(targetPermissions, prevDeclared, prevGranted, isInstalled)

	latest := pkg.Latest
	if latest == "" && len(sortedVers) > 0 {
		latest = sortedVers[0].Version
	}
	kind := pkg.Kind
	if kind == "" {
		kind = KindPlugin
	}

	return MarketPackageView{
		RegistryID:              entry.Source.ID,
		RegistryName:            entry.Source.Name,
		ID:                      pkg.ID,
		Kind:                    kind,
		Runtime:                 pkg.EffectiveRuntime(),
		Category:                pkg.Category,
		Official:                pkg.Official && entry.Official,
		Name:                    pkg.Name.Resolve(locale),
		RawName:                 pkg.Name,
		Description:             pkg.Description.Resolve(locale),
		RawDescription:          pkg.Description,
		Authors:                 append([]string(nil), pkg.Authors...),
		License:                 pkg.License,
		Homepage:                pkg.Homepage,
		Repository:              pkg.Repository,
		Icon:                    pkg.Icon,
		Latest:                  latest,
		Installed:               isInstalled,
		InstalledVersion:        instVer,
		InstalledStatus:         instStat,
		UpgradeAvailable:        compat.UpgradeAvailable,
		Compatibility:           compat,
		RecommendedArtifact:     recArt,
		RecommendedPlatform:     recPlat,
		SupportedPlatforms:      compat.SupportedPlatforms,
		Permissions:             permGroups,
		RequiresHighRiskConfirm: needsHighRisk,
		Versions:                versionViews,
	}
}

func matchesMarketFilter(v MarketPackageView, f MarketFilter) bool {
	if f.CompatibleOnly && !v.Compatibility.Compatible {
		return false
	}
	if kind := strings.ToLower(strings.TrimSpace(f.Kind)); kind != "" && kind != "all" {
		if strings.ToLower(v.Kind) != kind {
			return false
		}
	}
	if rt := strings.ToLower(strings.TrimSpace(f.Runtime)); rt != "" && rt != "all" {
		if strings.ToLower(v.Runtime) != rt {
			return false
		}
	}
	if cat := strings.ToLower(strings.TrimSpace(f.Category)); cat != "" && cat != "all" {
		switch cat {
		case "recommended", "official":
			if !v.Official {
				return false
			}
		case "js", "sandbox":
			if v.Runtime != RuntimeJS || v.Kind != KindPlugin {
				return false
			}
		case "native":
			if v.Runtime != RuntimeNative {
				return false
			}
		case "theme", "themes":
			if v.Kind != KindTheme {
				return false
			}
		case "security", "auth":
			c := strings.ToLower(v.Category)
			if c != "security" && c != "auth" {
				return false
			}
		case "score", "economy", "operations":
			c := strings.ToLower(v.Category)
			if c != "score" && c != "economy" && c != "operations" {
				return false
			}
		case "migration":
			if strings.ToLower(v.Category) != "migration" {
				return false
			}
		default:
			if strings.ToLower(v.Category) != cat {
				return false
			}
		}
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		matched := strings.Contains(strings.ToLower(v.ID), q) ||
			strings.Contains(strings.ToLower(v.Name), q) ||
			strings.Contains(strings.ToLower(v.Description), q) ||
			strings.Contains(strings.ToLower(v.Category), q)
		if !matched {
			for _, text := range v.RawName {
				if strings.Contains(strings.ToLower(text), q) {
					matched = true
					break
				}
			}
		}
		if !matched {
			for _, text := range v.RawDescription {
				if strings.Contains(strings.ToLower(text), q) {
					matched = true
					break
				}
			}
		}
		if !matched {
			for _, a := range v.Authors {
				if strings.Contains(strings.ToLower(a), q) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// RefreshAndEnforce forces a market index refresh across all enabled registries
// and immediately executes the blocked-plugin check.
func (c *RegistryClient) RefreshAndEnforce(ctx context.Context, locale string) (*MarketRefreshResult, error) {
	listRes, err := c.ListPackages(ctx, MarketFilter{
		Locale:       locale,
		ForceRefresh: true,
	})
	if err != nil {
		return nil, err
	}
	actions, _ := c.EnforceBlockedPlugins(ctx)
	// Refresh package list installed/status fields if any plugins were just disabled.
	if len(actions) > 0 {
		if updated, lErr := c.ListPackages(ctx, MarketFilter{Locale: locale}); lErr == nil {
			listRes = updated
		}
	}
	return &MarketRefreshResult{
		HostEnv:        listRes.HostEnv,
		Registries:     listRes.Registries,
		Packages:       listRes.Packages,
		BlockedActions: actions,
	}, nil
}

// EnforceBlockedPlugins checks all installed plugins against signature-verified registry
// blocklists. Any enabled plugin matching a block rule is immediately hot-disabled,
// recorded in AuditLog, and reported to all superadmins via in-app notification.
func (c *RegistryClient) EnforceBlockedPlugins(ctx context.Context) ([]BlockedPluginAction, error) {
	if c.mgr == nil || c.mgr.db == nil {
		return nil, nil
	}
	_, _, _, blocked, blockedSource := c.FetchAllIndexes(ctx, false)
	if len(blocked) == 0 {
		return nil, nil
	}

	rows, err := c.mgr.db.Plugin.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	var actions []BlockedPluginAction
	for _, row := range rows {
		rule, isBlocked := blocked[row.ID]
		if !isBlocked || !rule.MatchesVersion(row.Version) {
			continue
		}
		regID := blockedSource[row.ID]
		if regID == "" {
			regID = "official"
		}
		c.mgr.mu.RLock()
		_, isActive := c.mgr.active[row.ID]
		c.mgr.mu.RUnlock()

		if row.Status != entplugin.StatusEnabled && !isActive {
			continue
		}

		if err := c.disableBlockedPlugin(ctx, row, rule, regID); err != nil {
			c.mgr.log.Warn("failed to disable blocked plugin", "plugin", row.ID, "err", err)
			continue
		}
		actions = append(actions, BlockedPluginAction{
			PluginID:   row.ID,
			Version:    row.Version,
			Reason:     rule.Reason,
			RegistryID: regID,
		})
	}
	return actions, nil
}

func (c *RegistryClient) disableBlockedPlugin(ctx context.Context, row *ent.Plugin, rule RegistryBlocked, registryID string) error {
	m := c.mgr
	if m == nil {
		return nil
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	if m.bus != nil {
		m.bus.Remove("plugin:" + row.ID)
	}
	m.unregisterHostExtensions(row.ID)
	m.mu.Lock()
	rt := m.active[row.ID]
	delete(m.active, row.ID)
	m.mu.Unlock()

	reasonMsg := fmt.Sprintf("blocked by registry (%s): %s", registryID, rule.Reason)
	if len(reasonMsg) > 1000 {
		reasonMsg = reasonMsg[:1000]
	}

	if m.db != nil {
		_, err := m.db.Plugin.UpdateOneID(row.ID).
			SetStatus(entplugin.StatusDisabled).
			SetError(reasonMsg).
			SetUpdatedAt(time.Now().UTC()).
			Save(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		_ = audit.Write(ctx, m.db, &audit.Entry{
			Action:     "plugin.blocked_disabled",
			TargetType: "plugin",
			TargetID:   row.ID,
			Meta: map[string]any{
				"version":    row.Version,
				"reason":     rule.Reason,
				"registryId": registryID,
			},
		})
	}

	if rt != nil {
		_ = rt.invokeLifecycle(ctx, "onDisable", nil)
		rt.close(5 * time.Second)
	}
	if row.Kind == entplugin.KindTheme {
		if m.settings != nil {
			if cur := m.settings.Current(); cur != nil && cur.String("theme.active") == row.ID {
				defaultJSON, _ := json.Marshal(theme.DefaultID)
				_, _ = m.settings.Set(ctx, nil, map[string]json.RawMessage{"theme.active": defaultJSON}, nil)
			}
		}
		m.reloadThemes()
	}
	m.bumpConfigVersion(ctx)

	if m.db != nil && m.notify != nil {
		admins, err := m.db.User.Query().
			Where(user.RoleEQ(user.RoleSuperadmin), user.StatusEQ(user.StatusActive)).
			All(ctx)
		if err == nil {
			title := fmt.Sprintf("Security Alert: Plugin %s blocked and disabled", row.ID)
			if len([]rune(title)) > notify.MaxTitle {
				title = string([]rune(title)[:notify.MaxTitle])
			}
			body := fmt.Sprintf(
				"Plugin `%s` (`v%s`) matched a security block rule in registry `%s` and was automatically disabled.\n\nReason:\n```\n%s\n```",
				row.ID, row.Version, registryID, rule.Reason,
			)
			for _, adm := range admins {
				if _, nErr := m.notify.Send(ctx, adm.ID, title, body, nil); nErr != nil {
					m.log.Warn("failed to notify superadmin of blocked plugin", "plugin", row.ID, "userId", adm.ID, "err", nErr)
				}
			}
		}
	}
	return nil
}

// StartPatrolLoop launches the periodic (default 6h) background check for blocked plugins.
func (c *RegistryClient) StartPatrolLoop() {
	c.patrolMu.Lock()
	if c.patrolStop != nil {
		c.patrolMu.Unlock()
		return
	}
	c.mu.RLock()
	interval := c.patrolInt
	c.mu.RUnlock()
	if interval <= 0 {
		interval = DefaultRegistryPatrolInterval
	}
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	c.patrolStop = stop
	c.patrolWg = wg
	wg.Add(1)
	c.patrolMu.Unlock()

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				// Force refresh verified registry index during scheduled security patrol.
				_, _, _, _, _ = c.FetchAllIndexes(ctx, true)
				_, _ = c.EnforceBlockedPlugins(ctx)
				cancel()
			}
		}
	}()
}

// StopPatrolLoop stops the periodic blocked-plugin patrol goroutine and waits for it to exit.
func (c *RegistryClient) StopPatrolLoop() {
	c.patrolMu.Lock()
	stop := c.patrolStop
	wg := c.patrolWg
	c.patrolStop = nil
	c.patrolWg = nil
	c.patrolMu.Unlock()
	if stop != nil {
		close(stop)
		if wg != nil {
			wg.Wait()
		}
	}
}

func (c *RegistryClient) findMarketPackage(
	ctx context.Context,
	registryID string,
	pkgID string,
	version string,
) (
	entry *CachedRegistryIndex,
	pkg RegistryPackage,
	verSpec RegistryVersion,
	env HostEnv,
	catalog map[string]RegistryPackage,
	catalogEntries map[string]*CachedRegistryIndex,
	blocked map[string]RegistryBlocked,
	err error,
) {
	entries, _, catalog, blocked, _ := c.FetchAllIndexes(ctx, false)
	env, _ = c.HostEnv(ctx)

	catalogEntries = make(map[string]*CachedRegistryIndex, len(catalog))
	for _, e := range entries {
		if e.Index == nil {
			continue
		}
		for _, p := range e.Index.Packages {
			if _, exists := catalogEntries[p.ID]; !exists {
				catalogEntries[p.ID] = e
			}
		}
	}

	var foundEntry *CachedRegistryIndex
	var foundPkg *RegistryPackage
	for _, e := range entries {
		if registryID != "" && e.Source.ID != registryID && !strings.HasPrefix(registryID, e.ActiveMirror) {
			continue
		}
		if e.Index == nil {
			continue
		}
		for i := range e.Index.Packages {
			if e.Index.Packages[i].ID == pkgID {
				foundEntry = e
				foundPkg = &e.Index.Packages[i]
				break
			}
		}
		if foundPkg != nil {
			break
		}
	}
	if foundPkg == nil {
		return nil, RegistryPackage{}, RegistryVersion{}, env, catalog, catalogEntries, blocked, fmt.Errorf("%w: %q", ErrMarketPackageNotFound, pkgID)
	}

	compat := SelectBestVersion(*foundPkg, env, catalog, blocked)
	targetVer := strings.TrimSpace(version)
	if targetVer == "" {
		if compat.RecommendedVersion != "" {
			targetVer = compat.RecommendedVersion
		} else if foundPkg.Latest != "" {
			targetVer = foundPkg.Latest
		} else if len(foundPkg.Versions) > 0 {
			targetVer = foundPkg.Versions[0].Version
		}
	}

	var matchedVer *RegistryVersion
	for i := range foundPkg.Versions {
		if foundPkg.Versions[i].Version == targetVer {
			matchedVer = &foundPkg.Versions[i]
			break
		}
	}
	if matchedVer == nil {
		return nil, RegistryPackage{}, RegistryVersion{}, env, catalog, catalogEntries, blocked, fmt.Errorf("%w: %s@%s", ErrMarketPackageNotFound, pkgID, targetVer)
	}
	if rule, hasBlock := blocked[pkgID]; hasBlock && rule.MatchesVersion(matchedVer.Version) {
		return nil, RegistryPackage{}, RegistryVersion{}, env, catalog, catalogEntries, blocked, fmt.Errorf("%w: %s@%s (%s)", ErrMarketPackageBlocked, pkgID, matchedVer.Version, rule.Reason)
	}

	return foundEntry, *foundPkg, *matchedVer, env, catalog, catalogEntries, blocked, nil
}

func (c *RegistryClient) downloadAndVerifyArtifact(
	ctx context.Context,
	entry *CachedRegistryIndex,
	art RegistryArtifact,
) ([]byte, error) {
	if len(art.URLs) == 0 {
		return nil, fmt.Errorf("%w: artifact has no download URLs", ErrBadPackage)
	}
	expectedSHA := strings.ToLower(strings.TrimSpace(art.SHA256))
	if len(expectedSHA) != 64 {
		return nil, fmt.Errorf("%w: artifact is missing valid 64-hex sha256", ErrBadPackage)
	}

	var baseMirror string
	if entry != nil {
		baseMirror = strings.TrimRight(entry.ActiveMirror, "/")
	}

	var errs []string
	for _, rawU := range art.URLs {
		dlURL := resolveArtifactURL(baseMirror, rawU)
		if dlURL == "" {
			continue
		}
		data, err := c.httpGetBytes(ctx, dlURL, MaxNativeFatPackageBytes)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dlURL, err))
			continue
		}
		if art.Size > 0 && int64(len(data)) != art.Size {
			errs = append(errs, fmt.Sprintf("%s: size mismatch (got %d, want %d)", dlURL, len(data), art.Size))
			continue
		}
		sum := sha256.Sum256(data)
		actualSHA := hex.EncodeToString(sum[:])
		if subtle.ConstantTimeCompare([]byte(actualSHA), []byte(expectedSHA)) != 1 {
			errs = append(errs, fmt.Sprintf("%s: sha256 mismatch (got %s, want %s)", dlURL, actualSHA, expectedSHA))
			continue
		}
		if err := c.verifyArtifactExternalSignature(entry, art, data); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dlURL, err))
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("%w: failed to download artifact from all mirrors (%s)", ErrBadPackage, strings.Join(errs, "; "))
}

func resolveArtifactURL(baseMirror, rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" || strings.HasPrefix(u, "//") {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Host == "" {
			return ""
		}
		return u
	}
	if strings.Contains(u, "://") || baseMirror == "" {
		return ""
	}
	baseParsed, err := url.Parse(baseMirror + "/")
	if err != nil || (baseParsed.Scheme != "http" && baseParsed.Scheme != "https") || baseParsed.Host == "" {
		return ""
	}
	relParsed, err := url.Parse(strings.TrimLeft(u, "/"))
	if err != nil || relParsed.Scheme != "" || relParsed.Host != "" {
		return ""
	}
	return baseParsed.ResolveReference(relParsed).String()
}

func (c *RegistryClient) verifyArtifactExternalSignature(
	entry *CachedRegistryIndex,
	art RegistryArtifact,
	pkgBytes []byte,
) error {
	sigText := strings.TrimSpace(art.Signature)
	if sigText == "" {
		return nil
	}
	sigBytes, err := decodeEd25519Sig(sigText)
	if err != nil {
		return fmt.Errorf("%w: invalid artifact signature encoding: %v", ErrUntrustedPackage, err)
	}

	var extraKey string
	if entry != nil && !entry.Official {
		extraKey = entry.Source.PublicKey
	}
	keys := c.trustedKeysForVerification(extraKey)

	// An artifact signature in index.v1.json may sign either the raw .slp archive bytes
	// OR the internal checksums.txt payload of the .slp archive.
	for _, tk := range keys {
		if ed25519.Verify(tk.PublicKey, pkgBytes, sigBytes) {
			return nil
		}
	}
	if checksumsRaw, err := extractChecksumsFromZip(pkgBytes); err == nil && len(checksumsRaw) > 0 {
		for _, tk := range keys {
			if ed25519.Verify(tk.PublicKey, checksumsRaw, sigBytes) {
				return nil
			}
		}
	}
	if entry != nil && (entry.Official || entry.Verified) {
		return fmt.Errorf("%w: artifact signature in registry index failed verification", ErrUntrustedPackage)
	}
	return nil
}

func extractChecksumsFromZip(pkgBytes []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(pkgBytes), int64(len(pkgBytes)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name == "checksums.txt" {
			return readLimitedZipEntry(f, maxManifestEntryBytes)
		}
	}
	return nil, errors.New("checksums.txt not found in zip")
}

// InspectFromMarket downloads the host-compatible .slp artifact for a market package,
// verifies its SHA-256 and Ed25519 signature, runs InspectPackage, and resolves cascade dependencies.
func (c *RegistryClient) InspectFromMarket(ctx context.Context, req MarketInspectRequest, locale string) (*MarketInspectResult, error) {
	if c.mgr == nil {
		return nil, errors.New("plugin manager unavailable")
	}
	entry, pkg, verSpec, env, catalog, catalogEntries, blocked, err := c.findMarketPackage(ctx, req.RegistryID, req.ID, req.Version)
	if err != nil {
		return nil, err
	}

	verCompat := CheckVersionCompatWithBlocked(pkg, verSpec, env, catalog, blocked)
	if verCompat.Artifact == nil {
		msg := fmt.Sprintf("no compatible artifact for platform %s", env.Platform())
		if len(verCompat.Issues) > 0 {
			msg = verCompat.Issues[0].Message
		}
		return nil, fmt.Errorf("%w: %s", ErrMarketPackageIncompatible, msg)
	}

	pkgBytes, err := c.downloadAndVerifyArtifact(ctx, entry, *verCompat.Artifact)
	if err != nil {
		return nil, err
	}

	// Include custom registry publicKey (scoped to entry) when inspecting market package.
	inspectRes, err := c.inspectMarketArchiveBytes(ctx, entry, pkgBytes, locale)
	if err != nil {
		return nil, err
	}
	if !verCompat.Compatible {
		for _, iss := range verCompat.Issues {
			inspectRes.Warnings = append(inspectRes.Warnings, fmt.Sprintf("[%s] %s", iss.Code, iss.Message))
		}
	}

	var depsToInstall []MarketDependencyInspectItem
	if verCompat.DependencyPlan != nil {
		for _, step := range verCompat.DependencyPlan.Install {
			depPkg, ok := catalog[step.ID]
			if !ok {
				continue
			}
			depEntry := catalogEntries[step.ID]
			if depEntry == nil {
				depEntry = entry
			}
			var depPerms []string
			for _, dv := range depPkg.Versions {
				if dv.Version == step.Version {
					depPerms = append([]string(nil), dv.Permissions...)
					break
				}
			}
			if depPkg.EffectiveRuntime() == RuntimeNative && !slices.Contains(depPerms, PermNativeExec) {
				depPerms = append(depPerms, PermNativeExec)
			}
			pg, highRisk := ClassifyPermissions(depPerms, nil, nil, false)
			if depPkg.EffectiveRuntime() == RuntimeNative {
				highRisk = true
			}
			if highRisk {
				inspectRes.RequiresHighRiskConfirm = true
			}
			depsToInstall = append(depsToInstall, MarketDependencyInspectItem{
				ID:                      step.ID,
				Name:                    depPkg.Name.Resolve(locale),
				Version:                 step.Version,
				Runtime:                 depPkg.EffectiveRuntime(),
				Official:                depPkg.Official && depEntry != nil && depEntry.Official,
				Permissions:             pg,
				RequiresHighRiskConfirm: highRisk,
			})
		}
	}

	return &MarketInspectResult{
		PackageInspectResult:  inspectRes,
		RegistryID:            entry.Source.ID,
		OfficialRegistry:      entry.Official,
		Compatibility:         verCompat,
		DependencyPlan:        verCompat.DependencyPlan,
		DependenciesToInstall: depsToInstall,
	}, nil
}

func (c *RegistryClient) inspectMarketArchiveBytes(
	ctx context.Context,
	entry *CachedRegistryIndex,
	pkgBytes []byte,
	locale string,
) (*PackageInspectResult, error) {
	res, err := c.mgr.inspectPackageWithKeys(ctx, pkgBytes, locale, c.extraKeysForEntry(entry))
	if err != nil {
		return nil, err
	}
	c.promoteOfficialVerificationIfTestKey(entry, &res.Verification)
	if res.Runtime == RuntimeNative && res.Verification.Trusted && res.NativeInstallBlocked {
		res.NativeInstallBlocked = false
		res.NativeBlockReason = ""
	}
	return res, nil
}

func (c *RegistryClient) testOfficialPublicKeyOverride() string {
	c.mu.RLock()
	offSpec := c.officialPubKeySpec
	c.mu.RUnlock()
	if offSpec != "" && offSpec != OfficialPublicKeyPEM {
		return offSpec
	}
	return ""
}

func (c *RegistryClient) extraKeysForEntry(entry *CachedRegistryIndex) []string {
	if entry != nil && !entry.Official && strings.TrimSpace(entry.Source.PublicKey) != "" {
		return []string{strings.TrimSpace(entry.Source.PublicKey)}
	}
	return nil
}

func (c *RegistryClient) promoteOfficialVerificationIfTestKey(entry *CachedRegistryIndex, v *PackageVerification) {
	if v == nil || !v.Verified {
		return
	}
	if pub, err := c.officialPublicKey(); err == nil {
		if v.KeyID == PublicKeyFingerprint(pub) && entry != nil && entry.Official {
			v.Official = true
			v.Trusted = true
			v.Signer = "SkinLoom Official"
		}
	}
}

// InstallFromMarket resolves any missing cascade dependencies, downloads and verifies each
// required .slp package from the market, and installs/upgrades them in topological order.
func (c *RegistryClient) InstallFromMarket(ctx context.Context, req MarketInstallRequest) (*MarketInstallResult, error) {
	if c.mgr == nil {
		return nil, errors.New("plugin manager unavailable")
	}
	entry, pkg, verSpec, env, catalog, catalogEntries, blocked, err := c.findMarketPackage(ctx, req.RegistryID, req.ID, req.Version)
	if err != nil {
		return nil, err
	}

	verCompat := CheckVersionCompatWithBlocked(pkg, verSpec, env, catalog, blocked)
	if !verCompat.Compatible || verCompat.Artifact == nil {
		msg := fmt.Sprintf("package %s@%s is not compatible with host %s", pkg.ID, verSpec.Version, env.Platform())
		if len(verCompat.Issues) > 0 {
			msg = verCompat.Issues[0].Message
		}
		if verCompat.Status == CompatBlocked {
			return nil, fmt.Errorf("%w: %s", ErrMarketPackageBlocked, msg)
		}
		return nil, fmt.Errorf("%w: %s", ErrMarketPackageIncompatible, msg)
	}

	installDeps := true
	if req.InstallDependencies != nil {
		installDeps = *req.InstallDependencies
	}

	var installedDeps []*PluginInfo
	if installDeps && verCompat.DependencyPlan != nil && len(verCompat.DependencyPlan.Install) > 0 {
		for _, step := range verCompat.DependencyPlan.Install {
			if step.Artifact == nil {
				return nil, fmt.Errorf("%w: missing artifact for dependency %s@%s", ErrMarketPackageIncompatible, step.ID, step.Version)
			}
			depPkg, ok := catalog[step.ID]
			if !ok {
				return nil, fmt.Errorf("%w: dependency %q not found in catalog", ErrMarketPackageNotFound, step.ID)
			}
			depEntry := catalogEntries[step.ID]
			if depEntry == nil {
				depEntry = entry
			}
			var depVer RegistryVersion
			for _, dv := range depPkg.Versions {
				if dv.Version == step.Version {
					depVer = dv
					break
				}
			}
			depBytes, dlErr := c.downloadAndVerifyArtifact(ctx, depEntry, *step.Artifact)
			if dlErr != nil {
				return nil, fmt.Errorf("download dependency %s@%s: %w", step.ID, step.Version, dlErr)
			}
			depInfo, instErr := c.installMarketArchiveBytes(ctx, depEntry, depVer, depBytes, InstallOptions{
				GrantedPermissions: req.GrantedPermissions,
				GrantAllDeclared:   true,
				AllowUnsigned:      req.AllowUnsigned,
				Enable:             !req.Disabled,
				Source:             "market",
				RegistrySource:     depEntry.Source.ID,
				PlatformPackages:   platformSHAsFromVersion(depVer),
			})
			if instErr != nil {
				return nil, fmt.Errorf("install dependency %s@%s: %w", step.ID, step.Version, instErr)
			}
			installedDeps = append(installedDeps, depInfo)
		}
	}

	pkgBytes, err := c.downloadAndVerifyArtifact(ctx, entry, *verCompat.Artifact)
	if err != nil {
		return nil, err
	}

	targetInfo, err := c.installMarketArchiveBytes(ctx, entry, verSpec, pkgBytes, InstallOptions{
		GrantedPermissions: req.GrantedPermissions,
		GrantAllDeclared:   req.GrantAll,
		AllowUnsigned:      req.AllowUnsigned,
		Enable:             !req.Disabled,
		Source:             "market",
		RegistrySource:     entry.Source.ID,
		PlatformPackages:   platformSHAsFromVersion(verSpec),
	})
	if err != nil {
		return nil, err
	}

	return &MarketInstallResult{
		Plugin:                targetInfo,
		InstalledDependencies: installedDeps,
	}, nil
}

func (c *RegistryClient) installMarketArchiveBytes(
	ctx context.Context,
	entry *CachedRegistryIndex,
	_ RegistryVersion,
	pkgBytes []byte,
	opts InstallOptions,
) (*PluginInfo, error) {
	opts.ExtraTrustedKeys = append(opts.ExtraTrustedKeys, c.extraKeysForEntry(entry)...)
	return c.mgr.InstallPackage(ctx, pkgBytes, opts)
}

func platformSHAsFromVersion(ver RegistryVersion) map[string]string {
	if len(ver.Artifacts) == 0 {
		return nil
	}
	out := make(map[string]string, len(ver.Artifacts))
	for plat, art := range ver.Artifacts {
		if plat != "" && len(strings.TrimSpace(art.SHA256)) == 64 {
			out[plat] = strings.ToLower(strings.TrimSpace(art.SHA256))
		}
	}
	return out
}

// FetchPlatformPackage implements PlatformPackageFetcher so multi-architecture
// cluster nodes can automatically download and verify their own GOOS/GOARCH .slp
// package from the market registry during SyncCluster.
func (c *RegistryClient) FetchPlatformPackage(
	ctx context.Context,
	registrySource, pluginID, version, platform string,
) ([]byte, error) {
	entry, pkg, verSpec, _, _, _, _, err := c.findMarketPackage(ctx, registrySource, pluginID, version)
	if err != nil && registrySource != "" {
		// Retry across all enabled registries if registrySource was a mirror URL or renamed ID.
		entry, pkg, verSpec, _, _, _, _, err = c.findMarketPackage(ctx, "", pluginID, version)
	}
	if err != nil {
		return nil, err
	}
	_, art, ok := selectArtifact(pkg.EffectiveRuntime(), verSpec.Artifacts, platform)
	if !ok {
		return nil, fmt.Errorf("%w: no artifact for %s@%s on platform %s", ErrMarketPackageIncompatible, pluginID, version, platform)
	}
	return c.downloadAndVerifyArtifact(ctx, entry, art)
}
