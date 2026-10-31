package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
)

func TestTextOverrides(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken

	for name, body := range map[string]string{
		"locale":        `{"i18n.overrides": {"chinese": {"nav.skinlib": "x"}}}`,
		"key":           `{"i18n.overrides": {"en": {"nav skinlib": "x"}}}`,
		"braces":        `{"i18n.overrides": {"en": {"hero.join": "Join {site"}}}`,
		"closing first": `{"i18n.overrides": {"en": {"hero.join": "}{"}}}`,
		"not text":      `{"i18n.overrides": {"en": {"hero.join": 1}}}`,
		"too long":      `{"i18n.overrides": {"en": {"hero.join": "` + strings.Repeat("x", 2001) + `"}}}`,
	} {
		if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	ok := `{"i18n.overrides": {"zh-CN": {"nav.skinlib": "皮肤广场", "auth.signInTo": "登录 {site}"}, "en": {"nav.skinlib": "Gallery"}}}`
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, ok); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	// The bootstrap carries the visitor's language only.
	for lang, want := range map[string]map[string]string{
		"zh-CN": {"nav.skinlib": "皮肤广场", "auth.signInTo": "登录 {site}"},
		"en":    {"nav.skinlib": "Gallery"},
	} {
		var b Bootstrap
		_ = json.Unmarshal(do(t, h, "GET", "/api/v1/site/bootstrap?lang="+lang, "", "").Body.Bytes(), &b)
		if len(b.Messages) != len(want) || b.Messages["nav.skinlib"] != want["nav.skinlib"] {
			t.Errorf("%s messages %v", lang, b.Messages)
		}
	}
	setSettings(t, d, `{"i18n.overrides": null}`)
	rec := do(t, h, "GET", "/api/v1/site/bootstrap?lang=en", "", "")
	if !strings.Contains(rec.Body.String(), `"messages":{}`) {
		t.Fatalf("no overrides: %s", rec.Body)
	}
}

func TestSystemStatus(t *testing.T) {
	d := newTestDeps(t)
	d.Runtime = RuntimeInfo{Started: time.Now(), Role: "all", Web: "managed", Storage: "local"}
	h, _ := New(d)
	auth := "Bearer " + testToken

	if rec := do(t, h, "GET", "/api/v1/admin/status", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	if err := jobs.EnqueueAt(t.Context(), d.DB.Ent, "test.stuck", map[string]int{}, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, "GET", "/api/v1/admin/status", auth, "")
	var s SystemStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if s.Database.Dialect != "sqlite" || s.Database.Version == "" || s.Database.Migration == 0 || s.Database.Pending != 0 ||
		s.Server.Web != "managed" || s.Storage.Driver != "local" || s.Config.Theme != "default" {
		t.Fatalf("status %+v", s)
	}
	if s.Jobs.Pending != 1 || s.Jobs.Overdue != 1 || s.Jobs.OldestOverdue == nil {
		t.Fatalf("jobs %+v", s.Jobs)
	}
	levels := map[string]string{}
	for _, c := range s.Checks {
		levels[c.ID] = c.Level
	}
	want := map[string]string{"database": "ok", "migrations": "ok", "jobs_overdue": "warn", "jobs_failed": "ok", "mail": "warn", "site_url": "warn"}
	for id, level := range want {
		if levels[id] != level {
			t.Errorf("check %s = %q, want %q", id, levels[id], level)
		}
	}
	if strings.Contains(rec.Body.String(), "t.db") {
		t.Fatal("the database location must not be shown")
	}
}

func TestPurgeCDN(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken
	body := `{"players": ["Steve"]}`
	if rec := do(t, h, "POST", "/api/v1/admin/cdn/purge", auth, body); rec.Code != http.StatusConflict {
		t.Fatalf("no CDN: %d %s", rec.Code, rec.Body)
	}
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URLs []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		got = in.URLs
		if strings.Contains(r.URL.Path, "fail") {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	setSettings(t, d, `{"cdn.provider": "webhook", "cdn.webhook.url": "`+srv.URL+`", "site.url": "https://skin.example.com"}`)
	rec := do(t, h, "POST", "/api/v1/admin/cdn/purge", auth, body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"urls":6`) || len(got) != 6 || got[0] != "https://skin.example.com/Steve.json" {
		t.Fatalf("purge: %d %s %v", rec.Code, rec.Body, got)
	}
	setSettings(t, d, `{"cdn.webhook.url": "`+srv.URL+`/fail"}`)
	if rec := do(t, h, "POST", "/api/v1/admin/cdn/purge", auth, body); rec.Code != http.StatusBadGateway {
		t.Fatalf("provider refuses: %d %s", rec.Code, rec.Body)
	}
}

// /readyz waits for the frontend the process runs, so a probe does not route visitors
// to pages that still fail.
func TestReadyzWaitsForTheFrontend(t *testing.T) {
	d := newTestDeps(t)
	up := false
	d.WebReady = func() bool { return up }
	h, _ := New(d)
	if rec := do(t, h, "GET", "/readyz", "", ""); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "frontend") {
		t.Fatalf("frontend down: %d %s", rec.Code, rec.Body)
	}
	up = true
	if rec := do(t, h, "GET", "/readyz", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("frontend up: %d", rec.Code)
	}
}
