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

	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/httpx"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
)

// Deps are the services the API needs.
type Deps struct {
	Log      *slog.Logger
	DB       *db.DB // nil when only generating the OpenAPI document
	Settings *settings.Service
	Themes   *theme.Registry
	Broker   *pubsub.Broker
	// AdminToken guards /api/v1/admin until accounts and roles land in M1.
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
	r.Use(httpx.WithSecurityHeaders, middleware.RequestID, middleware.Recoverer, requestLog(d.Log))

	cfg := huma.DefaultConfig("SkinLoom API", buildinfo.Version)
	cfg.Info.Description = "SkinLoom (云织) REST API. Game protocols (CSL/USM/Legacy/Yggdrasil) are documented separately."
	cfg.Info.License = &huma.License{Name: "MIT", URL: "https://opensource.org/licenses/MIT"}
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = "/api/schemas"
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"adminToken": {Type: "http", Scheme: "bearer", Description: "Temporary admin token from data/secrets/admin.token (replaced by sessions and roles in M1)."},
	}
	api := humachi.New(r, cfg)

	registerSite(api, d)
	registerAdmin(api, d)

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

// checkAdmin validates the temporary bearer token.
func checkAdmin(d Deps, authorization string) error {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || d.AdminToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(d.AdminToken)) != 1 {
		return huma.Error401Unauthorized("admin token required")
	}
	return nil
}
