package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
)

// lastLink returns the token of the newest queued mail linking to path.
func lastLink(t *testing.T, d Deps, path string) string {
	t.Helper()
	j, err := d.DB.Ent.Job.Query().Where(job.Kind(account.MailJobKind)).Order(ent.Desc(job.FieldID)).First(context.Background())
	if err != nil {
		t.Fatalf("no mail queued: %v", err)
	}
	var m mail.Message
	_ = json.Unmarshal([]byte(j.Payload), &m)
	i := strings.Index(m.Text, path)
	if i < 0 {
		t.Fatalf("newest mail has no %s link:\n%s", path, m.Text)
	}
	return m.Text[i+len(path) : i+len(path)+43]
}

func TestForgotAndResetPassword(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	b := &browser{t: t, h: h}

	if rec := b.do("POST", "/api/v1/auth/password/forgot", `{"email":"admin@example.com"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without mail: %d", rec.Code)
	}
	// Without site.url there is nothing safe to build links from: the request's Host
	// header is chosen by the client (password reset poisoning).
	setSettings(t, d, `{"mail.driver": "log", "site.url": ""}`)
	if rec := b.do("POST", "/api/v1/auth/password/forgot", `{"email":"admin@example.com"}`); rec.Code != http.StatusServiceUnavailable ||
		!strings.Contains(rec.Body.String(), "site.url") {
		t.Fatalf("without site.url: %d %s", rec.Code, rec.Body)
	}
	if rec := b.do("GET", "/api/v1/site/bootstrap", ""); !strings.Contains(rec.Body.String(), `"mail":false`) {
		t.Fatalf("bootstrap must not offer mail features without site.url: %s", rec.Body)
	}

	setSettings(t, d, `{"site.url": "https://skins.example.com"}`)
	attacker := &browser{t: t, h: h, headers: map[string]string{"Host": "evil.example"}}
	known := attacker.do("POST", "/api/v1/auth/password/forgot", `{"email":"admin@example.com"}`)
	unknown := b.do("POST", "/api/v1/auth/password/forgot", `{"email":"ghost@example.com"}`)
	if known.Code != http.StatusAccepted || unknown.Code != http.StatusAccepted || known.Body.String() != unknown.Body.String() {
		t.Fatalf("forgot must answer the same for any address: %d %q / %d %q", known.Code, known.Body, unknown.Code, unknown.Body)
	}
	// The link uses site.url whatever Host the request named.
	token := lastLink(t, d, "https://skins.example.com/auth/reset/")

	if rec := b.do("POST", "/api/v1/auth/password/reset", `{"token":"`+token+`","password":"short"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d", rec.Code)
	}
	if rec := b.do("POST", "/api/v1/auth/password/reset", `{"token":"`+token+`","password":"brand-new-pass-1"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("existing sessions must end after a reset: %d", rec.Code)
	}
	if rec := b.do("POST", "/api/v1/auth/password/reset", `{"token":"`+token+`","password":"other-pass-123"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("reused link: %d", rec.Code)
	}
	if rec := b.do("POST", "/api/v1/auth/login", `{"email":"admin@example.com","password":"brand-new-pass-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("login with the new password: %d %s", rec.Code, rec.Body)
	}
}

func TestEmailVerificationFlow(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "mail.driver": "log", "site.url": "https://skins.example.com"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})

	if rec := (&browser{t: t, h: h}).do("POST", "/api/v1/auth/email/resend", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("resend while signed out: %d", rec.Code)
	}
	b := &browser{t: t, h: h}
	if rec := b.do("POST", "/api/v1/auth/register", `{"email":"steve@example.com","nickname":"Steve","password":"steve-pass-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	token := lastLink(t, d, "https://skins.example.com/auth/verify/")
	stranger := &browser{t: t, h: h} // the link works from any browser, signed in or not
	if rec := stranger.do("POST", "/api/v1/auth/email/verify", `{"token":"`+token+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body)
	}
	var me UserView
	_ = json.Unmarshal(b.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if !me.EmailVerified {
		t.Fatal("me.emailVerified after verification")
	}
	if rec := b.do("POST", "/api/v1/auth/email/resend", ""); rec.Code != http.StatusConflict {
		t.Fatalf("resend when verified: %d", rec.Code)
	}
	if rec := stranger.do("POST", "/api/v1/auth/email/verify", `{"token":"`+token+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("reused verification link: %d", rec.Code)
	}
}

func TestAdminTestMail(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	if rec := admin.do("POST", "/api/v1/admin/mail/test", `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("mail.driver none: %d", rec.Code)
	}
	setSettings(t, d, `{"mail.driver": "log"}`)
	if rec := admin.do("POST", "/api/v1/admin/mail/test", `{}`); rec.Code != http.StatusNoContent {
		t.Fatalf("test mail to the signed-in admin: %d %s", rec.Code, rec.Body)
	}
	setSettings(t, d, `{"mail.driver": "smtp", "mail.from": "x@example.com", "mail.smtp.host": "127.0.0.1", "mail.smtp.port": 1, "mail.smtp.encryption": "none"}`)
	rec := admin.do("POST", "/api/v1/admin/mail/test", `{}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "connect to 127.0.0.1:1") {
		t.Fatalf("admins see the SMTP error: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, "POST", "/api/v1/admin/mail/test", "Bearer "+testToken, `{}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("token without a recipient: %d", rec.Code)
	}
	bootMail := func() bool {
		var boot Bootstrap
		_ = json.Unmarshal(do(t, h, "GET", "/api/v1/site/bootstrap", "", "").Body.Bytes(), &boot)
		return boot.Auth.Mail
	}
	if bootMail() {
		t.Fatal("bootstrap.auth.mail needs site.url too: mail links are built from it")
	}
	setSettings(t, d, `{"site.url": "https://skins.example.com"}`)
	if !bootMail() {
		t.Fatal("bootstrap.auth.mail must say mail is configured")
	}
}
