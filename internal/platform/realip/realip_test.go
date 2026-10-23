package realip

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(remote string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://site.test/", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestNewRejectsBadCIDR(t *testing.T) {
	if _, err := New([]string{"10.0.0.0/8", "not-an-ip"}); err == nil {
		t.Fatal("want error")
	}
	if _, err := New([]string{"192.168.1.10", "::1", "fd00::/8"}); err != nil {
		t.Fatalf("plain IPs and IPv6 must be accepted: %v", err)
	}
}

func TestUntrustedPeerHeadersIgnored(t *testing.T) {
	res, _ := New(nil) // trust nobody (the default)
	r := req("203.0.113.7:51000", map[string]string{
		"X-Forwarded-For":   "1.2.3.4",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "evil.test",
	})
	info := res.Resolve(r)
	if info.ClientIP != "203.0.113.7" || info.Proto != "http" || info.Host != "site.test" || info.ViaTrustedProxy {
		t.Fatalf("spoofed headers accepted: %+v", info)
	}
}

func TestTrustedProxyChain(t *testing.T) {
	res, _ := New([]string{"10.0.0.0/8"})
	// client 198.51.100.9 → CDN 10.1.1.1 → nginx 10.2.2.2 → us
	r := req("10.2.2.2:443", map[string]string{
		"X-Forwarded-For":   "6.6.6.6, 198.51.100.9, 10.1.1.1",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "skin.example.com",
	})
	info := res.Resolve(r)
	if info.ClientIP != "198.51.100.9" {
		t.Fatalf("client IP %q: must be the right-most untrusted hop, not the spoofable left-most", info.ClientIP)
	}
	if info.Proto != "https" || info.Host != "skin.example.com" || !info.ViaTrustedProxy {
		t.Fatalf("trusted proxy headers not honoured: %+v", info)
	}
}

func TestTrustedProxyWithoutHeaders(t *testing.T) {
	res, _ := New([]string{"127.0.0.1/32"})
	info := res.Resolve(req("127.0.0.1:9000", nil))
	if info.ClientIP != "127.0.0.1" || info.Proto != "http" {
		t.Fatalf("%+v", info)
	}
}

func TestBadForwardedValuesFallBack(t *testing.T) {
	res, _ := New([]string{"127.0.0.1/32"})
	info := res.Resolve(req("127.0.0.1:9000", map[string]string{
		"X-Forwarded-For":   "garbage, also-garbage",
		"X-Forwarded-Proto": "gopher",
	}))
	if info.ClientIP != "127.0.0.1" || info.Proto != "http" {
		t.Fatalf("invalid forwarded values must be ignored: %+v", info)
	}
}

func TestTLSRequest(t *testing.T) {
	res, _ := New(nil)
	r := httptest.NewRequest(http.MethodGet, "https://site.test/", nil)
	if info := res.Resolve(r); info.Proto != "https" {
		t.Fatalf("direct TLS: proto %q", info.Proto)
	}
}
