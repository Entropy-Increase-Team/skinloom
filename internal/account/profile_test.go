package account

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/ent/session"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// newUser registers an ordinary user and returns their viewer and session token.
func newUser(t *testing.T, s *Service, email string) (*Viewer, string) {
	t.Helper()
	ctx := context.Background()
	_, sess, err := s.Register(ctx, RegisterInput{Email: email, Nickname: "U", Password: "user-pass-1"}, RegisterPolicy{Open: true}, noMail, meta)
	if err != nil {
		t.Fatal(err)
	}
	return signedIn(t, s, sess.Token), sess.Token
}

func signedIn(t *testing.T, s *Service, token string) *Viewer {
	t.Helper()
	v, err := s.Authenticate(context.Background(), token)
	if err != nil || v == nil {
		t.Fatalf("Authenticate: %v, %v", v, err)
	}
	return v
}

// otherDevice signs the same account in again and returns that session's token.
func otherDevice(t *testing.T, s *Service, email, password string) string {
	t.Helper()
	_, sess, err := s.Login(context.Background(), LoginInput{Email: email, Password: password}, noCaptcha, RequestMeta{IP: "198.51.100.200"})
	if err != nil {
		t.Fatal(err)
	}
	return sess.Token
}

func TestUpdateProfile(t *testing.T) { forEachDB(t, testUpdateProfile) }

func testUpdateProfile(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	v, _ := newUser(t, s, "u@example.com")
	locales := []string{"zh-CN", "en"}
	bad, empty := "xx; DROP", ""
	if _, err := s.UpdateProfile(ctx, v.User.ID, ProfileUpdate{Locale: &bad}, locales); len(FieldErrors(err)) != 1 {
		t.Fatalf("unknown language must be refused (S04): %v", err)
	}
	nick, en := "新昵称", "en"
	u, err := s.UpdateProfile(ctx, v.User.ID, ProfileUpdate{Nickname: &nick, Locale: &en}, locales)
	if err != nil || u.Nickname != nick || u.Locale != "en" {
		t.Fatalf("UpdateProfile = %+v, %v", u, err)
	}
	if u, _ := s.UpdateProfile(ctx, v.User.ID, ProfileUpdate{Locale: &empty}, locales); u.Locale != "" {
		t.Fatal("empty locale = follow the browser")
	}
}

func TestChangePassword(t *testing.T) { forEachDB(t, testChangePassword) }

func testChangePassword(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	v, _ := newUser(t, s, "u@example.com")
	other := otherDevice(t, s, "u@example.com", "user-pass-1")
	if err := s.RequestPasswordReset(ctx, "u@example.com", withMail, meta); err != nil {
		t.Fatal(err)
	}
	pendingReset, _ := queuedLink(t, s, "/auth/reset/")

	if err := s.ChangePassword(ctx, v, "wrong-pass-1", "next-pass-123", meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := s.ChangePassword(ctx, v, "user-pass-1", "short", meta); len(FieldErrors(err)) != 1 || FieldErrors(err)[0].Field != "newPassword" {
		t.Fatalf("short new password: %v", err)
	}
	if err := s.ChangePassword(ctx, v, "user-pass-1", "next-pass-123", meta); err != nil {
		t.Fatal(err)
	}
	if x, _ := s.Authenticate(ctx, other); x != nil {
		t.Fatal("other devices must be signed out after a password change (S08)")
	}
	if x := s.db.Session.GetX(ctx, v.SessionID); x.RevokedAt != nil {
		t.Fatal("the session that changed the password stays signed in")
	}
	if err := s.ResetPassword(ctx, pendingReset, "overwrite-1", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a pending reset link must stop working after a password change: %v", err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "u@example.com", Password: "next-pass-123"}, noCaptcha, meta); err != nil {
		t.Fatalf("new password: %v", err)
	}
	v = signedIn(t, s, otherDevice(t, s, "u@example.com", "next-pass-123")) // fresh user record
	var rl *RateLimitError
	for range 5 {
		_ = s.ChangePassword(ctx, v, "wrong-pass-1", "x-pass-1234", meta)
	}
	if err := s.ChangePassword(ctx, v, "next-pass-123", "x-pass-1234", meta); !errors.As(err, &rl) {
		t.Fatalf("guessing the current password through a stolen session must be limited: %v", err)
	}
}

func TestChangeEmail(t *testing.T) { forEachDB(t, testChangeEmail) }

func testChangeEmail(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	_, token := newUser(t, s, "u@example.com")
	s.db.User.Update().Where(user.EmailNormalized("u@example.com")).SetEmailVerifiedAt(time.Now()).ExecX(ctx)
	v := signedIn(t, s, token)
	other := otherDevice(t, s, "u@example.com", "user-pass-1")
	// A reset link waiting in the old inbox must die with the move: whoever reads that
	// inbox is exactly who the user may be moving away from.
	if err := s.RequestPasswordReset(ctx, "u@example.com", withMail, meta); err != nil {
		t.Fatal(err)
	}
	oldReset, _ := queuedLink(t, s, "/auth/reset/")

	// Only the case changes: same mailbox, nothing to verify, nobody signed out.
	u, err := s.ChangeEmail(ctx, v, "user-pass-1", "U@Example.com", withMail, meta)
	if err != nil || u.Email != "U@Example.com" || u.EmailVerifiedAt == nil {
		t.Fatalf("case-only change: %+v, %v", u, err)
	}
	if x, _ := s.Authenticate(ctx, other); x == nil {
		t.Fatal("a case-only change must not sign anyone out")
	}
	v = signedIn(t, s, token)
	if _, err := s.ChangeEmail(ctx, v, "user-pass-1", "admin@example.com", withMail, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("taken address: %v", err)
	}
	if _, err := s.ChangeEmail(ctx, v, "wrong-pass-1", "new@example.com", withMail, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("wrong password: %v", err)
	}
	u, err = s.ChangeEmail(ctx, v, "user-pass-1", "new@example.com", withMail, meta)
	if err != nil || u.EmailNormalized != "new@example.com" || u.EmailVerifiedAt != nil {
		t.Fatalf("ChangeEmail = %+v, %v", u, err)
	}
	if x, _ := s.Authenticate(ctx, other); x != nil {
		t.Fatal("other devices must be signed out after an email change (S08)")
	}
	if x, _ := s.Authenticate(ctx, token); x == nil {
		t.Fatal("the session that changed the email stays signed in")
	}
	var toOld, toNew bool
	for _, j := range s.db.Job.Query().Where(job.Kind(MailJobKind)).AllX(ctx) {
		toOld = toOld || strings.Contains(j.Payload, `"to":"U@Example.com"`) && strings.Contains(j.Payload, "new@example.com")
		toNew = toNew || strings.Contains(j.Payload, `"to":"new@example.com"`) && strings.Contains(j.Payload, "/auth/verify/")
	}
	if !toOld || !toNew {
		t.Fatal("want a notice to the old address and a verification link to the new one")
	}
	if err := s.ResetPassword(ctx, oldReset, "taken-over-1", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a reset link mailed to the old address must stop working: %v", err)
	}

	// "Already registered" answers whether an address has an account: the number of
	// questions is limited.
	var rl *RateLimitError
	for range 12 {
		if _, err := s.ChangeEmail(ctx, v, "user-pass-1", "admin@example.com", withMail, meta); errors.As(err, &rl) {
			break
		}
	}
	if rl == nil {
		t.Fatal("email changes must be limited per user")
	}
}

func TestSessionsList(t *testing.T) { forEachDB(t, testSessionsList) }

func testSessionsList(t *testing.T, s *Service) {
	ctx := context.Background()
	admin, _ := setupAdmin(t, s)
	v, _ := newUser(t, s, "u@example.com")
	otherDevice(t, s, "u@example.com", "user-pass-1")
	list, err := s.ListSessions(ctx, v)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListSessions = %d, %v", len(list), err)
	}
	// The current device comes first even though the other one signed in later.
	current, other := list[0], list[1]
	if !current.Current || current.ID != v.SessionID || other.Current || other.IP != "198.51.100.200" {
		t.Fatalf("sessions %+v", list)
	}
	adminSession := s.db.Session.Query().Where(session.UserID(admin.ID)).FirstX(ctx)
	if err := s.RevokeSession(ctx, v, adminSession.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking someone else's session: %v", err)
	}
	if err := s.RevokeSession(ctx, v, other.ID); err != nil {
		t.Fatal(err)
	}
	otherDevice(t, s, "u@example.com", "user-pass-1")
	if err := s.RevokeOtherSessions(ctx, v); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListSessions(ctx, v); len(list) != 1 || !list[0].Current {
		t.Fatalf("after signing out the others: %+v", list)
	}
}

