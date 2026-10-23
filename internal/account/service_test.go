package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

// forEachDB runs fn on SQLite, plus MySQL / PostgreSQL when SKINLOOM_TEST_MYSQL /
// SKINLOOM_TEST_POSTGRES name a test database (CI's integration job sets them).
func forEachDB(t *testing.T, fn func(t *testing.T, s *Service)) {
	targets := []struct{ driver, dsn string }{{"sqlite", "t.db"}}
	if dsn := os.Getenv("SKINLOOM_TEST_MYSQL"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"mysql", dsn})
	}
	if dsn := os.Getenv("SKINLOOM_TEST_POSTGRES"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"postgres", dsn})
	}
	for _, tg := range targets {
		t.Run(tg.driver, func(t *testing.T) {
			fn(t, newService(t, tg.driver, tg.dsn))
		})
	}
}

func newService(t *testing.T, driver, dsn string) *Service {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: driver, DSN: dsn, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatal(err)
	}
	if driver != "sqlite" { // shared server database: start from empty account tables
		for _, q := range []string{
			"DELETE FROM sessions", "DELETE FROM auth_tokens", "DELETE FROM users",
			"DELETE FROM audit_logs", "DELETE FROM system_state WHERE " + quoteKey(driver) + " = 'setup.completed'",
		} {
			if _, err := database.SQL.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	return NewService(database.Ent, log)
}

func quoteKey(driver string) string {
	if driver == "mysql" {
		return "`key`"
	}
	return `"key"`
}

var meta = RequestMeta{IP: "203.0.113.7", UserAgent: "test"}

func setupAdmin(t *testing.T, s *Service) (*ent.User, *NewSession) {
	t.Helper()
	ctx := context.Background()
	token, err := s.NewSetupToken(ctx)
	if err != nil || token == "" {
		t.Fatalf("NewSetupToken = %q, %v", token, err)
	}
	u, sess, err := s.CompleteSetup(ctx, SetupInput{Token: token, Email: "Admin@Example.com", Nickname: "Admin", Password: "admin-pass-1"}, meta)
	if err != nil {
		t.Fatal(err)
	}
	return u, sess
}

func TestSetup(t *testing.T) { forEachDB(t, testSetup) }

func testSetup(t *testing.T, s *Service) {
	ctx := context.Background()
	if needs, err := s.NeedsSetup(ctx); !needs || err != nil {
		t.Fatalf("NeedsSetup = %v, %v on an empty database", needs, err)
	}
	token, _ := s.NewSetupToken(ctx)
	spare, _ := s.NewSetupToken(ctx) // e.g. printed by an earlier start

	in := SetupInput{Token: NewToken(), Email: "admin@example.com", Nickname: "Admin", Password: "admin-pass-1"}
	if _, _, err := s.CompleteSetup(ctx, in, meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token: %v, want ErrInvalidToken", err)
	}
	in.Token, in.Password = token, "short"
	if _, _, err := s.CompleteSetup(ctx, in, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("short password: %v, want a field error", err)
	}

	in.Password = "admin-pass-1"
	u, sess, err := s.CompleteSetup(ctx, in, meta)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != user.RoleSuperadmin || u.EmailVerifiedAt == nil || u.PasswordAlgo != AlgoArgon2id {
		t.Fatalf("setup created %+v", u)
	}
	v, err := s.Authenticate(ctx, sess.Token)
	if err != nil || v == nil || v.User.ID != u.ID || !v.IsAdmin() {
		t.Fatalf("setup must log the admin in: %+v, %v", v, err)
	}

	// Every setup token is spent, and a fresh process (no cache) agrees setup is done.
	in.Token = spare
	if _, _, err := s.CompleteSetup(ctx, in, meta); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second setup: %v, want ErrSetupDone", err)
	}
	if n := s.db.AuthToken.Query().Where(authtoken.UsedAtIsNil()).CountX(ctx); n != 0 {
		t.Fatalf("%d setup tokens left unspent", n)
	}
	fresh := NewService(s.db, s.log)
	if needs, _ := fresh.NeedsSetup(ctx); needs {
		t.Fatal("NeedsSetup must stay false after setup")
	}
	if tok, err := fresh.NewSetupToken(ctx); tok != "" || err != nil {
		t.Fatalf("NewSetupToken after setup = %q, %v", tok, err)
	}
	if n := s.db.AuditLog.Query().Where(auditlog.Action("setup.complete")).CountX(ctx); n != 1 {
		t.Fatalf("want one setup audit record, got %d", n)
	}
}

