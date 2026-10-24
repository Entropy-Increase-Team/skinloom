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
			"DELETE FROM closet_items", "DELETE FROM textures", "DELETE FROM blobs", "DELETE FROM score_ledger",
			"DELETE FROM jobs", "DELETE FROM sessions", "DELETE FROM auth_tokens", "DELETE FROM users",
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

	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1", Remember: false}, noCaptcha, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "nobody@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown account must look exactly like a wrong password: %v", err)
	}
	// Email is case-insensitive (BS #581).
	u, sess, err := s.Login(ctx, LoginInput{Email: "  ADMIN@example.COM", Password: "admin-pass-1", Remember: true}, noCaptcha, meta)
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

	_, sess, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, meta)
	if err != nil {
		t.Fatal(err)
	}
	s.db.User.UpdateOneID(admin.ID).SetStatus(user.StatusBanned).ExecX(ctx)
	if v, _ := s.Authenticate(ctx, sess.Token); v != nil {
		t.Fatal("a banned user's sessions must stop working at once")
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, meta); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("banned login: %v", err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1", Remember: false}, noCaptcha, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("banned + wrong password must not reveal the ban: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) { forEachDB(t, testLoginRateLimit) }

func testLoginRateLimit(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	for range 10 {
		_, _, _ = s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1", Remember: false}, noCaptcha, meta)
	}
	var rl *RateLimitError
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, meta); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("11th attempt on one account: %v, want a rate limit even with the right password", err)
	}
	// Another account from another address is unaffected.
	other := RequestMeta{IP: "198.51.100.1"}
	if _, _, err := s.Login(ctx, LoginInput{Email: "someone@example.com", Password: "whatever-123", Remember: false}, noCaptcha, other); !errors.Is(err, ErrInvalidCredentials) {
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

	if _, _, err := s.Login(ctx, LoginInput{Email: "old@example.com", Password: "old-pass-1", Remember: false}, noCaptcha, meta); err != nil {
		t.Fatal(err)
	}
	after := s.db.User.GetX(ctx, u.ID)
	if after.PasswordAlgo != AlgoArgon2id || after.PasswordHash == u.PasswordHash || after.LastLoginAt == nil {
		t.Fatalf("legacy hash not upgraded: %s %s", after.PasswordAlgo, after.PasswordHash)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "old@example.com", Password: "old-pass-1", Remember: false}, noCaptcha, meta); err != nil {
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
		_, _, _ = s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1", Remember: false}, noCaptcha, attacker)
	}
	var rl *RateLimitError
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, attacker); !errors.As(err, &rl) {
		t.Fatalf("the failing client must be blocked: %v", err)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, meta); err != nil {
		t.Fatalf("the real admin from another address must still get in: %v", err)
	}
	for i := range 10 { // 100 failures spread over 10 addresses
		m := RequestMeta{IP: fmt.Sprintf("203.0.113.%d", 100+i)}
		for range 10 {
			_, _, _ = s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "wrong-pass-1", Remember: false}, noCaptcha, m)
		}
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, RequestMeta{IP: "192.0.2.99"}); !errors.As(err, &rl) {
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
	if _, _, err := s.Login(ctx, LoginInput{Email: "admin@example.com", Password: "admin-pass-1", Remember: false}, noCaptcha, long); err != nil {
		t.Fatalf("login with a long client address: %v", err)
	}
}

// noCaptcha is the login policy of a site without a captcha provider.
var noCaptcha = LoginPolicy{}

var openRegistration = RegisterPolicy{Open: true, PerIPPerDay: 3, InitialScore: 1000}

func TestRegister(t *testing.T) { forEachDB(t, testRegister) }

