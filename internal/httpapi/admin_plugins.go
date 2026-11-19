package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

// AdminPluginRoute describes one registered HTTP route belonging to a plugin.
type AdminPluginRoute struct {
	Method      string `json:"method"`
	Pattern     string `json:"pattern"`
	Auth        string `json:"auth"`
	StreamProxy bool   `json:"streamProxy,omitempty"`
}

// AdminPluginView summarizes an installed plugin or theme for the admin panel.
type AdminPluginView struct {
	ID                 string                  `json:"id"`
	Kind               string                  `json:"kind"`
	Runtime            string                  `json:"runtime"`
	Version            string                  `json:"version"`
	Name               string                  `json:"name"`
	Description        string                  `json:"description,omitempty"`
	Authors            []string                `json:"authors,omitempty"`
	Homepage           string                  `json:"homepage,omitempty"`
	Status             string                  `json:"status"`
	Error              string                  `json:"error,omitempty"`
	Source             string                  `json:"source"`
	RegistrySource     string                  `json:"registrySource,omitempty"`
	PackageSHA256      string                  `json:"packageSha256,omitempty"`
	Platforms          []string                `json:"platforms,omitempty"`
	HasServer          bool                    `json:"hasServer"`
	HasWeb             bool                    `json:"hasWeb"`
	HasSettings        bool                    `json:"hasSettings"`
	Permissions        plugin.PermissionGroups `json:"permissions"`
	GrantedPermissions []string                `json:"grantedPermissions"`
	Settings           map[string]any          `json:"settings,omitempty"`
	Routes             []AdminPluginRoute      `json:"routes,omitempty"`
	Stats              *jsrt.PoolStats         `json:"stats,omitempty"`
	NativeStats        *plugin.NativeStats     `json:"nativeStats,omitempty"`
	Usage              *plugin.PluginUsage     `json:"usage,omitempty"`
	RecentLogs         []plugin.LogEntry       `json:"recentLogs,omitempty"`
	CircuitTripped     bool                    `json:"circuitTripped,omitempty"`
	ConsecutiveErrors  int                     `json:"consecutiveErrors,omitempty"`
}

type pluginDevReloadInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with a superadmin session"`
	Body          struct {
		Manifest map[string]any    `json:"manifest" doc:"Parsed plugin.json object"`
		ServerJS string            `json:"serverJs,omitempty" doc:"Bundled ES2023 server.js source (Tier 1 JS plugins)"`
		Files    map[string]string `json:"files,omitempty" doc:"Base64-encoded static files (web/*, assets/*, locales/*) and native executable binaries (bin/*)"`
	}
}

type pluginDevReloadOutput struct {
	Body *plugin.PluginInfo
}

func isDevReloadAllowed(d Deps) bool {
	if d.Plugins != nil && d.Plugins.AllowUnsignedNative() {
		return true
	}
	// Allow when Runtime.Web is "dev" or unset (unit test harnesses); block when
	// explicitly running in production web modes ("managed", "external", "off")
	// without plugins.allow_unsigned_native.
	switch strings.ToLower(strings.TrimSpace(d.Runtime.Web)) {
	case "", "dev":
		return true
	default:
		return false
	}
}

