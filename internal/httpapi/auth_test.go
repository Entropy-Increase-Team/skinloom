package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

// browser sends requests the way the frontend does: JSON, the CSRF header, and
// whatever session cookie it holds.
type browser struct {
	t       *testing.T
	h       http.Handler
	cookie  string
	headers map[string]string
}

func (b *browser) do(method, path, body string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CSRFHeader, "1")
	for k, v := range b.headers {
		if k == "Host" {
			req.Host = v // what servers read; the header map does not set it
			continue
		}
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	if b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: b.cookie})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			b.cookie = c.Value
		}
	}
	return rec
}

func setupSite(t *testing.T, d Deps, b *browser) {
	t.Helper()
	token, err := d.Accounts.NewSetupToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec := b.do("POST", "/api/v1/setup", `{"token":"`+token+`","email":"admin@example.com","nickname":"Admin","password":"admin-pass-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
}

func TestSetupFlow(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	b := &browser{t: t, h: h}

	if rec := b.do("GET", "/api/v1/setup", ""); !strings.Contains(rec.Body.String(), `"needsSetup":true`) {
		t.Fatalf("fresh site: %s", rec.Body)
	}
	token, _ := d.Accounts.NewSetupToken(context.Background())
	body := `{"token":"` + token + `","email":"admin@example.com","nickname":"Admin","password":"admin-pass-1",` +
		`"siteName":"我的皮肤站","siteUrl":"https://skins.example.com/"}`

	noHeader := &browser{t: t, h: h, headers: map[string]string{CSRFHeader: ""}}
	if rec := noHeader.do("POST", "/api/v1/setup", body); rec.Code != http.StatusForbidden {
		t.Fatalf("setup without %s: %d, want 403", CSRFHeader, rec.Code)
	}
	if rec := b.do("POST", "/api/v1/setup", strings.Replace(body, token, account.NewToken(), 1)); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("POST", "/api/v1/setup", strings.Replace(body, "https://skins", "javascript:skins", 1)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad site URL must fail before the admin is created: %d %s", rec.Code, rec.Body)
	}

	rec := b.do("POST", "/api/v1/setup", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	c := rec.Result().Cookies()[0]
	if c.Name != SessionCookie || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.MaxAge != 0 {
		t.Fatalf("session cookie %+v: want HttpOnly, SameSite=Lax, no Secure over http, browser-session lifetime", c)
	}
	if got := d.Settings.Current().String("site.url"); got != "https://skins.example.com" {
		t.Fatalf("site.url = %q", got)
	}
	if got := d.Settings.Current().Localized("site.name", "en"); got != "我的皮肤站" {
		t.Fatalf("site.name = %q", got)
	}
	me := b.do("GET", "/api/v1/auth/me", "")
	var u UserView
	_ = json.Unmarshal(me.Body.Bytes(), &u)
	if me.Code != http.StatusOK || u.Role != "superadmin" || u.Email != "admin@example.com" {
		t.Fatalf("me: %d %s", me.Code, me.Body)
	}
	if rec := b.do("POST", "/api/v1/setup", body); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: %d, want 409 (already set up)", rec.Code)
	}
}

func TestLoginLogoutAndAdmin(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	b := &browser{t: t, h: h}
	wrong := b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"nope-nope-1"}`)
	unknown := b.do("POST", "/api/v1/auth/login", `{"email":"ghost@example.com","password":"nope-nope-1"}`)
	if wrong.Code != http.StatusUnauthorized || wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("failures must be indistinguishable:\n%s\n%s", wrong.Body, unknown.Body)
	}
	if rec := b.do("POST", "/api/v1/auth/login", `{"email":"ADMIN@example.com","password":"admin-pass-1","remember":true}`); rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	} else if c := rec.Result().Cookies()[0]; c.MaxAge < 29*24*3600 {
		t.Fatalf("remember me cookie must persist: %+v", c)
	}

	// An admin session manages settings; the cookie alone is not enough (CSRF).
	if rec := b.do("PATCH", "/api/v1/admin/settings", `{"site.description":{"zh-CN":"x","en":"x"}}`); rec.Code != http.StatusOK {
		t.Fatalf("admin session PATCH: %d %s", rec.Code, rec.Body)
	}
	forged := &browser{t: t, h: h, cookie: b.cookie, headers: map[string]string{CSRFHeader: ""}}
	if rec := forged.do("PATCH", "/api/v1/admin/settings", `{"site.description":null}`); rec.Code != http.StatusForbidden {
		t.Fatalf("cookie without %s: %d, want 403", CSRFHeader, rec.Code)
	}
	crossSite := &browser{t: t, h: h, cookie: b.cookie, headers: map[string]string{"Origin": "https://evil.example"}}
	if rec := crossSite.do("PATCH", "/api/v1/admin/settings", `{"site.description":null}`); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: %d, want 403", rec.Code)
	}
	sameSite := &browser{t: t, h: h, cookie: b.cookie, headers: map[string]string{"Origin": "http://example.com"}}
	if rec := sameSite.do("PATCH", "/api/v1/admin/settings", `{"site.description":null}`); rec.Code != http.StatusOK {
		t.Fatalf("same-origin PATCH: %d %s", rec.Code, rec.Body)
	}
	// Scripts with the bearer token need neither cookie nor CSRF header.
	if rec := do(t, h, "PATCH", "/api/v1/admin/settings", "Bearer "+testToken, `{"site.description":null}`); rec.Code != http.StatusOK {
		t.Fatalf("bearer PATCH: %d %s", rec.Code, rec.Body)
	}

	if rec := b.do("POST", "/api/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if b.cookie != "" {
		t.Fatal("logout must clear the cookie")
	}
	if rec := b.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}
}

func TestNonAdminCannotUseAdminAPI(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	ctx := context.Background()
	hash, _ := account.NewHasher(1).Hash(ctx, "user-pass-1")
	d.DB.Ent.User.Create().SetEmail("u@example.com").SetEmailNormalized("u@example.com").SetNickname("U").
		SetPasswordHash(hash).ExecX(ctx)

	b := &browser{t: t, h: h}
	if rec := b.do("POST", "/api/v1/auth/login", `{"email":"u@example.com","password":"user-pass-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("GET", "/api/v1/admin/settings", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("user session on admin API: %d, want 403", rec.Code)
	}
}

func TestLoginRateLimitAndSecureCookie(t *testing.T) {
	d := newTestDeps(t)
	// as if behind a trusted TLS-terminating proxy
	d.ClientInfo = func(*http.Request) realip.Info {
		return realip.Info{ClientIP: "198.51.100.4", Proto: "https", Host: "skins.example.com"}
	}
	setSettings(t, d, `{"captcha.provider": "none"}`) // the hard limit itself, not the captcha in front of it
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	b := &browser{t: t, h: h, headers: map[string]string{"Origin": "https://skins.example.com"}}
	rec := b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1"}`)
	if rec.Code != http.StatusOK || !rec.Result().Cookies()[0].Secure {
		t.Fatalf("https login must set a Secure cookie: %d %+v", rec.Code, rec.Result().Cookies())
	}
	for range 10 {
		b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"wrong-pass-1"}`)
	}
	rec = b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after 10 failures: %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// Regression: signing in again (e.g. switching accounts) left the browser's previous
// session valid in the database until it expired.
func TestLoginReplacesPreviousSession(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	b := &browser{t: t, h: h}
	b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1"}`)
	first := b.cookie
	b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1"}`)
	if b.cookie == first {
		t.Fatal("a new login must issue a new session")
	}
	old := &browser{t: t, h: h, cookie: first}
	if rec := old.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the replaced session still works: %d", rec.Code)
	}
}
