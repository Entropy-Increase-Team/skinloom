package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/httpx"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

// Deps are the services the API needs.
type Deps struct {
	Log         *slog.Logger
	DB          *db.DB // nil during OpenAPI export or Pre-Core Setup Mode
	Setup       SetupController
	Settings    *settings.Service
	SettingsReg *settings.Registry
	Themes      *theme.Registry
	Broker      *pubsub.Broker
	Bus         *hook.Bus
	Accounts    *account.Service
	Captcha     *captcha.Service
	Mailer      *mail.Mailer
	Textures    *texture.Service
	Players     *player.Service
	Reports     *report.Service
	Renders     *render.Service
	Notify      *notify.Service
	Yggdrasil   *yggdrasil.Service
	OAuth       *oauth.Service
	Social      *social.Service
	CDN         *cdn.Service
	Plugins     *plugin.Manager
	// ClientInfo resolves the client address and public host behind trusted proxies
	// (the gateway's live resolver). Nil = trust no proxy.
	ClientInfo func(*http.Request) realip.Info
	// AdminToken also authorizes /api/v1/admin, for scripts, until personal access
	// tokens land (M3). Browsers use an admin session.
	AdminToken string
	// MaxEventStreams caps concurrent /events connections (0 = default 5000), so a
	// flood of idle SSE clients cannot exhaust memory and file descriptors.
	MaxEventStreams int
	// Runtime describes this process for the system status page.
	Runtime RuntimeInfo
	// WebReady reports whether the frontend this process runs answers; /readyz waits for
	// it, so a probe does not send visitors to a site whose pages still fail. Nil = ready.
	WebReady func() bool
	// WebStatus optionally reports the supervised frontend's lifecycle and failure state
	// for /readyz responses (plain text and Accept: application/json).
	WebStatus func() WebStatusInfo
}

// WebStatusInfo describes the supervised frontend's state for /readyz diagnostics.
type WebStatusInfo struct {
	Mode        string   `json:"mode,omitempty"`
	State       string   `json:"state,omitempty"`
	Attempts    int      `json:"attempts,omitempty"`
	Failures    int      `json:"failures,omitempty"`
	LastErr     string   `json:"lastError,omitempty"`
	NextRetryIn int      `json:"nextRetryIn,omitempty"`
	Output      []string `json:"output,omitempty"`
}

// RuntimeInfo is how this process was started.
type RuntimeInfo struct {
	Started time.Time
	Role    string // server.role
	Web     string // web.mode
	Storage string // storage.driver
}

const defaultMaxEventStreams = 5000

func init() {
	// Empty lists are sent as [], never null, so clients need no null checks.
	huma.DefaultArrayNullable = false
}

// Locales the M0 frontend ships messages for; the full BS set is imported in M1.
var Locales = []LocaleInfo{
	{Code: "zh-CN", Name: "简体中文"},
	{Code: "en", Name: "English"},
}

// docsPath is the API reference, a page whose viewer comes from a CDN. Huma gives it a
// Content-Security-Policy of its own instead of the backend's.
const docsPath = "/api/docs"