// pluginUploadGuard enforces authentication and http.MaxBytesReader BEFORE Huma reads
// or parses large JSON/multipart payloads on /api/v1/admin/plugins/{dev-reload,inspect,install}.
func pluginUploadGuard(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			var maxBytes int64
			needSuperadmin := false
			needDevMode := false
			switch r.URL.Path {
			case "/api/v1/admin/plugins/inspect":
				maxBytes = plugin.MaxNativeFatPackageBytes + 64*1024
			case "/api/v1/admin/plugins/install":
				maxBytes = plugin.MaxNativeFatPackageBytes + 64*1024
				needSuperadmin = true
			case "/api/v1/admin/plugins/dev-reload":
				maxBytes = 96 * 1024 * 1024
				needSuperadmin = true
				needDevMode = true
			default:
				next.ServeHTTP(w, r)
				return
			}

			var err error
			if needSuperadmin {
				_, err = requirePluginSuperadmin(r.Context(), d, r.Header.Get("Authorization"))
			} else {
				_, err = requirePluginAdmin(r.Context(), d, r.Header.Get("Authorization"))
			}
			if err != nil {
				var se huma.StatusError
				if errors.As(err, &se) {
					writeProblem(w, se.GetStatus(), se.Error())
				} else {
					writeProblem(w, http.StatusUnauthorized, err.Error())
				}
				return
			}
			if needDevMode && !isDevReloadAllowed(d) {
				writeProblem(w, http.StatusForbidden, "plugin dev-reload is disabled in production mode")
				return
			}
			if r.Body != nil && maxBytes > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func registerAdminPlugins(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID:     "devReloadPlugin",
		Method:          http.MethodPost,
		Path:            "/api/v1/admin/plugins/dev-reload",
		Summary:         "Hot-reload an unpacked development plugin bundle",
		Description:     "Used by skinloom-plugin dev during local development. Replaces the active JS or native Go plugin runtime atomically without restarting the server.",
		Tags:            []string{"admin"},
		Security:        adminSecurity,
		MaxBodyBytes:    96 * 1024 * 1024,
		BodyReadTimeout: 2 * time.Minute,
	}, func(ctx context.Context, in *pluginDevReloadInput) (*pluginDevReloadOutput, error) {
		actorID, err := requirePluginSuperadmin(ctx, d, in.Authorization)
		if err != nil {
			return nil, err
		}
		if !isDevReloadAllowed(d) {
			return nil, huma.Error403Forbidden("plugin dev-reload is disabled in production mode")
		}
		manifestBytes, err := json.Marshal(in.Body.Manifest)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid manifest payload", err)
		}
		staticFiles := make(map[string][]byte, len(in.Body.Files))
		for rel, b64 := range in.Body.Files {
			clean, keyErr := plugin.ValidateFileKey(strings.TrimPrefix(rel, "/"))
			if keyErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid file path "+rel, keyErr)
			}
			data, decErr := base64.StdEncoding.DecodeString(b64)
			if decErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid base64 file content for "+clean, decErr)
			}
			staticFiles[clean] = data
		}
		runtimeKind, _ := in.Body.Manifest["runtime"].(string)
		var info *plugin.PluginInfo
		if strings.EqualFold(strings.TrimSpace(runtimeKind), plugin.RuntimeNative) {
			info, err = d.Plugins.LoadDevNative(ctx, manifestBytes, staticFiles, nil, true)
		} else {
			info, err = d.Plugins.LoadDevJS(ctx, manifestBytes, []byte(in.Body.ServerJS), staticFiles, nil, true)
		}
		if err != nil {
			return nil, pluginAPIError(err)
		}
		writePluginAudit(ctx, d, actorID, "plugin.dev_reload", info.ID, map[string]any{
			"version": info.Version,
			"kind":    info.Kind,
			"runtime": info.Runtime,
		})
		notifyPluginConfigFallback(d)
		return &pluginDevReloadOutput{Body: info}, nil
	})

	registerAdminPluginPackageRoutes(api, d)
	registerAdminMarketRoutes(api, d)
}

type pluginInspectForm struct {
	File huma.FormFile `form:"file" required:"true" doc:".slp plugin or theme archive"`
}

type pluginInspectInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>"`
	RawBody       huma.MultipartFormFiles[pluginInspectForm]
}

type pluginInstallForm struct {
	File               huma.FormFile `form:"file" required:"true" doc:".slp plugin or theme archive"`
	GrantedPermissions string        `form:"grantedPermissions" required:"false" doc:"Optional JSON string array of granted permission names"`
	GrantAll           bool          `form:"grantAll" required:"false" doc:"Grant all declared permissions in the manifest"`
	AllowUnsigned      bool          `form:"allowUnsigned" required:"false" doc:"Allow installing an unsigned or untrusted .slp archive after risk confirmation"`
	Disabled           bool          `form:"disabled" required:"false" doc:"Install without immediately enabling the plugin"`
}

type pluginInstallInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>"`
	RawBody       huma.MultipartFormFiles[pluginInstallForm]
}

type pluginIDInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>"`
	ID            string `path:"id" maxLength:"40"`
}

type uninstallPluginInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>"`
	ID            string `path:"id" maxLength:"40"`
	Purge         bool   `query:"purge" doc:"When true, also remove plugin KV, documents, and private files"`
}

