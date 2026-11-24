// Package gateway is the single public entry point. Backend-owned paths go to the Go
// handler; Next.js build assets are served from disk; every other path is proxied to
// the Next.js process. See docs/design/04 §2.1 for the path ownership table.
package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/httpx"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

// reservedPrefixes are owned by the backend. The frontend must never define routes
// under them (checked in CI against the Next.js route list).
var reservedPrefixes = []string{
	"/api/", "/oauth/", "/.well-known/",
	"/csl/", "/usm/", "/skin/", "/cape/", "/textures/", "/raw/",
	"/avatar/", "/preview/",
	"/plugins-assets/", "/themes-assets/",
}

var reservedExact = map[string]bool{"/api": true, "/events": true, "/healthz": true, "/readyz": true}

// cslProfile matches the CustomSkinLoader profile path /{player}.json at the root.
var cslProfile = regexp.MustCompile(`^/[^/]+\.json$`)

// Reserved reports whether the backend owns path.
func Reserved(p string) bool {
	if reservedExact[p] || cslProfile.MatchString(p) {
		return true
	}
	for _, prefix := range reservedPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// Gateway routes requests between the backend and the frontend.
type Gateway struct {
	api   atomic.Pointer[http.Handler]
	log   *slog.Logger
	state atomic.Pointer[webState]
	real  atomic.Pointer[realip.Resolver]
	// headers adds response headers to every answer, pages included (see SetHeaders).
	headers atomic.Pointer[func(http.Header)]
	// publicRoute matches root-level paths owned by active plugins via routes.public.
	publicRoute atomic.Pointer[func(string) bool]
	// webStatus reports the supervised frontend's startup state for the waiting page.
	webStatus atomic.Pointer[func() WebStatus]
}

// SetHeaders sets a function that adds headers to every response, such as
// X-Authlib-Injector-API-Location, which launchers look for on the site's front page.
func (g *Gateway) SetHeaders(f func(http.Header)) { g.headers.Store(&f) }

// SetPublicRouteMatcher installs a matcher for plugin-owned root paths (routes.public).
// Paths matched here are forwarded to the backend handler after built-in Reserved paths.
func (g *Gateway) SetPublicRouteMatcher(f func(string) bool) {
	if f == nil {
		g.publicRoute.Store(nil)
		return
	}
	g.publicRoute.Store(&f)
}

// SetAPI atomically replaces the backend API handler (used to transition from Setup Mode
// to Running Mode without restarting the process).
func (g *Gateway) SetAPI(h http.Handler) {
	if h == nil {
		h = http.NotFoundHandler()
	}
	g.api.Store(&h)
}

// ServeAPI dispatches directly to the current backend API handler (used when server.role = api).
func (g *Gateway) ServeAPI(w http.ResponseWriter, r *http.Request) {
	(*g.api.Load()).ServeHTTP(w, r)
}

type webState struct {
	staticDir string // <server.js dir>/.next/static, served under /_next/static/
	publicDir string // <server.js dir>/public
	proxy     *httputil.ReverseProxy
	reason    string // why there is no frontend, for the placeholder page
}

// New builds a gateway with no frontend attached. res decides which peers may set
// X-Forwarded-* headers (swap it at runtime with SetResolver).
func New(api http.Handler, log *slog.Logger, res *realip.Resolver) *Gateway {
	g := &Gateway{log: log}
	g.SetAPI(api)
	g.state.Store(&webState{reason: "前端尚未启动 / The web frontend is not running."})
	g.SetResolver(res)
	return g
}

// ClientInfo resolves r with the current trusted proxy list.
func (g *Gateway) ClientInfo(r *http.Request) realip.Info { return g.real.Load().Resolve(r) }

// SetResolver replaces the trusted proxy configuration (hot reload).
func (g *Gateway) SetResolver(res *realip.Resolver) {
	if res == nil {
		res, _ = realip.New(nil)
	}
	g.real.Store(res)
}

// SetWeb points the gateway at a running frontend. serverDir is the directory holding
// server.js for managed mode (empty for dev/external, where the upstream serves assets).
func (g *Gateway) SetWeb(upstream *url.URL, serverDir string) {
	st := &webState{}
	if serverDir != "" {
		st.staticDir = firstExistingDir(
			filepath.Join(serverDir, ".next", "static"),
			filepath.Join(serverDir, "..", "..", "..", "static"),
		)
		st.publicDir = firstExistingDir(
			filepath.Join(serverDir, "public"),
			filepath.Join(serverDir, "..", "..", "..", "..", "public"),
		)
	}
	if upstream != nil {
		st.proxy = &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				// Rewrite runs after the inbound X-Forwarded-* headers were stripped, so
				// we decide what the frontend sees: the resolved client view, which only
				// honours forwarding headers from trusted proxies.
				info := g.real.Load().Resolve(r.In)
				r.SetURL(upstream)
				r.Out.Host = info.Host // Next.js builds absolute URLs from the public host
				r.Out.Header.Set("X-Forwarded-For", info.ClientIP)
				r.Out.Header.Set("X-Forwarded-Host", info.Host)
				r.Out.Header.Set("X-Forwarded-Proto", info.Proto)
			},
			ModifyResponse: func(resp *http.Response) error {
				httpx.SecurityHeaders(resp.Header)
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				g.log.Debug("frontend unreachable", "path", r.URL.Path, "err", err)
				g.servePlaceholder(w, r, http.StatusBadGateway, "前端正在启动，请稍后刷新 / The web frontend is starting, refresh in a moment.", true)
			},
		}
	}
	g.state.Store(st)
}