// New returns the HTTP handler and the Huma API (whose OpenAPI document can be exported).
func New(d Deps) (http.Handler, huma.API) {
	r := chi.NewRouter()
	r.Use(httpx.WithSecurityHeaders, httpx.WithBackendPolicy(docsPath), middleware.RequestID, middleware.Recoverer, requestLog(d.Log),
		withRequestInfo(d), csrfGuard(d), setupModeGuard(d), pluginUploadGuard(d))

	cfg := huma.DefaultConfig("SkinLoom API", buildinfo.Version)
	cfg.Info.Description = "SkinLoom (云织) REST API. Game protocols (CSL/USM/Legacy/Yggdrasil) are documented separately."
	cfg.Info.License = &huma.License{Name: "MIT", URL: "https://opensource.org/licenses/MIT"}
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = docsPath
	cfg.SchemasPath = "/api/schemas"
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: SessionCookie,
			Description: "Browser session from /api/v1/auth/login. State-changing requests must also send the " + CSRFHeader + " header."},
		"adminToken": {Type: "http", Scheme: "bearer", Description: "Admin token from data/secrets/admin.token, for scripts."},
	}
	api := humachi.New(r, cfg)

	registerSite(api, d)
	registerAuth(api, d)
	registerSetup(api, d)
	registerAccounts(api, d)
	registerMail(api, d)
	registerMe(api, d)
	registerAdminUsers(api, d)
	registerAdmin(api, d)
	registerTextures(api, d)
	registerPlayers(api, d)
	registerManage(api, d)
	registerReports(api, d)
	registerAdminPlayers(api, d)
	registerNotifications(api, d)
	registerYggdrasilAccount(api, d)
	registerOAuthAccount(api, d)
	registerSocial(api, d)
	registerStatus(api, d)
	registerCDN(api, d)
	registerAdminPlugins(api, d)
	mountSetupRaw(r, d)
	mountFiles(r, d)
	mountGame(r, d)
	mountRender(r, d)
	mountYggdrasil(r, d)
	mountOAuth(r, d)
	mountBSAPI(r, d)
	mountSocial(r, d)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		backendOK := true
		backendErr := ""
		if d.DB != nil {
			ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
			defer cancel()
			if d.DB.SQL.PingContext(ctx) != nil {
				backendOK = false
				backendErr = "database unavailable"
			}
		} else if d.Setup == nil {
			backendOK = false
			backendErr = "database unavailable"
		}
		webOK := d.WebReady == nil || d.WebReady()
		var ws WebStatusInfo
		if d.WebStatus != nil {
			ws = d.WebStatus()
			// Never leak raw process errors or output outside local dev mode.
			if ws.Mode != "dev" {
				ws.LastErr = ""
				ws.Output = nil
			}
		}
		if strings.Contains(strings.ToLower(req.Header.Get("Accept")), "application/json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			ready := backendOK && webOK
			if !ready {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			type backendPayload struct {
				OK    bool   `json:"ok"`
				Error string `json:"error,omitempty"`
			}
			type frontendPayload struct {
				Ready       bool     `json:"ready"`
				Mode        string   `json:"mode,omitempty"`
				State       string   `json:"state,omitempty"`
				Attempts    int      `json:"attempts,omitempty"`
				Failures    int      `json:"failures,omitempty"`
				LastError   string   `json:"lastError,omitempty"`
				NextRetryIn int      `json:"nextRetryIn,omitempty"`
				Output      []string `json:"output,omitempty"`
			}
			_ = json.NewEncoder(w).Encode(struct {
				Ready    bool            `json:"ready"`
				Backend  backendPayload  `json:"backend"`
				Frontend frontendPayload `json:"frontend"`
			}{
				Ready:   ready,
				Backend: backendPayload{OK: backendOK, Error: backendErr},
				Frontend: frontendPayload{
					Ready:       webOK,
					Mode:        ws.Mode,
					State:       ws.State,
					Attempts:    ws.Attempts,
					Failures:    ws.Failures,
					LastError:   ws.LastErr,
					NextRetryIn: ws.NextRetryIn,
					Output:      ws.Output,
				},
			})
			return
		}
		if !backendOK {
			http.Error(w, backendErr, http.StatusServiceUnavailable)
			return
		}
		if !webOK {
			if ws.State == "restarting" || ws.Failures > 0 {
				fails := max(ws.Failures, 1)
				if ws.Mode == "dev" && ws.LastErr != "" {
					http.Error(w, fmt.Sprintf("frontend failed (%d failures): %s", fails, ws.LastErr), http.StatusServiceUnavailable)
					return
				}
				http.Error(w, fmt.Sprintf("frontend failed (%d failures)", fails), http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "frontend starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready"))
	})
	r.Get("/events", serveEvents(d))
	// Password managers send people here to change a password (W3C "well-known URL").
	r.Get("/.well-known/change-password", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/user/profile", http.StatusFound)
	})
	if d.Themes != nil {
		r.Handle("/themes-assets/_custom/*", theme.CustomHandler(
			func() string {
				if d.Settings == nil {
					return ""
				}
				return d.Settings.Current().String("theme.custom_css")
			},
			func() string {
				if d.Settings == nil {
					return ""
				}
				return d.Settings.Current().String("theme.custom_js")
			}))
		r.Handle("/themes-assets/*", d.Themes.AssetHandlerWithTrust(trustThemeWebFn(d)))
	}
	if d.Plugins != nil {
		servePlug := func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			subpath := "/"
			if rest := chi.URLParam(r, "*"); rest != "" {
				subpath = "/" + strings.TrimPrefix(rest, "/")
			}
			ri := requestInfo(r.Context())
			d.Plugins.ServePluginAPI(w, r, id, subpath, pluginViewerFromReq(r, d), ri.locale)
		}
		r.HandleFunc("/api/plugins/{id}", servePlug)
		r.HandleFunc("/api/plugins/{id}/*", servePlug)
		r.Handle("/plugins-assets/*", d.Plugins.AssetHandler())
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			ri := requestInfo(req.Context())
			if d.Plugins.ServePublicRoute(w, req, pluginViewerFromReq(req, d), ri.locale) {
				return
			}
			http.NotFound(w, req)
		})
	}
	return r, api
}