type updatePluginSettingsInput struct {
	Authorization string         `header:"Authorization" doc:"Bearer <admin token>"`
	ID            string         `path:"id" maxLength:"40"`
	Body          map[string]any `doc:"Setting keys to update. Send null to reset or omit/send ******** to keep existing secrets."`
}

type uninstallPluginResult struct {
	ID     string `json:"id"`
	Purged bool   `json:"purged"`
}

func registerAdminPluginPackageRoutes(api huma.API, d Deps) {
	op := func(id, method, p, summary string, status int) huma.Operation {
		return huma.Operation{
			OperationID:   id,
			Method:        method,
			Path:          p,
			Summary:       summary,
			Tags:          []string{"admin"},
			Security:      adminSecurity,
			DefaultStatus: status,
		}
	}

	huma.Register(api, op("listPlugins", http.MethodGet, "/api/v1/admin/plugins", "List installed plugins and themes", 0),
		func(ctx context.Context, in *adminAuth) (*struct{ Body []AdminPluginView }, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			list, err := d.Plugins.List(ctx)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			locale := requestInfo(ctx).locale
			out := make([]AdminPluginView, 0, len(list))
			for i := range list {
				out = append(out, toAdminPluginView(&list[i], locale))
			}
			return &struct{ Body []AdminPluginView }{out}, nil
		})

	huma.Register(api, op("getPlugin", http.MethodGet, "/api/v1/admin/plugins/{id}", "Inspect one installed plugin", 0),
		func(ctx context.Context, in *pluginIDInput) (*struct{ Body AdminPluginView }, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			info, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			return &struct{ Body AdminPluginView }{toAdminPluginView(info, requestInfo(ctx).locale)}, nil
		})

	huma.Register(api, huma.Operation{
		OperationID:     "inspectPluginPackage",
		Method:          http.MethodPost,
		Path:            "/api/v1/admin/plugins/inspect",
		Summary:         "Validate and inspect an uploaded .slp package",
		Description:     "Verifies integrity checksums, Ed25519 signature status, upgrade delta, dual-tier platform compatibility, and permission risk tiers without installing.",
		Tags:            []string{"admin"},
		Security:        adminSecurity,
		MaxBodyBytes:    plugin.MaxNativeFatPackageBytes + 64*1024,
		BodyReadTimeout: 3 * time.Minute,
	}, func(ctx context.Context, in *pluginInspectInput) (*struct {
		Body *plugin.PackageInspectResult
	}, error) {
		if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		form := in.RawBody.Data()
		if form.File.File == nil {
			return nil, huma.Error422UnprocessableEntity("missing .slp package file")
		}
		defer form.File.File.Close()
		pkgBytes, err := readPackageUpload(form.File)
		if err != nil {
			return nil, err
		}
		res, err := d.Plugins.InspectPackage(ctx, pkgBytes, requestInfo(ctx).locale)
		if err != nil {
			return nil, pluginAPIError(err)
		}
		return &struct{ Body *plugin.PackageInspectResult }{res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:     "installPluginPackage",
		Method:          http.MethodPost,
		Path:            "/api/v1/admin/plugins/install",
		Summary:         "Install or upgrade a .slp plugin or theme package",
		Description:     "Super administrators only. Verifies archive integrity, Ed25519 trust, and explicit permission confirmation before installing or upgrading.",
		Tags:            []string{"admin"},
		Security:        adminSecurity,
		DefaultStatus:   http.StatusCreated,
		MaxBodyBytes:    plugin.MaxNativeFatPackageBytes + 64*1024,
		BodyReadTimeout: 3 * time.Minute,
	}, func(ctx context.Context, in *pluginInstallInput) (*struct{ Body AdminPluginView }, error) {
		actorID, err := requirePluginSuperadmin(ctx, d, in.Authorization)
		if err != nil {
			return nil, err
		}
		form := in.RawBody.Data()
		if form.File.File == nil {
			return nil, huma.Error422UnprocessableEntity("missing .slp package file")
		}
		defer form.File.File.Close()
		pkgBytes, err := readPackageUpload(form.File)
		if err != nil {
			return nil, err
		}
		var granted []string
		if rawPerms := strings.TrimSpace(form.GrantedPermissions); rawPerms != "" {
			var parsed []string
			if err := json.Unmarshal([]byte(rawPerms), &parsed); err != nil {
				return nil, huma.Error422UnprocessableEntity("invalid grantedPermissions JSON array", err)
			}
			granted = parsed
			if granted == nil {
				granted = []string{}
			}
		}

		info, err := d.Plugins.InstallPackage(ctx, pkgBytes, plugin.InstallOptions{
			GrantedPermissions: granted,
			GrantAllDeclared:   form.GrantAll,
			AllowUnsigned:      form.AllowUnsigned,
			Enable:             !form.Disabled,
			Source:             "package",
		})
		if err != nil {
			return nil, pluginAPIError(err)
		}

		writePluginAudit(ctx, d, actorID, "plugin.installed", info.ID, map[string]any{
			"version":       info.Version,
			"kind":          info.Kind,
			"allowUnsigned": form.AllowUnsigned,
			"enabled":       !form.Disabled,
		})
		notifyPluginConfigFallback(d)
		return &struct{ Body AdminPluginView }{toAdminPluginView(info, requestInfo(ctx).locale)}, nil
	})

	huma.Register(api, op("enablePlugin", http.MethodPost, "/api/v1/admin/plugins/{id}/enable", "Enable an installed plugin", 0),
		func(ctx context.Context, in *pluginIDInput) (*struct{ Body AdminPluginView }, error) {
			actorID, err := requirePluginAdmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			existing, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			if pluginRequiresSuperadmin(existing, false) {
				if _, sErr := requirePluginSuperadmin(ctx, d, in.Authorization); sErr != nil {
					return nil, sErr
				}
			}
			if err := d.Plugins.Enable(ctx, in.ID); err != nil {
				return nil, pluginAPIError(err)
			}
			info, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			writePluginAudit(ctx, d, actorID, "plugin.enabled", in.ID, nil)
			notifyPluginConfigFallback(d)
			return &struct{ Body AdminPluginView }{toAdminPluginView(info, requestInfo(ctx).locale)}, nil
		})

	huma.Register(api, op("disablePlugin", http.MethodPost, "/api/v1/admin/plugins/{id}/disable", "Disable an active plugin", 0),
		func(ctx context.Context, in *pluginIDInput) (*struct{ Body AdminPluginView }, error) {
			actorID, err := requirePluginAdmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			existing, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			if pluginRequiresSuperadmin(existing, false) {
				if _, sErr := requirePluginSuperadmin(ctx, d, in.Authorization); sErr != nil {
					return nil, sErr
				}
			}
			if err := d.Plugins.Disable(ctx, in.ID); err != nil {
				return nil, pluginAPIError(err)
			}
			info, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			writePluginAudit(ctx, d, actorID, "plugin.disabled", in.ID, nil)
			notifyPluginConfigFallback(d)
			return &struct{ Body AdminPluginView }{toAdminPluginView(info, requestInfo(ctx).locale)}, nil
		})

	huma.Register(api, op("reloadPlugin", http.MethodPost, "/api/v1/admin/plugins/{id}/reload", "Reload a plugin bundle cleanly", 0),
		func(ctx context.Context, in *pluginIDInput) (*struct{ Body AdminPluginView }, error) {
			actorID, err := requirePluginAdmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			if existing, ok := d.Plugins.Get(ctx, in.ID); ok && pluginRequiresSuperadmin(existing, false) {
				if _, sErr := requirePluginSuperadmin(ctx, d, in.Authorization); sErr != nil {
					return nil, sErr
				}
			}
			info, err := d.Plugins.ReloadPlugin(ctx, in.ID)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			writePluginAudit(ctx, d, actorID, "plugin.reloaded", in.ID, nil)
			notifyPluginConfigFallback(d)
			return &struct{ Body AdminPluginView }{toAdminPluginView(info, requestInfo(ctx).locale)}, nil
		})

	huma.Register(api, op("uninstallPlugin", http.MethodDelete, "/api/v1/admin/plugins/{id}", "Uninstall a plugin", 0),
		func(ctx context.Context, in *uninstallPluginInput) (*struct{ Body uninstallPluginResult }, error) {
			actorID, err := requirePluginSuperadmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			if _, ok := d.Plugins.Get(ctx, in.ID); !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			if err := d.Plugins.Uninstall(ctx, in.ID, in.Purge); err != nil {
				return nil, pluginAPIError(err)
			}
			writePluginAudit(ctx, d, actorID, "plugin.uninstalled", in.ID, map[string]any{"purge": in.Purge})
			notifyPluginConfigFallback(d)
			return &struct{ Body uninstallPluginResult }{uninstallPluginResult{ID: in.ID, Purged: in.Purge}}, nil
		})

	huma.Register(api, op("getPluginSettings", http.MethodGet, "/api/v1/admin/plugins/{id}/settings", "Get a plugin's settings schema and redacted values", 0),
		func(ctx context.Context, in *pluginIDInput) (*struct {
			Body *plugin.PluginSettingsResponse
		}, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			res, err := d.Plugins.PluginSettingsView(ctx, in.ID, requestInfo(ctx).locale)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			return &struct {
				Body *plugin.PluginSettingsResponse
			}{res}, nil
		})

	huma.Register(api, op("updatePluginSettings", http.MethodPatch, "/api/v1/admin/plugins/{id}/settings", "Update a plugin's settings", 0),
		func(ctx context.Context, in *updatePluginSettingsInput) (*struct {
			Body *plugin.PluginSettingsResponse
		}, error) {
			actorID, err := requirePluginAdmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			existing, ok := d.Plugins.Get(ctx, in.ID)
			if !ok {
				return nil, huma.Error404NotFound("no such plugin")
			}
			if pluginRequiresSuperadmin(existing, true) {
				if _, sErr := requirePluginSuperadmin(ctx, d, in.Authorization); sErr != nil {
					return nil, sErr
				}
			}
			if err := d.Plugins.UpdateSettings(ctx, in.ID, in.Body); err != nil {
				return nil, pluginAPIError(err)
			}
			changedKeys := make([]string, 0, len(in.Body))
			for k := range in.Body {
				changedKeys = append(changedKeys, k)
			}
			slices.Sort(changedKeys)
			writePluginAudit(ctx, d, actorID, "plugin.settings_updated", in.ID, map[string]any{"keys": changedKeys})
			notifyPluginConfigFallback(d)
			res, err := d.Plugins.PluginSettingsView(ctx, in.ID, requestInfo(ctx).locale)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			return &struct {
				Body *plugin.PluginSettingsResponse
			}{res}, nil
		})
}

func pluginRequiresSuperadmin(info *plugin.PluginInfo, _ bool) bool {
	if info == nil {
		return false
	}
	if strings.EqualFold(info.Runtime, plugin.RuntimeNative) {
		return true
	}
	for _, p := range info.GrantedPermissions {
		if p == plugin.PermNativeExec {
			return true
		}
	}
	if mf := info.Manifest; mf != nil {
		if mf.EffectiveRuntime() == plugin.RuntimeNative {
			return true
		}
	}
	return false
}

func requirePluginAdmin(ctx context.Context, d Deps, authorization string) (*int64, error) {
	actorID, err := checkAdmin(ctx, d, authorization)
	if err != nil {
		return nil, err
	}
	if d.Plugins == nil {
		return nil, huma.Error503ServiceUnavailable("plugin manager is unavailable")
	}
	return actorID, nil
}

func requirePluginSuperadmin(ctx context.Context, d Deps, authorization string) (*int64, error) {
	actorID, err := requirePluginAdmin(ctx, d, authorization)
	if err != nil {
		return nil, err
	}
	if actorID != nil {
		if v := requestInfo(ctx).viewer(); v == nil || v.User.Role != user.RoleSuperadmin {
			return nil, huma.Error403Forbidden("super administrators only")
		}
	}
	return actorID, nil
}

func readPackageUpload(f huma.FormFile) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(f.File, plugin.MaxNativeFatPackageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > plugin.MaxNativeFatPackageBytes {
		return nil, huma.NewError(http.StatusRequestEntityTooLarge, "package archive exceeds 150 MiB hard limit")
	}
	return data, nil
}

func pluginAPIError(err error) error {
	switch {
	case ent.IsNotFound(err):
		return huma.Error404NotFound("no such plugin")
	case errors.Is(err, plugin.ErrMarketPackageNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, plugin.ErrUnsignedNativeForbidden),
		errors.Is(err, plugin.ErrMarketPackageBlocked):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, plugin.ErrBadPackage),
		errors.Is(err, plugin.ErrBadManifest),
		errors.Is(err, plugin.ErrUntrustedPackage),
		errors.Is(err, plugin.ErrInvalidSettings),
		errors.Is(err, plugin.ErrMissingDependency),
		errors.Is(err, plugin.ErrMarketPackageIncompatible),
		errors.Is(err, plugin.ErrRegistrySignatureInvalid),
		errors.Is(err, jsrt.ErrBundleTooLarge),
		errors.Is(err, jsrt.ErrInvalidModule),
		errors.Is(err, jsrt.ErrTimeout),
		errors.Is(err, jsrt.ErrOutOfMemory):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		var jsErr *jsrt.JSError
		if errors.As(err, &jsErr) {
			return huma.Error422UnprocessableEntity(err.Error())
		}
	}
	return apiError(err)
}

