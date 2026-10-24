// Package httpapi builds the backend HTTP surface: /api/v1 (Huma, OpenAPI 3.1), the
// SSE change stream, health probes and theme assets. Game protocols mount here too as
// their modules land.
package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/httpx"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
)

// Deps are the services the API needs.
type Deps struct {
	Log      *slog.Logger
	DB       *db.DB // nil when only generating the OpenAPI document
	Settings *settings.Service
	Themes   *theme.Registry
	Broker   *pubsub.Broker
	Accounts *account.Service
	Captcha  *captcha.Service
	Mailer   *mail.Mailer
	Textures *texture.Service
	// ClientInfo resolves the client address and public host behind trusted proxies
	// (the gateway's live resolver). Nil = trust no proxy.
	ClientInfo func(*http.Request) realip.Info
	// AdminToken also authorizes /api/v1/admin, for scripts, until personal access
	// tokens land (M3). Browsers use an admin session.
	AdminToken string
	// MaxEventStreams caps concurrent /events connections (0 = default 5000), so a
	// flood of idle SSE clients cannot exhaust memory and file descriptors.
	MaxEventStreams int
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

// New returns the HTTP handler and the Huma API (whose OpenAPI document can be exported).
func New(d Deps) (http.Handler, huma.API) {
	r := chi.NewRouter()
	r.Use(httpx.WithSecurityHeaders, middleware.RequestID, middleware.Recoverer, requestLog(d.Log),
		withRequestInfo(d), csrfGuard(d))

	cfg := huma.DefaultConfig("SkinLoom API", buildinfo.Version)
	cfg.Info.Description = "SkinLoom (云织) REST API. Game protocols (CSL/USM/Legacy/Yggdrasil) are documented separately."
	cfg.Info.License = &huma.License{Name: "MIT", URL: "https://opensource.org/licenses/MIT"}
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = "/api/schemas"
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: SessionCookie,
			Description: "Browser session from /api/v1/auth/login. State-changing requests must also send the " + CSRFHeader + " header."},
		"adminToken": {Type: "http", Scheme: "bearer", Description: "Admin token from data/secrets/admin.token, for scripts until personal access tokens land (M3)."},
	}
	api := humachi.New(r, cfg)

	registerSite(api, d)
	registerAuth(api, d)
	registerAccounts(api, d)
	registerMail(api, d)
	registerMe(api, d)
	registerAdminUsers(api, d)
	registerAdmin(api, d)
	registerTextures(api, d)
	mountFiles(r, d)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if d.DB == nil || d.DB.SQL.PingContext(ctx) != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
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
		r.Handle("/themes-assets/*", d.Themes.AssetHandler())
	}
	return r, api
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
