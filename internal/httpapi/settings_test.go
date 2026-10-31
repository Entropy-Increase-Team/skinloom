package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Secrets are write-only: the admin panel learns whether one is set, never the value
// (Blessing Skin showed the reCAPTCHA secret key in a plain text field), and a secret
// it sends back redacted is kept.
func TestAdminSettingsHideSecrets(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken
	providers := `[{"id":"github","type":"github","clientId":"gh-id","clientSecret":"gh-secret"}]`
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth,
		`{"mail.smtp.password":"smtp-secret","captcha.turnstile.secret_key":"ts-secret","auth.social.providers":`+providers+`}`); rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	list := func() map[string]SettingView {
		t.Helper()
		rec := do(t, h, http.MethodGet, "/api/v1/admin/settings", auth, "")
		for _, secret := range []string{"smtp-secret", "ts-secret", "gh-secret"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatalf("%s left the server: %s", secret, rec.Body)
			}
		}
		var out SettingsList
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		views := map[string]SettingView{}
		for _, s := range out.Settings {
			views[s.Key] = s
		}
		return views
	}
	views := list()
	if pw := views["mail.smtp.password"]; !pw.Secret || !pw.IsSet || pw.Value != nil {
		t.Fatalf("mail.smtp.password = %+v", pw)
	}
	shown := views["auth.social.providers"].Value.([]any)[0].(map[string]any)
	if shown["clientSecret"] != settings.Redacted {
		t.Fatalf("provider shown as %v", shown)
	}

	// The panel saves what it was shown, with another client id: the secret stays.
	shown["clientId"] = "gh-id-2"
	body, _ := json.Marshal(map[string]any{"auth.social.providers": []any{shown}})
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, string(body)); rec.Code != http.StatusOK {
		t.Fatalf("save redacted: %d %s", rec.Code, rec.Body)
	}
	saved := settings.Decode[[]map[string]string](d.Settings.Current(), "auth.social.providers")
	if len(saved) != 1 || saved[0]["clientId"] != "gh-id-2" || saved[0]["clientSecret"] != "gh-secret" {
		t.Fatalf("saved %v", saved)
	}
	// A provider whose id changed has no saved secret to keep: GitHub needs one.
	shown["id"] = "gh"
	body, _ = json.Marshal(map[string]any{"auth.social.providers": []any{shown}})
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, string(body)); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "clientSecret") {
		t.Fatalf("new id with a redacted secret: %d %s", rec.Code, rec.Body)
	}
	if got := settings.Decode[[]map[string]string](d.Settings.Current(), "auth.social.providers"); got[0]["id"] != "github" {
		t.Fatalf("a refused save changed the setting: %v", got)
	}

	// The panel also learns the choices and bounds, checked on save.
	views = list()
	if v := views["mail.driver"]; len(v.Enum) != 3 {
		t.Fatalf("mail.driver enum %v", v.Enum)
	}
	if v := views["mail.smtp.port"]; v.Min == nil || *v.Min != 1 || v.Max == nil || *v.Max != 65535 {
		t.Fatalf("mail.smtp.port range %v %v", v.Min, v.Max)
	}
	if v := views["score.initial"]; v.Min == nil || *v.Min != 0 || v.Max == nil || *v.Max != 1_000_000_000 {
		t.Fatalf("score.initial range %v %v", v.Min, v.Max)
	}
	rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, `{"mail.driver":"pigeon","mail.smtp.port":0,"score.initial":-1}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "want one of [none smtp log]") ||
		!strings.Contains(rec.Body.String(), "must be 1 to 65535") || !strings.Contains(rec.Body.String(), "must be 0 to 1000000000") {
		t.Fatalf("out of range: %d %s", rec.Code, rec.Body)
	}
}

// With auth.require_email_verification on, an unverified account still signs in and
// uses its closet, but cannot manage players, upload, report or issue credentials, as
// behind Blessing Skin's verified middleware; verifying lifts it.
func TestRequireEmailVerification(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	b := registerUser(t, h, "u@example.com")
	rec := b.do("POST", "/api/v1/me/tokens", `{"name":"script","scopes":["Player.ReadWrite"],"expiresInDays":1}`)
	var pat NewPersonalToken
	_ = json.Unmarshal(rec.Body.Bytes(), &pat)
	if rec.Code != http.StatusCreated {
		t.Fatalf("token before the requirement: %d %s", rec.Code, rec.Body)
	}
	setSettings(t, d, `{"auth.require_email_verification": true}`)

	blocked := []struct{ method, path, body string }{
		{"POST", "/api/v1/me/players", `{"name":"Steve"}`},
		{"POST", "/api/v1/textures/1/reports", `{"reason":"not theirs"}`},
		{"POST", "/api/v1/me/tokens", `{"name":"script","scopes":["User.Read"],"expiresInDays":1}`},
		{"POST", "/api/v1/me/oauth/clients", `{"name":"app","redirectUris":["https://app.test/cb"],"confidential":true}`},
	}
	for _, c := range blocked {
		if rec := b.do(c.method, c.path, c.body); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"type":"`+ProblemEmailNotVerified+`"`) {
			t.Errorf("%s %s unverified: %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
	if rec := app(t, h, "POST", "/api/players", "application/json", `{"name":"Steve"}`, "Authorization", "Bearer "+pat.Token); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "验证邮箱") { // in the account's language, zh-CN
		t.Errorf("BS API unverified: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("GET", "/api/v1/me/closet", ""); rec.Code != http.StatusOK {
		t.Errorf("closet unverified: %d %s", rec.Code, rec.Body)
	}

	if err := d.DB.Ent.User.Update().Where(user.EmailNormalized("u@example.com")).SetEmailVerifiedAt(time.Now()).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rec := b.do("POST", "/api/v1/me/players", `{"name":"Steve"}`); rec.Code != http.StatusOK {
		t.Fatalf("verified: %d %s", rec.Code, rec.Body)
	}
}

// A kept SMTP password follows the server it was given for: a save that points the
// mail settings at another server has to give it again, or an admin could have the
// stored password sent to a host of their choosing.
func TestAdminSettingsSecretFollowsItsServer(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	auth := "Bearer " + testToken
	if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth,
		`{"mail.smtp.host":"smtp.example.com","mail.smtp.password":"smtp-secret"}`); rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body)
	}
	for _, body := range []string{`{"mail.smtp.host":"attacker.example"}`, `{"mail.smtp.encryption":"none"}`, `{"mail.smtp.host":null}`} {
		rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, body)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "body.mail.smtp.password") {
			t.Errorf("%s without the password: %d %s", body, rec.Code, rec.Body)
		}
	}
	if d.Settings.Current().String("mail.smtp.host") != "smtp.example.com" {
		t.Fatal("a refused save changed the host")
	}
	// The same server, or the password given again, is fine.
	for _, body := range []string{`{"mail.smtp.host":"smtp.example.com","mail.from":"a@example.com"}`,
		`{"mail.smtp.host":"smtp2.example.com","mail.smtp.password":"new-secret"}`} {
		if rec := do(t, h, http.MethodPatch, "/api/v1/admin/settings", auth, body); rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}