func writePluginAudit(ctx context.Context, d Deps, actorID *int64, action, pluginID string, meta map[string]any) {
	if d.DB == nil {
		return
	}
	rec := &audit.Entry{
		Actor:      actorID,
		Action:     action,
		TargetType: "plugin",
		TargetID:   pluginID,
		IP:         requestInfo(ctx).client.ClientIP,
		Meta:       meta,
	}
	if err := audit.Write(ctx, d.DB.Ent, rec); err != nil && d.Log != nil {
		d.Log.Warn("failed to record plugin audit log", "action", action, "plugin", pluginID, "err", err)
	}
}

// notifyPluginConfigFallback publishes a config-changed event only when Settings is nil
// (standalone HTTP unit tests without a Settings service); in normal operation Manager
// already calls m.bumpConfigVersion(ctx) -> Settings.Touch(ctx).
func notifyPluginConfigFallback(d Deps) {
	if d.Settings == nil && d.Broker != nil {
		d.Broker.Publish(pubsub.TopicConfigChanged, time.Now().UnixMilli())
	}
}

func toAdminPluginView(info *plugin.PluginInfo, locale string) AdminPluginView {
	granted := append([]string(nil), info.GrantedPermissions...)
	if granted == nil {
		granted = []string{}
	}
	runtimeKind := info.Runtime
	if runtimeKind == "" {
		runtimeKind = plugin.RuntimeJS
	}
	view := AdminPluginView{
		ID:                 info.ID,
		Kind:               info.Kind,
		Runtime:            runtimeKind,
		Version:            info.Version,
		Name:               info.ID,
		Status:             info.Status,
		Error:              info.Error,
		Source:             info.Source,
		RegistrySource:     info.RegistrySource,
		PackageSHA256:      info.PackageSHA256,
		GrantedPermissions: granted,
		Settings:           info.Settings,
		Stats:              info.Stats,
		NativeStats:        info.NativeStats,
		Usage:              info.Usage,
		RecentLogs:         info.RecentLogs,
		CircuitTripped:     info.CircuitTripped,
		ConsecutiveErrors:  info.ConsecutiveErrors,
	}
	var declared []string
	if info.Manifest != nil {
		view.Runtime = info.Manifest.EffectiveRuntime()
		if n := info.Manifest.Name.Resolve(locale); n != "" {
			view.Name = n
		}
		view.Description = info.Manifest.Description.Resolve(locale)
		view.Authors = append([]string(nil), info.Manifest.Authors...)
		view.Homepage = info.Manifest.Homepage
		view.HasServer = info.Manifest.Server != "" || view.Runtime == plugin.RuntimeNative
		view.HasWeb = info.Manifest.Web != ""
		view.HasSettings = len(info.Manifest.Settings) > 0
		declared = info.Manifest.EffectivePermissions()
		if info.Manifest.Native != nil && len(info.Manifest.Native.Platforms) > 0 {
			view.Platforms = append([]string(nil), info.Manifest.Native.Platforms...)
		}
	}
	if len(view.Platforms) == 0 && len(info.PlatformPackages) > 0 {
		plats := make([]string, 0, len(info.PlatformPackages))
		for p := range info.PlatformPackages {
			plats = append(plats, p)
		}
		slices.Sort(plats)
		view.Platforms = plats
	}
	groups, _ := plugin.ClassifyPermissions(declared, nil, granted, false)
	view.Permissions = groups
	if len(info.Routes) > 0 {
		view.Routes = make([]AdminPluginRoute, 0, len(info.Routes))
		for _, r := range info.Routes {
			view.Routes = append(view.Routes, AdminPluginRoute{
				Method:      r.Method,
				Pattern:     r.Pattern,
				Auth:        r.Auth,
				StreamProxy: r.StreamProxy,
			})
		}
	}
	return view
}
