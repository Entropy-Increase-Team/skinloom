// Package gateway is the single public entry point. Backend-owned paths go to the Go
// handler; Next.js build assets are served from disk; every other path is proxied to
// the Next.js process. See docs/design/04 §2.1 for the path ownership table.
package gateway

import (
	"html/template"
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
	api   http.Handler
	log   *slog.Logger
	state atomic.Pointer[webState]
	real  atomic.Pointer[realip.Resolver]
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
	g := &Gateway{api: api, log: log}
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
		st.staticDir = filepath.Join(serverDir, ".next", "static")
		st.publicDir = filepath.Join(serverDir, "public")
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
				placeholder(w, http.StatusBadGateway, "前端正在启动，请稍后刷新 / The web frontend is starting, refresh in a moment.")
			},
		}
	}
	g.state.Store(st)
}

// ClearWeb detaches the frontend and shows reason on page requests.
func (g *Gateway) ClearWeb(reason string) {
	g.state.Store(&webState{reason: reason})
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		g.api.ServeHTTP(w, r)
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
		placeholder(w, http.StatusServiceUnavailable, st.reason)
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

var placeholderTmpl = template.Must(template.New("p").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="5"><title>SkinLoom</title>
<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#f5f7fb;color:#1f2937}
main{max-width:32rem;padding:2rem;text-align:center}h1{font-size:1.25rem}p{color:#4b5563;line-height:1.6}code{background:#e5e7eb;padding:.1rem .3rem;border-radius:.25rem}</style>
</head><body><main><h1>云织 SkinLoom</h1><p>{{.}}</p><p>后端运行正常：<code>/healthz</code>。本页每 5 秒自动刷新。</p></main></body></html>`))

func placeholder(w http.ResponseWriter, status int, reason string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	httpx.SecurityHeaders(h)
	w.WriteHeader(status)
	_ = placeholderTmpl.Execute(w, reason)
}
