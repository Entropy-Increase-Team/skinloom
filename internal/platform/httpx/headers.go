// Package httpx holds small HTTP helpers shared by the API and the gateway.
package httpx

import "net/http"

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