// Two people racing with two valid tokens: exactly one superadmin.
func TestSetupRace(t *testing.T) { forEachDB(t, testSetupRace) }

func testSetupRace(t *testing.T, s *Service) {
	ctx := context.Background()
	tokens := []string{}
	for range 2 {
		tok, err := s.NewSetupToken(ctx)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, tok)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, tok := range tokens {
		wg.Go(func() {
			_, _, errs[i] = s.CompleteSetup(ctx, SetupInput{
				Token: tok, Email: string(rune('a'+i)) + "@example.com", Nickname: "Admin", Password: "admin-pass-1",
			}, meta)
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		}
	}
	if n := s.db.User.Query().Where(user.RoleEQ(user.RoleSuperadmin)).CountX(ctx); ok != 1 || n != 1 {
		t.Fatalf("%d setups succeeded, %d superadmins (errors %v)", ok, n, errs)
	}
}

func TestLogin(t *testing.T) { forEachDB(t, testLogin) }

func testLogin(t *testing.T, s *Service) {
	ctx := context.Background()
	admin, _ := setupAdmin(t, s)

	if _, _, err := s.Login(ctx, "admin@example.com", "wrong-pass-1", false, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, "nobody@example.com", "admin-pass-1", false, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown account must look exactly like a wrong password: %v", err)
	}
	// Email is case-insensitive (BS #581).
	u, sess, err := s.Login(ctx, "  ADMIN@example.COM", "admin-pass-1", true, meta)
	if err != nil || u.ID != admin.ID {
		t.Fatalf("Login = %v, %v", u, err)
	}
	if !sess.Persistent || time.Until(sess.ExpiresAt) < RememberSessionTTL-time.Minute {
		t.Fatalf("remember me must last 30 days: %+v", sess)
	}
	if v, _ := s.Authenticate(ctx, sess.Token); v == nil || v.User.ID != admin.ID {
		t.Fatal("new session must authenticate")
	}
	if err := s.Logout(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Authenticate(ctx, sess.Token); v != nil {
		t.Fatal("logged-out session must not authenticate")
	}
	for _, bad := range []string{"", "nope", NewToken()} {
		if v, err := s.Authenticate(ctx, bad); v != nil || err != nil {
			t.Fatalf("Authenticate(%q) = %v, %v", bad, v, err)
		}
	}
}

func TestSessionExpiryAndBan(t *testing.T) { forEachDB(t, testSessionExpiryAndBan) }

func testSessionExpiryAndBan(t *testing.T, s *Service) {
	ctx := context.Background()
	admin, sess := setupAdmin(t, s)
	real := s.now
	s.now = func() time.Time { return real().Add(SessionTTL + time.Second) }
	if v, _ := s.Authenticate(ctx, sess.Token); v != nil {
		t.Fatal("expired session must not authenticate")
	}
	s.now = real

	_, sess, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, meta)
	if err != nil {
		t.Fatal(err)
	}
	s.db.User.UpdateOneID(admin.ID).SetStatus(user.StatusBanned).ExecX(ctx)
	if v, _ := s.Authenticate(ctx, sess.Token); v != nil {
		t.Fatal("a banned user's sessions must stop working at once")
	}
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, meta); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("banned login: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin@example.com", "wrong-pass-1", false, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("banned + wrong password must not reveal the ban: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) { forEachDB(t, testLoginRateLimit) }

func testLoginRateLimit(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	for range 10 {
		_, _, _ = s.Login(ctx, "admin@example.com", "wrong-pass-1", false, meta)
	}
	var rl *RateLimitError
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, meta); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("11th attempt on one account: %v, want a rate limit even with the right password", err)
	}
	// Another account from another address is unaffected.
	other := RequestMeta{IP: "198.51.100.1"}
	if _, _, err := s.Login(ctx, "someone@example.com", "whatever-123", false, other); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("other account: %v", err)
	}
}

func TestLegacyHashUpgrade(t *testing.T) { forEachDB(t, testLegacyHashUpgrade) }