func firstExistingDir(primary, fallback string) string {
	if info, err := os.Stat(primary); err == nil && info.IsDir() {
		return primary
	}
	if info, err := os.Stat(fallback); err == nil && info.IsDir() {
		return filepath.Clean(fallback)
	}
	return primary
}

// ClearWeb detaches the frontend and shows reason on page requests.
func (g *Gateway) ClearWeb(reason string) {
	g.state.Store(&webState{reason: reason})
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f := g.headers.Load(); f != nil {
		(*f)(w.Header())
	}
	p := r.URL.Path
	// Route on the cleaned path only. Like http.ServeMux, redirect "/a/../b" and "//b"
	// so path ownership cannot be sidestepped and caches see one canonical URL.
	if clean := cleanPath(p); clean != p {
		u := *r.URL
		u.Path, u.RawPath = clean, ""
		http.Redirect(w, r, u.RequestURI(), http.StatusPermanentRedirect)
		return
	}
	if Reserved(p) {
		(*g.api.Load()).ServeHTTP(w, r)
		return
	}
	if match := g.publicRoute.Load(); match != nil && (*match)(p) {
		(*g.api.Load()).ServeHTTP(w, r)
		return
	}
	st := g.state.Load()
	if st.staticDir != "" && strings.HasPrefix(p, "/_next/static/") {
		serveFile(w, r, st.staticDir, strings.TrimPrefix(p, "/_next/static/"), "public, max-age=31536000, immutable")
		return
	}
	if st.publicDir != "" && p != "/" && !strings.HasPrefix(p, "/_next/") {
		if f := safeJoin(st.publicDir, p); f != "" {
			if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
				w.Header().Set("Cache-Control", "public, max-age=3600")
				httpx.SecurityHeaders(w.Header())
				http.ServeFile(w, r, f)
				return
			}
		}
	}
	if st.proxy == nil {
		g.servePlaceholder(w, r, http.StatusServiceUnavailable, st.reason, false)
		return
	}
	st.proxy.ServeHTTP(w, r)
}

// cleanPath is path.Clean that keeps a trailing slash.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	c := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}
	return c
}

func serveFile(w http.ResponseWriter, r *http.Request, root, rel, cache string) {
	f := safeJoin(root, "/"+rel)
	if f == "" {
		http.NotFound(w, r)
		return
	}
	if info, err := os.Stat(f); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", cache)
	httpx.SecurityHeaders(w.Header())
	http.ServeFile(w, r, f)
}

// safeJoin maps a URL path inside root, refusing traversal.
func safeJoin(root, urlPath string) string {
	clean := path.Clean("/" + urlPath)
	if strings.ContainsAny(clean, "\x00\\:") {
		return "" // backslashes and drive letters are path syntax on Windows
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	if rel, err := filepath.Rel(root, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return full
}