func testRegister(t *testing.T, s *Service) {
	ctx := context.Background()
	in := RegisterInput{Email: "Steve@Example.com", Nickname: "Steve", Password: "steve-pass-1"}
	if _, _, err := s.Register(ctx, in, openRegistration, noMail, meta); !errors.Is(err, ErrSetupPending) {
		t.Fatalf("before setup: %v, want ErrSetupPending (the first account comes from the setup page)", err)
	}
	setupAdmin(t, s)
	if _, _, err := s.Register(ctx, in, RegisterPolicy{}, noMail, meta); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("closed: %v", err)
	}
	bad := RegisterInput{Email: "nope", Nickname: "", Password: "short"}
	if _, _, err := s.Register(ctx, bad, openRegistration, noMail, meta); len(FieldErrors(err)) != 3 {
		t.Fatalf("invalid input: %v, want three field errors", err)
	}

	u, sess, err := s.Register(ctx, in, openRegistration, noMail, meta)
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != user.RoleUser || u.Score != 1000 || u.EmailNormalized != "steve@example.com" || u.RegisterIP != meta.IP || u.EmailVerifiedAt != nil {
		t.Fatalf("registered %+v", u)
	}
	if e := s.db.ScoreEntry.Query().OnlyX(ctx); e.UserID != u.ID || e.Delta != 1000 || e.Balance != 1000 || e.Reason != "register" {
		t.Fatalf("the initial score must be in the ledger: %+v", e)
	}
	if v, _ := s.Authenticate(ctx, sess.Token); v == nil || v.User.ID != u.ID || v.IsAdmin() {
		t.Fatal("registration must sign the new user in as an ordinary user")
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "steve@example.com", Password: "steve-pass-1"}, noCaptcha, meta); err != nil {
		t.Fatalf("login after registration: %v", err)
	}
	dup := RegisterInput{Email: "STEVE@example.com", Nickname: "Other", Password: "other-pass-1"}
	errs := FieldErrors(func() error {
		_, _, err := s.Register(ctx, dup, openRegistration, noMail, RequestMeta{IP: "198.51.100.9"})
		return err
	}())
	if len(errs) != 1 || errs[0].Field != "email" {
		t.Fatalf("same email in another case: %v", errs)
	}
	if n := s.db.AuditLog.Query().Where(auditlog.Action("auth.register")).CountX(ctx); n != 1 {
		t.Fatalf("want one registration audit record, got %d", n)
	}
}

func TestRegistrationsPerAddress(t *testing.T) { forEachDB(t, testRegistrationsPerAddress) }

