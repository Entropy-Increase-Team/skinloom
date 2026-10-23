package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

func TestReserved(t *testing.T) {
	cases := map[string]bool{
		"/api/v1/site/bootstrap":                 true,
		"/api":                                   true,
		"/Notch.json":                            true, // CustomSkinLoader profile
		"/csl/Notch.json":                        true,
		"/textures/abc":                          true,
		"/api/yggdrasil/authserver/authenticate": true,
		"/events":                                true,
		"/themes-assets/default/0.1.0/theme.css": true,
		"/":                                      false,
		"/skinlib":                               false,
		"/skinlib/show/12":                       false,
		"/user/closet":                           false,
		"/_next/static/chunks/main.js":           false,
		"/favicon.ico":                           false,
		"/docs/a.json":                           false, // only single-segment .json is CSL
		"/apiary":                                false,
	}
	for p, want := range cases {
		if got := Reserved(p); got != want {
			t.Errorf("Reserved(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestSafeJoinRefusesTraversal(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"/../etc/passwd", "/a/../../x", "/..\\..\\x", "/\x00x"} {
		got := safeJoin(root, p)
		if got == "" {
			continue
		}
		if rel, err := filepath.Rel(root, got); err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("safeJoin(%q) escaped root: %s", p, got)
		}
	}
}

type harness struct {
	gw       *Gateway
	upstream *httptest.Server
	seen     chan *http.Request
	api      chan string
}

func newHarness(t *testing.T, trusted []string) *harness {
	t.Helper()
	h := &harness{seen: make(chan *http.Request, 4), api: make(chan string, 4)}
	h.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.seen <- r.Clone(r.Context())
		w.Header().Set("X-From", "frontend")
		_, _ = w.Write([]byte("page"))
	}))
	t.Cleanup(h.upstream.Close)
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.api <- r.URL.Path
		_, _ = w.Write([]byte("api"))
	})
	res, err := realip.New(trusted)
	if err != nil {
		t.Fatal(err)
	}
	h.gw = New(api, slog.New(slog.NewTextHandler(io.Discard, nil)), res)
	u, _ := url.Parse(h.upstream.URL)
	h.gw.SetWeb(u, "")
	return h
}

func (h *harness) do(method, target, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.gw.ServeHTTP(rec, r)
	return rec
}

// Regression: paths were routed before cleaning, so /x/../api/... reached the frontend
// and /api/../user reached the backend router.
func TestUncleanPathsAreRedirected(t *testing.T) {
	h := newHarness(t, nil)
	cases := map[string]string{
		"/x/../api/v1/site/bootstrap": "/api/v1/site/bootstrap",
		"//api/v1/site/bootstrap":     "/api/v1/site/bootstrap",
		"/api/../user?tab=1":          "/user?tab=1",
		"/skinlib/./show/1":           "/skinlib/show/1",
	}
	for in, want := range cases {
		rec := h.do(http.MethodGet, in, "203.0.113.1:1", nil)
		if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
			t.Errorf("%s: status %d Location %q, want 308 %q", in, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	if rec := h.do(http.MethodGet, "/skinlib/", "203.0.113.1:1", nil); rec.Code != http.StatusOK {
		t.Errorf("trailing slash must not redirect: %d", rec.Code)
	}
}

func TestRoutingAndSecurityHeaders(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodGet, "/api/v1/site/bootstrap", "203.0.113.1:1", nil)
	if rec.Body.String() != "api" || <-h.api != "/api/v1/site/bootstrap" {
		t.Fatalf("reserved path not sent to the backend: %q", rec.Body)
	}
	rec = h.do(http.MethodGet, "/skinlib", "203.0.113.1:1", nil)
	if rec.Body.String() != "page" || rec.Header().Get("X-From") != "frontend" {
		t.Fatalf("page not proxied: %q", rec.Body)
	}
	<-h.seen
	for _, hdr := range []string{"X-Content-Type-Options", "Referrer-Policy", "X-Frame-Options"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("proxied page missing %s", hdr)
		}
	}
}

func TestForwardedHeadersFromUntrustedPeerAreReplaced(t *testing.T) {
	h := newHarness(t, nil)
	h.do(http.MethodGet, "http://site.test/skinlib", "203.0.113.5:4000", map[string]string{
		"X-Forwarded-For":   "1.1.1.1",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "evil.test",
	})
	got := <-h.seen
	if got.Header.Get("X-Forwarded-For") != "203.0.113.5" || got.Header.Get("X-Forwarded-Proto") != "http" ||
		got.Header.Get("X-Forwarded-Host") != "site.test" {
		t.Fatalf("spoofed headers reached the frontend: %v", got.Header)
	}
}

// Regression: behind an HTTPS reverse proxy the frontend was told proto=http, so it
// rendered http:// addresses (e.g. the authlib-injector API URL).
func TestForwardedHeadersFromTrustedProxyArePreserved(t *testing.T) {
	h := newHarness(t, []string{"10.0.0.0/8"})
	h.do(http.MethodGet, "http://internal:8080/", "10.0.0.2:4000", map[string]string{
		"X-Forwarded-For":   "198.51.100.9",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "skin.example.com",
	})
	got := <-h.seen
	if got.Header.Get("X-Forwarded-Proto") != "https" || got.Header.Get("X-Forwarded-Host") != "skin.example.com" ||
		got.Header.Get("X-Forwarded-For") != "198.51.100.9" || got.Host != "skin.example.com" {
		t.Fatalf("trusted proxy headers lost: host=%s %v", got.Host, got.Header)
	}
}

func TestPlaceholderWhenNoFrontend(t *testing.T) {
	res, _ := realip.New(nil)
	gw := New(http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)), res)
	gw.ClearWeb(`<script>alert(1)</script>`)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "<script>alert") {
		t.Fatalf("placeholder: %d, escaped=%v", rec.Code, !strings.Contains(rec.Body.String(), "<script>alert"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("placeholder missing security headers")
	}
}

func TestStaticAssetsServedFromDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".next", "static", "chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".next", "static", "chunks", "a.js"), []byte("js"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "public"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "public", "robots.txt"), []byte("ok"), 0o644)

	h := newHarness(t, nil)
	u, _ := url.Parse(h.upstream.URL)
	h.gw.SetWeb(u, dir)

	rec := h.do(http.MethodGet, "/_next/static/chunks/a.js", "203.0.113.1:1", nil)
	if rec.Body.String() != "js" || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("static chunk: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Cache-Control"))
	}
	if rec := h.do(http.MethodGet, "/_next/static/chunks", "203.0.113.1:1", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("directory must 404, got %d", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/robots.txt", "203.0.113.1:1", nil); rec.Body.String() != "ok" {
		t.Fatalf("public file: %q", rec.Body)
	}
}
