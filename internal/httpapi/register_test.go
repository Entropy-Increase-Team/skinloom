package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
)

func setSettings(t *testing.T, d Deps, body string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Settings.Set(context.Background(), nil, m, nil); err != nil {
		t.Fatal(err)
	}
}

// fakeTurnstile accepts the token "pass" and counts siteverify calls.
func fakeTurnstile(t *testing.T, d *Deps) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		ok := r.PostForm.Get("response") == "pass" && r.PostForm.Get("secret") == "ts-secret"
		_, _ = w.Write([]byte(`{"success": ` + map[bool]string{true: "true", false: "false"}[ok] + `}`))
	}))
	t.Cleanup(srv.Close)
	d.Captcha = captcha.NewService(captcha.Options{Endpoints: map[string]string{captcha.Turnstile: srv.URL}})
	setSettings(t, *d, `{"captcha.provider": "turnstile", "captcha.turnstile.site_key": "ts-site", "captcha.turnstile.secret_key": "ts-secret"}`)
	return calls
}

func fieldOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Errors []struct{ Location string } `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if len(p.Errors) == 0 {
		return ""
	}
	return p.Errors[0].Location
}

func TestRegisterFlow(t *testing.T) {
	d := newTestDeps(t)
	calls := fakeTurnstile(t, &d)
	h, _ := New(d)
	b := &browser{t: t, h: h}
	form := func(token string) string {
		return `{"email":"Steve@example.com","nickname":"史蒂夫","password":"steve-pass-1","captcha":{"value":"` + token + `"}}`
	}

	if rec := b.do("POST", "/api/v1/auth/register", form("pass")); rec.Code != http.StatusForbidden {
		t.Fatalf("before setup: %d %s", rec.Code, rec.Body)
	}
	setupSite(t, d, &browser{t: t, h: h})

	if rec := b.do("POST", "/api/v1/auth/register", `{"email":"steve@example.com","nickname":"S","password":"steve-pass-1"}`); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.captcha" {
		t.Fatalf("no captcha: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("POST", "/api/v1/auth/register", form("fail")); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.captcha" {
		t.Fatalf("wrong captcha: %d %s", rec.Code, rec.Body)
	}
	before := calls.Load()
	if rec := b.do("POST", "/api/v1/auth/register", `{"email":"bad","nickname":"S","password":"x","captcha":{"value":"pass"}}`); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) == "body.captcha" {
		t.Fatalf("invalid fields: %d %s", rec.Code, rec.Body)
	}
	if calls.Load() != before {
		t.Fatal("a form with field errors must not spend the captcha")
	}

	rec := b.do("POST", "/api/v1/auth/register", form("pass"))
	if rec.Code != http.StatusOK || b.cookie == "" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var me UserView
	_ = json.Unmarshal(b.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if me.Role != "user" || me.Nickname != "史蒂夫" || me.Email != "Steve@example.com" {
		t.Fatalf("me after registration: %+v", me)
	}
	again := &browser{t: t, h: h}
	if rec := again.do("POST", "/api/v1/auth/register", strings.Replace(form("pass"), "Steve@", "STEVE@", 1)); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.email" {
		t.Fatalf("duplicate email: %d %s", rec.Code, rec.Body)
	}

	setSettings(t, d, `{"auth.registration": "closed"}`)
	if rec := again.do("POST", "/api/v1/auth/register", strings.Replace(form("pass"), "Steve@", "alex@", 1)); rec.Code != http.StatusForbidden {
		t.Fatalf("closed: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginAsksForCaptcha(t *testing.T) {
	d := newTestDeps(t)
	fakeTurnstile(t, &d)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	b := &browser{t: t, h: h}
	for range 3 {
		b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"wrong-pass-1"}`)
	}
	rec := b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1"}`)
	if rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.captcha" {
		t.Fatalf("after 3 failures: %d %s, want 422 on body.captcha", rec.Code, rec.Body)
	}
	rec = b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"admin-pass-1","captcha":{"value":"pass"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("with captcha: %d %s", rec.Code, rec.Body)
	}
}

func TestCaptchaImageEndpoint(t *testing.T) {
	d := newTestDeps(t) // captcha.provider defaults to image
	h, _ := New(d)
	rec := do(t, h, "GET", "/api/v1/captcha/image", "", "")
	var img CaptchaImage
	_ = json.Unmarshal(rec.Body.Bytes(), &img)
	if rec.Code != http.StatusOK || img.ID == "" || !strings.HasPrefix(img.Image, "data:image/png;base64,") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image captcha: %d %s", rec.Code, rec.Header())
	}
	// One client cannot render images without end (CPU, and evicting others' challenges).
	for range captchaImagesPerClient - 1 {
		do(t, h, "GET", "/api/v1/captcha/image", "", "")
	}
	if rec := do(t, h, "GET", "/api/v1/captcha/image", "", ""); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("image flood from one client: %d", rec.Code)
	}
	setSettings(t, d, `{"captcha.provider": "none"}`)
	if rec := do(t, h, "GET", "/api/v1/captcha/image", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("image captcha while disabled: %d", rec.Code)
	}
}

func TestBootstrapAuthInfo(t *testing.T) {
	d := newTestDeps(t)
	fakeTurnstile(t, &d)
	h, _ := New(d)
	var boot Bootstrap
	_ = json.Unmarshal(do(t, h, "GET", "/api/v1/site/bootstrap", "", "").Body.Bytes(), &boot)
	if boot.Auth.Registration != "open" || boot.Auth.Captcha.Provider != "turnstile" || boot.Auth.Captcha.SiteKey != "ts-site" {
		t.Fatalf("auth info %+v", boot.Auth)
	}
	raw := do(t, h, "GET", "/api/v1/site/bootstrap", "", "").Body.String()
	if strings.Contains(raw, "ts-secret") {
		t.Fatal("the captcha secret must never reach the browser")
	}
}