func trustThemeWebFn(d Deps) func(string) bool {
	return func(themeID string) bool {
		if d.Plugins != nil && d.Plugins.HasTrustedThemeWeb(themeID) {
			return true
		}
		if d.Settings == nil {
			return false
		}
		snap := d.Settings.Current()
		if snap == nil {
			return false
		}
		return slices.Contains(settings.Decode[[]string](snap, "theme.trusted_web"), themeID)
	}
}

func isHostAuthBearer(authHeader, adminToken string) bool {
	hdr := strings.TrimSpace(authHeader)
	if len(hdr) < 7 || !strings.EqualFold(hdr[:7], "Bearer ") {
		return false
	}
	tok := strings.TrimSpace(hdr[7:])
	if tok == "" {
		return false
	}
	if adminToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(adminToken)) == 1 {
		return true
	}
	return strings.HasPrefix(tok, "slo_") || strings.HasPrefix(tok, "slp_") || strings.HasPrefix(tok, "slr_")
}

func pluginViewerFromReq(r *http.Request, d Deps) plugin.Viewer {
	hasHostAuth := isHostAuthBearer(r.Header.Get("Authorization"), d.AdminToken)
	if v := requestInfo(r.Context()).viewer(); v != nil && v.User != nil {
		return plugin.Viewer{
			Authenticated:     true,
			UserID:            v.User.ID,
			Email:             v.User.Email,
			Nickname:          v.User.Nickname,
			Role:              string(v.User.Role),
			Admin:             v.IsAdmin(),
			Superadmin:        v.User.Role == user.RoleSuperadmin,
			HasHostAuthHeader: hasHostAuth,
		}
	}
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok &&
		d.AdminToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(d.AdminToken)) == 1 {
		return plugin.Viewer{
			Authenticated:     true,
			Role:              "superadmin",
			Admin:             true,
			Superadmin:        true,
			HasHostAuthHeader: true,
		}
	}
	return plugin.Viewer{HasHostAuthHeader: hasHostAuth}
}

// setupModeGuard refuses all business, game, and OAuth routes while the server is in
// Pre-Core Setup Mode (d.Setup != nil && d.DB == nil), preventing handlers from touching
// uninitialized services or a nil database.
func setupModeGuard(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if d.Setup == nil || d.DB != nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !setupAllowedPath(r.URL.Path) {
				writeProblem(w, http.StatusServiceUnavailable, "setup not completed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func setupAllowedPath(p string) bool {
	switch p {
	case "/api/v1/site/bootstrap", "/api/v1/setup", "/events", "/healthz", "/readyz", "/api/docs":
		return true
	}
	return strings.HasPrefix(p, "/api/v1/setup/") ||
		strings.HasPrefix(p, "/api/openapi") ||
		strings.HasPrefix(p, "/api/schemas/") ||
		strings.HasPrefix(p, "/themes-assets/")
}

func requestLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/events" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"took", time.Since(start).Round(time.Microsecond), "request_id", middleware.GetReqID(r.Context()))
		})
	}
}

// checkAdmin accepts an admin session or the admin bearer token. It returns the acting
// user's id (nil for the token) for audit records and settings.updated_by.
func checkAdmin(ctx context.Context, d Deps, authorization string) (*int64, error) {
	if v := requestInfo(ctx).viewer(); v != nil {
		if !v.IsAdmin() {
			return nil, huma.Error403Forbidden("administrators only")
		}
		return &v.User.ID, nil
	}
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || d.AdminToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(d.AdminToken)) != 1 {
		return nil, huma.Error401Unauthorized("sign in as an administrator or send the admin token")
	}
	return nil, nil
}
