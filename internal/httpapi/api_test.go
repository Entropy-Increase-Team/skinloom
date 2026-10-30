package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

const testToken = "0123456789abcdef0123456789abcdef"

func newTestDeps(t *testing.T) Deps {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "t.db", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatal(err)
	}
	broker := pubsub.New()
	bus := hook.NewBus(log, 1)
	t.Cleanup(func() { bus.Close(context.Background()) })
	themeReg := theme.NewRegistry()
	if err := themeReg.LoadDir(themes.Builtin, "builtin"); err != nil {
		t.Fatal(err)
	}
	reg := settings.NewRegistry()
	if err := reg.Register(slices.Concat(account.SettingDefs(), captcha.SettingDefs(), mail.SettingDefs(),
		score.SettingDefs(), texture.SettingDefs(), player.SettingDefs(), yggdrasil.SettingDefs(), oauth.SettingDefs(), social.SettingDefs())...); err != nil {
		t.Fatal(err)
	}
	reg.SetValidator("theme.active", themeReg.ValidateActive)
	svc := settings.NewService(reg, database.Ent, broker, bus, log)
	if err := svc.Load(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(dir + "/textures")
	if err != nil {
		t.Fatal(err)
	}
	textures := texture.NewService(database.Ent, store, log)
	deps := Deps{Log: log, DB: database, Settings: svc, Themes: themeReg, Broker: broker, AdminToken: testToken,
		Accounts: account.NewService(database.Ent, log), Captcha: captcha.NewService(captcha.Options{}), Mailer: mail.New(log),
		Textures: textures, Players: player.NewService(database.Ent, log), Reports: report.NewService(database.Ent, textures, log),
		Renders: render.NewService(2, 1<<20), Notify: notify.NewService(database.Ent, log)}
	deps.Yggdrasil = yggdrasil.NewService(database.Ent, deps.Accounts, deps.Players, textures, log)
	deps.OAuth = oauth.NewService(database.Ent, log)
	deps.Social = social.NewService(database.Ent, deps.Accounts, log)
	deps.Yggdrasil.KeyBits = 1024 // 4096 takes seconds to generate
	return deps
}

func do(t *testing.T, h http.Handler, method, path, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminAuth(t *testing.T) {
	h, _ := New(newTestDeps(t))
	cases := []struct {
		name, auth string
		want       int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + testToken, http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"right token", "Bearer " + testToken, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, p := range []string{"/api/v1/admin/settings", "/api/v1/admin/themes"} {
				if rec := do(t, h, http.MethodGet, p, c.auth, ""); rec.Code != c.want {
					t.Errorf("GET %s: status %d, want %d (%s)", p, rec.Code, c.want, rec.Body)
				}
			}
			rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", c.auth, `{"site.url":"https://a.test"}`)
			if rec.Code != c.want {
				t.Errorf("PATCH: status %d, want %d (%s)", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestAdminSettingsValidationAndReset(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken

	rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, `{"theme.active":"nope","site.url":"ftp://x","no.such":1}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", rec.Code, rec.Body)
	}
	var problem struct {
		Errors []struct{ Location string } `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &problem)
	if len(problem.Errors) != 3 {
		t.Fatalf("want 3 field errors, got %s", rec.Body)
	}

	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, `{"site.name":{"en":"Test"}}`); rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	if got := d.Settings.Current().Localized("site.name", "en"); got != "Test" {
		t.Fatalf("site.name = %q", got)
	}
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, `{"site.name":null}`); rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if d.Settings.Current().IsSet("site.name") {
		t.Fatal("null must reset to the default")
	}
}

func TestBootstrapLocaleAndHeaders(t *testing.T) {
	h, _ := New(newTestDeps(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/site/bootstrap?lang=xx-evil", nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var b Bootstrap
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Locale != "en" || b.Site.Name != "SkinLoom" || b.Theme.ID != "default" {
		t.Fatalf("unexpected bootstrap: locale=%s name=%s theme=%s", b.Locale, b.Site.Name, b.Theme.ID)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing headers: %v", rec.Header())
	}
}

// Regression: an open SSE tab kept http.Server.Shutdown waiting for its full timeout.
// Closing the broker (done on shutdown) must end every stream promptly.
func TestEventsEndOnBrokerClose(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, "retry:") {
		t.Fatalf("first line %q", line)
	}

	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, r); close(done) }()
	d.Broker.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream still open after broker close")
	}
}

func TestEventsConnectionLimit(t *testing.T) {
	d := newTestDeps(t)
	d.MaxEventStreams = 1
	h, _ := New(d)
	srv := httptest.NewServer(h)
	defer srv.Close()

	first, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	_, _ = bufio.NewReader(first.Body).ReadString('\n')

	second, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second stream: status %d, want 503", second.StatusCode)
	}
}

func TestPickLocale(t *testing.T) {
	cases := []struct{ lang, cookie, accept, want string }{
		{"", "", "", "zh-CN"},
		{"en", "", "zh-CN", "en"},
		{"", "en", "zh-CN", "en"},
		{"xx", "yy", "fr-FR,en-GB;q=0.8", "en"},
		{"", "", "zh-TW,zh;q=0.9", "zh-CN"},
		{"EN_us", "", "", "en"},
	}
	for _, c := range cases {
		if got := PickLocale(c.lang, c.cookie, c.accept); got != c.want {
			t.Errorf("PickLocale(%q,%q,%q) = %q, want %q", c.lang, c.cookie, c.accept, got, c.want)
		}
	}
}
