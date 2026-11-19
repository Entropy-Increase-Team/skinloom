package plugin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// PluginDriver is an alias for Driver matching the dual-tier architecture design doc.
type PluginDriver = Driver

// DriverStats reports runtime-specific health and execution telemetry.
type DriverStats struct {
	Runtime string          `json:"runtime"` // "js" | "native"
	Pool    *jsrt.PoolStats `json:"pool,omitempty"`
	Native  *NativeStats    `json:"native,omitempty"`
}

// NativeStats reports OS subprocess telemetry for Tier 2 native Go plugins.
type NativeStats struct {
	PID            int       `json:"pid"`
	Running        bool      `json:"running"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	UptimeSeconds  int64     `json:"uptimeSeconds"`
	Restarts       int       `json:"restarts"`
	MemoryRSSBytes uint64    `json:"memoryRssBytes,omitempty"`
	HTTPProxy      bool      `json:"httpProxy,omitempty"`
	DBDriver       string    `json:"dbDriver,omitempty"`
	DBPrefix       string    `json:"dbPrefix,omitempty"`
}

// DriverInitConfig passes host-resolved runtime context into Driver.Init.
type DriverInitConfig struct {
	Runtime    *pluginRuntime
	Settings   map[string]any
	DBConfig   *PluginDBConfig
	DataDir    string
	RuntimeDir string
}

// DriverRegistrations captures every hook, route, cron job, service, extension,
// and native infrastructure provider declared by a plugin during Init.
type DriverRegistrations struct {
	Hooks               []hookBinding
	Routes              []routeBinding
	AuthProviders       []authProviderBinding
	PasswordVerifiers   []passwordVerifierBinding
	CaptchaProviders    []captchaBinding
	CDNProviders        []cdnBinding
	YggdrasilMetas      []yggMetaBinding
	Crons               []cronBinding
	Services            []serviceBinding
	HasSettingsOnChange bool
	StorageProvider     storage.Store
	SearchProvider      texture.SearchProvider
	RenderProvider      render.Provider
}

// Driver abstracts backend plugin execution across Tier 1 (QuickJS-NG/WASM) and
// Tier 2 (hashicorp/go-plugin native OS subprocess with bidirectional gRPC).
type Driver interface {
	Init(ctx context.Context, cfg DriverInitConfig) (*DriverRegistrations, error)
	Lifecycle(ctx context.Context, method string, payload any) error
	InvokeGuard(ctx context.Context, handlerID, hookName string, payload any) error
	InvokeFilter(ctx context.Context, handlerID, hookName string, value, payload any) (any, error)
	InvokeEvent(ctx context.Context, handlerID, hookName string, payload any) error
	InvokeHTTP(ctx context.Context, handlerID string, req map[string]any) (any, error)
	InvokeCron(ctx context.Context, handlerID, jobName string) error
	InvokeService(ctx context.Context, handlerID, serviceName, callerID string, args ...any) (any, error)
	InvokeExtension(ctx context.Context, kind, id, handlerID string, args ...any) (any, error)
	NotifySettings(ctx context.Context, newSettings map[string]any) error
	ProxyHandler() http.Handler
	Stats() DriverStats
	DrainAndClose(timeout time.Duration) error
}

func resolveHandlerID(handlerID string, fnID int) string {
	if handlerID != "" {
		return handlerID
	}
	return strconv.Itoa(fnID)
}
