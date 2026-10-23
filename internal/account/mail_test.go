package account

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
)

var withMail = MailContext{Enabled: true, BaseURL: "https://skins.example.com/", Locale: "en", Site: "Skins"}

// queuedLink returns the token in the newest queued mail whose link contains path.
func queuedLink(t *testing.T, s *Service, path string) (string, mail.Message) {
	t.Helper()
	jobs := s.db.Job.Query().Where(job.Kind(MailJobKind)).Order(ent.Desc(job.FieldID)).AllX(context.Background())
	for _, j := range jobs {
		var m mail.Message
		_ = json.Unmarshal([]byte(j.Payload), &m)
		if i := strings.Index(m.Text, "https://skins.example.com"+path); i >= 0 {
			token := m.Text[i+len("https://skins.example.com"+path):]
			return token[:tokenLen], m
		}
	}
	t.Fatalf("no queued mail with a %s link", path)
	return "", mail.Message{}
}

func TestPasswordReset(t *testing.T) { forEachDB(t, testPasswordReset) }

func testPasswordReset(t *testing.T, s *Service) {
	ctx := context.Background()
	var kicks atomic.Int32
	s.SetJobNotifier(func() { kicks.Add(1) })
	admin, oldSession := setupAdmin(t, s)
	if err := s.RequestPasswordReset(ctx, "admin@example.com", noMail, meta); !errors.Is(err, ErrMailDisabled) {
		t.Fatalf("without mail: %v", err)
	}

	if err := s.RequestPasswordReset(ctx, "nobody@example.com", withMail, meta); err != nil {
		t.Fatalf("unknown email must look like success: %v", err)
	}
	if n := s.db.Job.Query().CountX(ctx); n != 0 {
		t.Fatal("no mail for an unknown address")
	}
	if err := s.RequestPasswordReset(ctx, "ADMIN@example.com", withMail, meta); err != nil {
		t.Fatal(err)
	}
	first, msg := queuedLink(t, s, "/auth/reset/")
	if msg.To != "Admin@Example.com" || msg.Subject != "Reset your Skins password" || kicks.Load() != 1 {
		t.Fatalf("reset mail %+v (kicks %d)", msg, kicks.Load())
	}
	_ = s.RequestPasswordReset(ctx, "admin@example.com", withMail, meta)
	token, _ := queuedLink(t, s, "/auth/reset/")
	if err := s.ResetPassword(ctx, first, "new-pass-123", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("an older link must stop working once a newer one is sent: %v", err)
	}
	if err := s.ResetPassword(ctx, token, "short", meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("short password: %v", err)
	}
	if err := s.ResetPassword(ctx, token, "new-pass-123", meta); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Authenticate(ctx, oldSession.Token); v != nil {
		t.Fatal("a password reset must sign the user out everywhere (S02)")
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1"}, noCaptcha, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "new-pass-123"}, noCaptcha, meta); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if err := s.ResetPassword(ctx, token, "another-pass-1", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a reset link works once: %v", err)
	}

	// Expiry: 30 minutes.
	other := RequestMeta{IP: "198.51.100.40"}
	_ = s.RequestPasswordReset(ctx, "admin@example.com", withMail, other)
	late, _ := queuedLink(t, s, "/auth/reset/")
	real := s.now
	s.now = func() time.Time { return real().Add(ResetPasswordTTL + time.Second) }
	if err := s.ResetPassword(ctx, late, "late-pass-123", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired link: %v", err)
	}
	s.now = real
	_ = admin
}

func TestPasswordResetRateLimit(t *testing.T) { forEachDB(t, testPasswordResetRateLimit) }

func testPasswordResetRateLimit(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	var rl *RateLimitError
	for _, email := range []string{"admin@example.com", "nobody@example.com"} {
		for i := range 3 {
			if err := s.RequestPasswordReset(ctx, email, withMail, RequestMeta{IP: "198.51.100." + string(rune('1'+i))}); err != nil {
				t.Fatal(err)
			}
		}
		// The limit applies to unknown addresses too, so it reveals nothing.
		if err := s.RequestPasswordReset(ctx, email, withMail, RequestMeta{IP: "198.51.100.9"}); !errors.As(err, &rl) {
			t.Fatalf("%s: 4th request in an hour: %v", email, err)
		}
	}
}

func TestEmailVerification(t *testing.T) { forEachDB(t, testEmailVerification) }

func testEmailVerification(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	u, _, err := s.Register(ctx, RegisterInput{Email: "steve@example.com", Nickname: "Steve", Password: "steve-pass-1"}, openRegistration, withMail, meta)
	if err != nil {
		t.Fatal(err)
	}
	if u.EmailVerifiedAt != nil {
		t.Fatal("a new account starts unverified")
	}
	token, msg := queuedLink(t, s, "/auth/verify/")
	if msg.To != "steve@example.com" || !strings.Contains(msg.Text, "Hi Steve") {
		t.Fatalf("verification mail %+v", msg)
	}
	if err := s.ResendVerification(ctx, u.ID, noMail); !errors.Is(err, ErrMailDisabled) {
		t.Fatalf("resend without mail: %v", err)
	}
	if err := s.ResendVerification(ctx, u.ID, withMail); err != nil {
		t.Fatal(err)
	}
	var rl *RateLimitError
	if err := s.ResendVerification(ctx, u.ID, withMail); !errors.As(err, &rl) {
		t.Fatalf("second resend within a minute: %v", err)
	}
	if _, err := s.VerifyEmail(ctx, token, meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the first link was replaced by the resent one: %v", err)
	}
	newest, _ := queuedLink(t, s, "/auth/verify/")
	v, err := s.VerifyEmail(ctx, newest, meta)
	if err != nil || v.EmailVerifiedAt == nil {
		t.Fatalf("VerifyEmail = %v, %v", v, err)
	}
	if _, err := s.VerifyEmail(ctx, newest, meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a verification link works once: %v", err)
	}
	if err := s.ResendVerification(ctx, u.ID, withMail); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("resend after verification: %v", err)
	}
}
