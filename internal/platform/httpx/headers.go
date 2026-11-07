// Package httpx holds small HTTP helpers shared by the API and the gateway.
package httpx

import (
	"net/http"
	"slices"
)

var securityDefaults = [...][2]string{
	{"X-Content-Type-Options", "nosniff"},
	{"Referrer-Policy", "strict-origin-when-cross-origin"},
	{"X-Frame-Options", "SAMEORIGIN"},
}

// SecurityHeaders sets conservative defaults on h without overriding values a handler
// (or the frontend) chose. HSTS is left to the TLS-terminating proxy.
func SecurityHeaders(h http.Header) {
	for _, kv := range securityDefaults {
		if h.Get(kv[0]) == "" {
			h.Set(kv[0], kv[1])
		}
	}
}

// WithSecurityHeaders applies SecurityHeaders before next writes its response.
func WithSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SecurityHeaders(w.Header())
		next.ServeHTTP(w, r)
	})
}

// BackendPolicy is the Content-Security-Policy of the backend's answers: JSON, images,
// texture files, redirects. None is a page, so nothing opened from one may run a script,
// load anything or be framed by another site; the inline styles are the ones browsers
// give an image opened on its own. Pages carry the frontend's policy (ADR 0039).
const BackendPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox"

// WithBackendPolicy sets BackendPolicy on every answer except those of the paths in
// except (pages the backend renders itself, such as the API reference).
func WithBackendPolicy(except ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(except, r.URL.Path) {
				w.Header().Set("Content-Security-Policy", BackendPolicy)
			}
			next.ServeHTTP(w, r)
		})
	}
}
