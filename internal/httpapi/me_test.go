package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// registerUser signs a new ordinary user up in a fresh browser.
func registerUser(t *testing.T, h http.Handler, email string) *browser {
	t.Helper()
	b := &browser{t: t, h: h}
	if rec := b.do("POST", "/api/v1/auth/register", `{"email":"`+email+`","nickname":"U","password":"user-pass-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("register %s: %d %s", email, rec.Code, rec.Body)
	}
	return b
}

func TestMeEndpoints(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	if rec := (&browser{t: t, h: h}).do("GET", "/api/v1/me/sessions", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}
	b := registerUser(t, h, "u@example.com")
	laptop := &browser{t: t, h: h}
	laptop.do("POST", "/api/v1/auth/login", `{"email":"u@example.com","password":"user-pass-1"}`)

	if rec := b.do("PATCH", "/api/v1/me", `{"locale":"klingon"}`); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.locale" {
		t.Fatalf("unsupported language (S04): %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("PATCH", "/api/v1/me", `{"nickname":"新名字","locale":"en"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"locale":"en"`) {
		t.Fatalf("update profile: %d %s", rec.Code, rec.Body)
	}

	var sessions []SessionView
	_ = json.Unmarshal(b.do("GET", "/api/v1/me/sessions", "").Body.Bytes(), &sessions)
	if len(sessions) != 2 {
		t.Fatalf("want 2 devices, got %+v", sessions)
	}

	if rec := b.do("POST", "/api/v1/me/password", `{"currentPassword":"nope-nope-1","newPassword":"next-pass-123"}`); rec.Code != http.StatusUnprocessableEntity || fieldOf(t, rec) != "body.password" {
		t.Fatalf("wrong current password: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("POST", "/api/v1/me/password", `{"currentPassword":"user-pass-1","newPassword":"next-pass-123"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body)
	}
	if rec := laptop.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the other device must be signed out (S08): %d", rec.Code)
	}
	if rec := b.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusOK {
		t.Fatalf("this device stays signed in: %d", rec.Code)
	}

	if rec := b.do("POST", "/api/v1/me/delete", `{"password":"next-pass-123"}`); rec.Code != http.StatusOK || b.cookie != "" {
		t.Fatalf("delete account: %d, cookie %q", rec.Code, b.cookie)
	}
	if rec := b.do("POST", "/api/v1/auth/login", `{"email":"u@example.com","password":"next-pass-123"}`); rec.Code != http.StatusOK {
		t.Fatalf("signing in within 30 days restores the account: %d %s", rec.Code, rec.Body)
	}

	rec := do(t, h, "GET", "/.well-known/change-password", "", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/user/profile" {
		t.Fatalf("change-password well-known URL: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminUserEndpoints(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	super := &browser{t: t, h: h}
	setupSite(t, d, super)
	player := registerUser(t, h, "player@example.com")
	mod := registerUser(t, h, "mod@example.com")

	var page UserPage
	_ = json.Unmarshal(super.do("GET", "/api/v1/admin/users?q=player", "").Body.Bytes(), &page)
	if len(page.Users) != 1 || page.Users[0].Email != "player@example.com" {
		t.Fatalf("search: %+v", page)
	}
	playerID, modID := page.Users[0].ID, int64(0)
	_ = json.Unmarshal(super.do("GET", "/api/v1/admin/users?q=mod", "").Body.Bytes(), &page)
	modID = page.Users[0].ID

	if rec := player.do("GET", "/api/v1/admin/users", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("ordinary user on the admin API: %d", rec.Code)
	}
	if rec := super.do("PATCH", fmt.Sprintf("/api/v1/admin/users/%d", modID), `{"role":"admin"}`); rec.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", rec.Code, rec.Body)
	}
	// The moderator can now ban ordinary users, but not change roles.
	if rec := mod.do("PATCH", fmt.Sprintf("/api/v1/admin/users/%d", playerID), `{"role":"admin"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("admin changing a role: %d", rec.Code)
	}
	if rec := mod.do("PATCH", fmt.Sprintf("/api/v1/admin/users/%d", playerID), `{"status":"banned"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"banned"`) {
		t.Fatalf("ban: %d %s", rec.Code, rec.Body)
	}
	if rec := player.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a banned user is signed out: %d", rec.Code)
	}

	var me UserView
	_ = json.Unmarshal(super.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if rec := do(t, h, "PATCH", fmt.Sprintf("/api/v1/admin/users/%d", me.ID), "Bearer "+testToken, `{"role":"user"}`); rec.Code != http.StatusConflict {
		t.Fatalf("demoting the last super admin: %d %s", rec.Code, rec.Body)
	}
	if rec := super.do("DELETE", fmt.Sprintf("/api/v1/admin/users/%d", playerID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := super.do("GET", fmt.Sprintf("/api/v1/admin/users/%d", playerID), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted user: %d", rec.Code)
	}

	var audit AuditPage
	_ = json.Unmarshal(super.do("GET", "/api/v1/admin/audit?action=admin.", "").Body.Bytes(), &audit)
	if len(audit.Records) != 3 || audit.Records[0].Action != "admin.user_deleted" || audit.Records[0].ActorID == nil {
		t.Fatalf("audit: %+v", audit.Records)
	}
}