func testLegacyHashUpgrade(t *testing.T, s *Service) {
	ctx := context.Background()
	// as imported from a Blessing Skin site with PWD_METHOD=SALTED2MD5, SALT=pepper
	digest := hexDigest(newHash(AlgoMD5), hexDigest(newHash(AlgoMD5), "old-pass-1")+"pepper")
	u := s.db.User.Create().SetEmail("old@example.com").SetEmailNormalized("old@example.com").SetNickname("Old").
		SetPasswordHash(digest + ":pepper").SetPasswordAlgo(AlgoSalted2MD5).SaveX(ctx)

	if _, _, err := s.Login(ctx, "old@example.com", "old-pass-1", false, meta); err != nil {
		t.Fatal(err)
	}
	after := s.db.User.GetX(ctx, u.ID)
	if after.PasswordAlgo != AlgoArgon2id || after.PasswordHash == u.PasswordHash || after.LastLoginAt == nil {
		t.Fatalf("legacy hash not upgraded: %s %s", after.PasswordAlgo, after.PasswordHash)
	}
	if _, _, err := s.Login(ctx, "old@example.com", "old-pass-1", false, meta); err != nil {
		t.Fatalf("login after upgrade: %v", err)
	}
}

// Regression: ent's MaxLen counts bytes, so a 32-character CJK nickname (96 bytes)
// passed validation and then failed in the database layer with a 500.
func TestUnicodeNickname(t *testing.T) { forEachDB(t, testUnicodeNickname) }

func testUnicodeNickname(t *testing.T, s *Service) {
	ctx := context.Background()
	token, _ := s.NewSetupToken(ctx)
	nick := strings.Repeat("字", MaxNicknameLen)
	u, _, err := s.CompleteSetup(ctx, SetupInput{Token: token, Email: "a@example.com", Nickname: nick, Password: "admin-pass-1"}, meta)
	if err != nil || u.Nickname != nick {
		t.Fatalf("32-character nickname: %v", err)
	}
}

// Regression: setup hashed the password (19 MiB of argon2) before looking at the
// token, so anyone could make the server burn CPU through the public endpoint, even
// after setup. Wrong tokens and finished setups must be refused without hashing.
func TestSetupRefusesBeforeHashing(t *testing.T) { forEachDB(t, testSetupRefusesBeforeHashing) }

func testSetupRefusesBeforeHashing(t *testing.T, s *Service) {
	busy := func() context.Context {
		s.hasher = NewHasher(1)
		s.hasher.sem <- struct{}{} // any hash attempt now blocks until ctx ends
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		t.Cleanup(cancel)
		return ctx
	}
	in := SetupInput{Token: NewToken(), Email: "a@example.com", Nickname: "A", Password: "admin-pass-1"}
	if _, _, err := s.CompleteSetup(busy(), in, meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token: %v, want ErrInvalidToken without hashing", err)
	}
	s.hasher = NewHasher(1)
	setupAdmin(t, s)
	if _, _, err := s.CompleteSetup(busy(), in, meta); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("after setup: %v, want ErrSetupDone without hashing", err)
	}
}

// Regression: ten failures from a single address locked the account for everyone,
// so one attacker could keep the administrator out indefinitely. A client that fails
// is blocked for that account; others are only blocked by a distributed attack.
func TestLoginLockoutIsPerClient(t *testing.T) { forEachDB(t, testLoginLockoutIsPerClient) }

func testLoginLockoutIsPerClient(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	attacker := RequestMeta{IP: "198.51.100.66"}
	for range 10 {
		_, _, _ = s.Login(ctx, "admin@example.com", "wrong-pass-1", false, attacker)
	}
	var rl *RateLimitError
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, attacker); !errors.As(err, &rl) {
		t.Fatalf("the failing client must be blocked: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, meta); err != nil {
		t.Fatalf("the real admin from another address must still get in: %v", err)
	}
	for i := range 10 { // 100 failures spread over 10 addresses
		m := RequestMeta{IP: fmt.Sprintf("203.0.113.%d", 100+i)}
		for range 10 {
			_, _, _ = s.Login(ctx, "admin@example.com", "wrong-pass-1", false, m)
		}
	}
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, RequestMeta{IP: "192.0.2.99"}); !errors.As(err, &rl) {
		t.Fatalf("a distributed attack must still lock the account: %v", err)
	}
}

// Regression: an IPv6 address with a zone can exceed the 45-character column, which
// made login fail with a database validation error.
func TestLongClientAddress(t *testing.T) { forEachDB(t, testLongClientAddress) }

func testLongClientAddress(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	long := RequestMeta{IP: "fe80::1234:5678:9abc:def0%" + strings.Repeat("x", 40), UserAgent: strings.Repeat("ü", 600)}
	if _, _, err := s.Login(ctx, "admin@example.com", "admin-pass-1", false, long); err != nil {
		t.Fatalf("login with a long client address: %v", err)
	}
}