func TestDeleteAccount(t *testing.T) { forEachDB(t, testDeleteAccount) }

func testDeleteAccount(t *testing.T, s *Service) {
	ctx := context.Background()
	_, adminSess := setupAdmin(t, s)
	if err := s.DeleteAccount(ctx, signedIn(t, s, adminSess.Token), "admin-pass-1", meta); !errors.Is(err, ErrAdminCannotDelete) {
		t.Fatalf("admin deleting themselves: %v", err)
	}
	v, token := newUser(t, s, "u@example.com")
	if err := s.DeleteAccount(ctx, v, "wrong-pass-1", meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("wrong password: %v", err)
	}
	if err := s.DeleteAccount(ctx, v, "user-pass-1", meta); err != nil {
		t.Fatal(err)
	}
	if x, _ := s.Authenticate(ctx, token); x != nil {
		t.Fatal("a deleted account is signed out at once")
	}
	// Signing in within the grace period restores it.
	u, _, err := s.Login(ctx, LoginInput{Email: "u@example.com", Password: "user-pass-1"}, noCaptcha, meta)
	if err != nil {
		t.Fatalf("login within the grace period: %v", err)
	}
	if got := s.db.User.GetX(ctx, u.ID); got.Status != user.StatusActive || got.DeletedAt != nil {
		t.Fatalf("restored account %+v", got)
	}

	// An administrator can restore it too.
	v = signedIn(t, s, otherDevice(t, s, "u@example.com", "user-pass-1"))
	if err := s.DeleteAccount(ctx, v, "user-pass-1", meta); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, u.ID, AdminUpdate{Status: ptr("active")}, meta); err != nil {
		t.Fatal(err)
	}
	if got := s.db.User.GetX(ctx, u.ID); got.Status != user.StatusActive || got.DeletedAt != nil {
		t.Fatalf("account restored by an administrator %+v", got)
	}

	v = signedIn(t, s, otherDevice(t, s, "u@example.com", "user-pass-1"))
	if err := s.DeleteAccount(ctx, v, "user-pass-1", meta); err != nil {
		t.Fatal(err)
	}
	real := s.now
	s.now = func() time.Time { return real().Add(DeleteGrace + time.Hour) }
	if _, _, err := s.Login(ctx, LoginInput{Email: "u@example.com", Password: "user-pass-1"}, noCaptcha, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("after the grace period: %v", err)
	}
	if n, err := s.PurgeDeleted(ctx); n != 1 || err != nil {
		t.Fatalf("PurgeDeleted = %d, %v", n, err)
	}
	s.now = real
	newUser(t, s, "u@example.com") // the address is free again
}