func testRegistrationsPerAddress(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	p := RegisterPolicy{Open: true, PerIPPerDay: 2}
	reg := func(svc *Service, i int, ip string) error {
		_, _, err := svc.Register(ctx, RegisterInput{Email: fmt.Sprintf("u%d@example.com", i), Nickname: "U", Password: "user-pass-1"}, p, noMail, RequestMeta{IP: ip})
		return err
	}
	var rl *RateLimitError
	for i := range 2 {
		if err := reg(s, i, "198.51.100.20"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg(s, 2, "198.51.100.20"); !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("third account from one address: %v", err)
	}
	if err := reg(s, 3, "198.51.100.21"); err != nil {
		t.Fatalf("another address: %v", err)
	}
	// The database remembers exact addresses across restarts (a fresh service has no
	// in-memory counts).
	if err := reg(NewService(s.db, s.log), 4, "198.51.100.20"); !errors.As(err, &rl) {
		t.Fatalf("after a restart: %v", err)
	}
	// Rotating IPv6 addresses inside one /64 does not reset the count.
	for i, ip := range []string{"2001:db8:5::1", "2001:db8:5::2"} {
		if err := reg(s, 10+i, ip); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg(s, 12, "2001:db8:5::3"); !errors.As(err, &rl) {
		t.Fatalf("third address in the same /64: %v", err)
	}
	if err := reg(s, 13, "198.51.100.30"); err != nil {
		t.Fatal(err)
	}
	unlimited := RegisterPolicy{Open: true}
	if _, _, err := s.Register(ctx, RegisterInput{Email: "x@example.com", Nickname: "X", Password: "user-pass-1"}, unlimited, noMail, RequestMeta{IP: "198.51.100.20"}); err != nil {
		t.Fatalf("0 = unlimited: %v", err)
	}
}

// Regression: registration hashed the password before learning the address was taken,
// and only counted accounts created, so a client could keep posting taken addresses
// and hold the hashing slots that sign-ins need.
func TestRegisterAttempts(t *testing.T) { forEachDB(t, testRegisterAttempts) }

func testRegisterAttempts(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	taken := RegisterInput{Email: "ADMIN@example.com", Nickname: "X", Password: "user-pass-1"}
	s.hasher = NewHasher(1)
	s.hasher.sem <- struct{}{} // any hash attempt now blocks until ctx ends
	busy, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, _, err := s.Register(busy, taken, openRegistration, noMail, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("taken address: %v, want a field error without hashing", err)
	}
	var rl *RateLimitError
	attacker := RequestMeta{IP: "198.51.100.77"}
	for range 40 {
		_, _, err := s.Register(ctx, taken, openRegistration, noMail, attacker)
		if errors.As(err, &rl) {
			break
		}
	}
	if rl == nil {
		t.Fatal("endless failed registrations from one address must be limited")
	}
	// A site that does not limit by address (everyone behind one NAT) is not limited.
	unlimited := RegisterPolicy{Open: true}
	if _, _, err := s.Register(ctx, taken, unlimited, noMail, attacker); errors.As(err, new(*RateLimitError)) {
		t.Fatalf("auth.registrations_per_ip = 0: %v", err)
	}
}

func TestLoginCaptcha(t *testing.T) { forEachDB(t, testLoginCaptcha) }

func testLoginCaptcha(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	p := LoginPolicy{CaptchaEnabled: true, CaptchaAfter: 3}
	wrong := LoginInput{Email: "admin@example.com", Password: "wrong-pass-1"}
	right := LoginInput{Email: "admin@example.com", Password: "admin-pass-1"}

	if s.CaptchaRequired(right.Email, meta.IP, p) {
		t.Fatal("no captcha before any failure")
	}
	for range 3 {
		_, _, _ = s.Login(ctx, wrong, p, meta)
	}
	if !s.CaptchaRequired(right.Email, meta.IP, p) {
		t.Fatal("captcha required after 3 failures")
	}
	if _, _, err := s.Login(ctx, right, p, meta); !errors.Is(err, ErrCaptchaRequired) {
		t.Fatalf("right password without captcha: %v, want ErrCaptchaRequired", err)
	}
	other := RequestMeta{IP: "198.51.100.77"}
	if s.CaptchaRequired(right.Email, other.IP, p) {
		t.Fatal("another client does not need a captcha yet")
	}
	solved := right
	solved.CaptchaSolved = true
	if _, _, err := s.Login(ctx, solved, p, meta); err != nil {
		t.Fatalf("with a solved captcha: %v", err)
	}
	if s.CaptchaRequired(right.Email, meta.IP, p) {
		t.Fatal("a successful login forgives this client")
	}

	// A distributed attack: 120 failures from 15 addresses. Every client now needs a
	// captcha, but the account is never locked for someone who solves one.
	wrongSolved := wrong
	wrongSolved.CaptchaSolved = true
	for i := range 15 {
		m := RequestMeta{IP: fmt.Sprintf("203.0.113.%d", 100+i)}
		for range 8 {
			_, _, _ = s.Login(ctx, wrongSolved, p, m)
		}
	}
	fresh := RequestMeta{IP: "192.0.2.200"}
	if !s.CaptchaRequired(right.Email, fresh.IP, p) {
		t.Fatal("after a distributed attack every client needs a captcha")
	}
	if _, _, err := s.Login(ctx, solved, p, fresh); err != nil {
		t.Fatalf("real user with a captcha after 120 failures: %v", err)
	}
}

// noMail is a site without a mail driver.
var noMail = MailContext{}
