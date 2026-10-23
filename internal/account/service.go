// Package account owns users, browser sessions, one-time tokens, the first-run setup
// and the audit log. HTTP handlers live in internal/httpapi; this package has no
// knowledge of cookies or requests beyond RequestMeta.
package account

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/session"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// Session lifetimes. A "remember me" session lasts 30 days; otherwise the cookie dies
// with the browser and the server forgets the session after a day at the latest.
const (
	SessionTTL         = 24 * time.Hour
	RememberSessionTTL = 30 * 24 * time.Hour
	SetupTokenTTL      = 24 * time.Hour
	// lastSeenEvery throttles the last_seen_at write to once per interval per session.
	lastSeenEvery = 5 * time.Minute
)

// setupKey is the system_state row written by the transaction that completes setup.
// Its primary key makes a second, concurrent setup fail instead of creating a second
// superadmin, and it stays even if that superadmin is later removed, so the setup
// page can never be reopened to take over the site (S11).
const setupKey = "setup.completed"

// RequestMeta describes the client for audit records and rate limits.
type RequestMeta struct {
	IP        string
	UserAgent string
}

// NewSession is a freshly created login. Token goes into the cookie and is never stored.
type NewSession struct {
	Token     string
	ExpiresAt time.Time
	// Persistent sessions ("remember me") get a cookie with an expiry; others a
	// browser-session cookie.
	Persistent bool
}

// Viewer is the authenticated user of a request.
type Viewer struct {
	User      *ent.User
	SessionID string
}

// IsAdmin reports whether the viewer may use the admin API.
func (v *Viewer) IsAdmin() bool {
	return v != nil && (v.User.Role == user.RoleAdmin || v.User.Role == user.RoleSuperadmin)
}

// Service implements accounts and sessions.
type Service struct {
	db     *ent.Client
	log    *slog.Logger
	hasher *Hasher
	now    func() time.Time

	ipLimit            *Limiter
	clientAccountLimit *Limiter
	accountLimit       *Limiter
	registrations      *Limiter // per address (IPv6 /64) per day; only counted, see Register
	registerAttempts   *Limiter // registration requests per address, successful or not
	forgotIP           *Limiter // password reset requests per address
	forgotAccount      *Limiter // password reset requests per email
	resendMinute       *Limiter // verification mails per user
	resendDay          *Limiter
	passwordChecks     *Limiter // wrong current passwords per user (profile changes)
	emailChanges       *Limiter // email change attempts per user
	setupDone          atomic.Bool
	notify             func() // see SetJobNotifier
}

// accountCaptchaAt is the number of failures on one account, from all clients
// together, after which every client must solve a captcha (a distributed attack).
const accountCaptchaAt = 20

// NewService creates the service. Failed logins are limited per 15 minutes:
//   - 50 per client address (IPv6 per /64), whatever the account;
//   - 10 per account from one address, which stops guessing without letting a
//     single attacker lock the real user (e.g. the administrator) out;
//   - per account from all addresses together: with a captcha provider, 20
//     failures make every client solve a captcha; without one, 100 failures block.
//
// With a captcha provider a client also has to solve one after
// auth.login_captcha_after failures of its own (LoginPolicy).
func NewService(db *ent.Client, log *slog.Logger) *Service {
	return &Service{
		db: db, log: log, hasher: NewHasher(0), now: time.Now,
		ipLimit:            NewLimiter(50, 15*time.Minute),
		clientAccountLimit: NewLimiter(10, 15*time.Minute),
		accountLimit:       NewLimiter(100, 15*time.Minute),
		registrations:      NewLimiter(math.MaxInt, 24*time.Hour),
		registerAttempts:   NewLimiter(30, time.Hour),
		forgotIP:           NewLimiter(10, time.Hour),
		forgotAccount:      NewLimiter(3, time.Hour),
		resendMinute:       NewLimiter(1, time.Minute),
		resendDay:          NewLimiter(5, 24*time.Hour),
		passwordChecks:     NewLimiter(5, 15*time.Minute),
		emailChanges:       NewLimiter(10, time.Hour),
	}
}

// clipped bounds client-supplied metadata to the column sizes; an IPv6 address with a
// zone, or a huge User-Agent, must not fail a login with a database error.
func (m RequestMeta) clipped() RequestMeta {
	return RequestMeta{IP: truncate(m.IP, 45), UserAgent: truncate(m.UserAgent, 512)}
}

// NeedsSetup reports whether the first administrator still has to be created.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	if s.setupDone.Load() {
		return false, nil
	}
	done, err := s.db.SystemState.Query().Where(systemstate.ID(setupKey)).Exist(ctx)
	if err != nil {
		return false, err
	}
	if done {
		s.setupDone.Store(true)
	}
	return !done, nil
}

// NewSetupToken creates a one-time token for the setup page, or returns "" when setup
// is already done. Every start creates a new one; earlier ones stay valid until they
// expire or setup completes.
func (s *Service) NewSetupToken(ctx context.Context) (string, error) {
	needs, err := s.NeedsSetup(ctx)
	if err != nil || !needs {
		return "", err
	}
	now := s.now()
	if _, err := s.db.AuthToken.Delete().
		Where(authtoken.PurposeEQ(authtoken.PurposeSetup), authtoken.ExpiresAtLT(now)).Exec(ctx); err != nil {
		return "", err
	}
	token := NewToken()
	err = s.db.AuthToken.Create().
		SetPurpose(authtoken.PurposeSetup).
		SetTokenHash(HashToken(token)).
		SetExpiresAt(now.Add(SetupTokenTTL)).
		Exec(ctx)
	return token, err
}

// SetupInput creates the first administrator.
type SetupInput struct {
	Token    string
	Email    string
	Nickname string
	Password string
}

// CompleteSetup spends a setup token, creates the superadmin and logs them in, all in
// one transaction.
func (s *Service) CompleteSetup(ctx context.Context, in SetupInput, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	// The endpoint is public: refuse callers without a live token before doing any
	// expensive work (the password hash costs 19 MiB and tens of milliseconds).
	if !wellFormedToken(in.Token) {
		return nil, nil, ErrInvalidToken
	}
	if needs, err := s.NeedsSetup(ctx); err != nil || !needs {
		return nil, nil, cmp.Or(err, ErrSetupDone)
	}
	now := s.now()
	live := []predicate.AuthToken{
		authtoken.TokenHash(HashToken(in.Token)),
		authtoken.PurposeEQ(authtoken.PurposeSetup),
		authtoken.UsedAtIsNil(),
		authtoken.ExpiresAtGT(now),
	}
	if found, err := s.db.AuthToken.Query().Where(live...).Exist(ctx); err != nil || !found {
		return nil, nil, cmp.Or(err, ErrInvalidToken)
	}
	if err := ValidateNewAccount(in.Email, in.Nickname, in.Password); err != nil {
		return nil, nil, err
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, nil, err
	}
	var (
		u    *ent.User
		sess *NewSession
	)
	err = s.withTx(ctx, func(tx *ent.Client) error {
		// Checked again inside the transaction: the token may have been spent meanwhile.
		found, err := tx.AuthToken.Query().Where(live...).Exist(ctx)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidToken
		}
		if err := tx.SystemState.Create().SetID(setupKey).SetIntValue(1).Exec(ctx); err != nil {
			if ent.IsConstraintError(err) {
				return ErrSetupDone
			}
			return err
		}
		u, err = tx.User.Create().
			SetEmail(strings.TrimSpace(in.Email)).
			SetEmailNormalized(NormalizeEmail(in.Email)).
			SetNickname(strings.TrimSpace(in.Nickname)).
			SetPasswordHash(hash).
			SetPasswordAlgo(AlgoArgon2id).
			SetRole(user.RoleSuperadmin).
			SetEmailVerifiedAt(now).
			SetRegisterIP(meta.IP).
			SetLastLoginAt(now).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return &FieldError{Field: "email", Message: "already registered"}
			}
			return err
		}
		// Spend every setup token, not just this one.
		if err := tx.AuthToken.Update().
			Where(authtoken.PurposeEQ(authtoken.PurposeSetup), authtoken.UsedAtIsNil()).
			SetUsedAt(now).Exec(ctx); err != nil {
			return err
		}
		if sess, err = s.createSession(ctx, tx, u.ID, false, meta); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "setup.complete", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	s.setupDone.Store(true)
	return u, sess, nil
}

// LoginInput is one sign-in attempt. CaptchaSolved says the caller verified a captcha
// answer with this request (see CaptchaRequired).
type LoginInput struct {
	Email         string
	Password      string
	Remember      bool
	CaptchaSolved bool
}

// CaptchaRequired reports whether a login for email from ip must come with a solved
// captcha: after p.CaptchaAfter failures of this client on this account, or after a
// distributed attack on the account. Always false without a captcha provider.
func (s *Service) CaptchaRequired(email, ip string, p LoginPolicy) bool {
	if !p.CaptchaEnabled {
		return false
	}
	acct := NormalizeEmail(email)
	return s.clientAccountLimit.Count(ipKey(ip)+"|"+acct) >= p.CaptchaAfter ||
		s.accountLimit.Count(acct) >= accountCaptchaAt
}

// Login checks an email and password. Every failure looks the same to the client,
// and repeated failures are rate limited (see NewService).
func (s *Service) Login(ctx context.Context, in LoginInput, p LoginPolicy, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	acct, ip := NormalizeEmail(in.Email), ipKey(meta.IP)
	clientAcct := ip + "|" + acct
	limits := []struct {
		l   *Limiter
		key string
	}{{s.ipLimit, ip}, {s.clientAccountLimit, clientAcct}, {s.accountLimit, acct}}
	for _, c := range limits {
		if c.l == s.accountLimit && p.CaptchaEnabled {
			continue // a captcha, not a lockout, answers attacks spread over many clients
		}
		if wait, blocked := c.l.Blocked(c.key); blocked {
			return nil, nil, &RateLimitError{RetryAfter: wait}
		}
	}
	if !in.CaptchaSolved && s.CaptchaRequired(in.Email, meta.IP, p) {
		return nil, nil, ErrCaptchaRequired
	}
	fail := func() (*ent.User, *NewSession, error) {
		for _, c := range limits {
			c.l.Fail(c.key)
		}
		return nil, nil, ErrInvalidCredentials
	}
	password := in.Password
	if utf8Len := len([]rune(password)); utf8Len == 0 || utf8Len > MaxPasswordLen {
		return fail()
	}

	u, err := s.db.User.Query().Where(user.EmailNormalized(acct)).Only(ctx)
	if ent.IsNotFound(err) {
		s.hasher.burn(ctx, password)
		return fail()
	}
	if err != nil {
		return nil, nil, err
	}
	ok, rehash, err := s.hasher.Verify(ctx, u.PasswordAlgo, u.PasswordHash, password)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		s.log.Warn("stored password hash cannot be verified", "user", u.ID, "algo", u.PasswordAlgo, "err", err)
	}
	if !ok {
		return fail()
	}
	restore := false
	if u.Status == user.StatusDeleted {
		if u.DeletedAt == nil || !s.now().Before(u.DeletedAt.Add(DeleteGrace)) {
			return fail() // gone for good, just not purged yet
		}
		restore = true // signing in within the grace period cancels the deletion
	}
	if u.Status == user.StatusBanned {
		return nil, nil, ErrAccountDisabled
	}
	// Only this client's count is forgiven; the account-wide count of a distributed
	// attack keeps running even if the real user signs in meanwhile.
	s.clientAccountLimit.Reset(clientAcct)

	var newHash string
	if rehash {
		// Upgrade imported Blessing Skin hashes (and outdated argon2 parameters), before
		// the transaction so it is not held open while hashing.
		if newHash, err = s.hasher.Hash(ctx, password); err != nil {
			return nil, nil, err
		}
	}
	now := s.now()
	var sess *NewSession
	err = s.withTx(ctx, func(tx *ent.Client) error {
		upd := tx.User.UpdateOneID(u.ID).SetLastLoginAt(now)
		if newHash != "" {
			upd.SetPasswordHash(newHash).SetPasswordAlgo(AlgoArgon2id)
		}
		if restore {
			upd.SetStatus(user.StatusActive).ClearDeletedAt()
		}
		if err := upd.Exec(ctx); err != nil {
			return err
		}
		var err error
		if sess, err = s.createSession(ctx, tx, u.ID, in.Remember, meta); err != nil {
			return err
		}
		if restore {
			if err := audit(ctx, tx, &u.ID, "account.restored", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil); err != nil {
				return err
			}
		}
		return audit(ctx, tx, &u.ID, "auth.login", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	return u, sess, nil
}

// RegisterInput is a new account from the registration form.
type RegisterInput struct {
	Email    string
	Nickname string
	Password string
}

// Register creates an ordinary account and signs it in. The caller has already
// checked the captcha, so the expensive part (hashing) only runs for people.
func (s *Service) Register(ctx context.Context, in RegisterInput, p RegisterPolicy, mc MailContext, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	if !p.Open {
		return nil, nil, ErrRegistrationClosed
	}
	if needs, err := s.NeedsSetup(ctx); err != nil || needs {
		return nil, nil, cmp.Or(err, ErrSetupPending)
	}
	if err := ValidateNewAccount(in.Email, in.Nickname, in.Password); err != nil {
		return nil, nil, err
	}
	if wait, err := s.registrationWait(ctx, meta.IP, p.PerIPPerDay); err != nil || wait > 0 {
		return nil, nil, cmp.Or(err, error(&RateLimitError{RetryAfter: wait}))
	}
	// Attempts count too, not only accounts created: otherwise a client (above all
	// with captcha.provider = none) could post forever, each request holding one of
	// the few password hashing slots that sign-ins need too. PerIPPerDay = 0 means the
	// site does not limit by address (e.g. everyone shares one), so neither does this.
	if ip := ipKey(meta.IP); p.PerIPPerDay > 0 {
		if wait, blocked := s.registerAttempts.Blocked(ip); blocked {
			return nil, nil, &RateLimitError{RetryAfter: wait}
		}
		s.registerAttempts.Fail(ip)
	}
	// The answer for a taken address is explicit anyway; finding it out costs no hash.
	if taken, err := s.db.User.Query().Where(user.EmailNormalized(NormalizeEmail(in.Email))).Exist(ctx); err != nil || taken {
		return nil, nil, cmp.Or(err, error(&FieldError{Field: "email", Message: "already registered"}))
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, nil, err
	}
	var (
		u    *ent.User
		sess *NewSession
	)
	err = s.withTx(ctx, func(tx *ent.Client) error {
		var err error
		u, err = tx.User.Create().
			SetEmail(strings.TrimSpace(in.Email)).
			SetEmailNormalized(NormalizeEmail(in.Email)).
			SetNickname(strings.TrimSpace(in.Nickname)).
			SetPasswordHash(hash).
			SetPasswordAlgo(AlgoArgon2id).
			SetScore(p.InitialScore).
			SetRegisterIP(meta.IP).
			SetLastLoginAt(s.now()).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return &FieldError{Field: "email", Message: "already registered"}
			}
			return err
		}
		if sess, err = s.createSession(ctx, tx, u.ID, false, meta); err != nil {
			return err
		}
		if mc.Enabled {
			if err := s.sendVerification(ctx, tx, u, mc); err != nil {
				return err
			}
		}
		return audit(ctx, tx, &u.ID, "auth.register", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	s.registrations.Fail(ipKey(meta.IP)) // counts a registration, not a failure
	if mc.Enabled {
		s.kick()
	}
	return u, sess, nil
}

// registrationWait returns how long ip must wait before registering again, given at
// most perDay accounts per 24 hours. The database counts exact addresses (and
// survives restarts); the in-memory counter groups IPv6 by /64, so rotating
// addresses inside one subscriber's block does not help.
func (s *Service) registrationWait(ctx context.Context, ip string, perDay int) (time.Duration, error) {
	if perDay <= 0 {
		return 0, nil
	}
	if s.registrations.Count(ipKey(ip)) >= perDay {
		wait, _ := s.registrations.Blocked(ipKey(ip))
		return max(wait, time.Minute), nil
	}
	since := s.now().Add(-24 * time.Hour)
	recent := s.db.User.Query().Where(user.RegisterIP(ip), user.CreatedAtGT(since))
	n, err := recent.Count(ctx)
	if err != nil || n < perDay {
		return 0, err
	}
	oldest, err := recent.Order(ent.Asc(user.FieldCreatedAt)).First(ctx)
	if err != nil {
		return 0, err
	}
	return max(oldest.CreatedAt.Add(24*time.Hour).Sub(s.now()), time.Minute), nil
}

// Authenticate resolves a session cookie. It returns (nil, nil) for anything that is
// not a live session of an active user.
func (s *Service) Authenticate(ctx context.Context, token string) (*Viewer, error) {
	if !wellFormedToken(token) {
		return nil, nil
	}
	id := HashToken(token)
	sess, err := s.db.Session.Query().Where(session.ID(id)).WithUser().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := sess.Edges.User
	if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) || u == nil || u.Status != user.StatusActive {
		return nil, nil
	}
	if now.Sub(sess.LastSeenAt) >= lastSeenEvery {
		if err := s.db.Session.UpdateOneID(id).SetLastSeenAt(now).Exec(ctx); err != nil {
			s.log.Debug("update session last_seen_at", "err", err)
		}
	}
	return &Viewer{User: u, SessionID: id}, nil
}

// Logout revokes the session behind token. Unknown tokens are ignored.
func (s *Service) Logout(ctx context.Context, token string) error {
	if !wellFormedToken(token) {
		return nil
	}
	_, err := s.db.Session.Update().
		Where(session.ID(HashToken(token)), session.RevokedAtIsNil()).
		SetRevokedAt(s.now()).Save(ctx)
	return err
}

// Audit records an action by actor (nil = system). Modules call this after a change
// that happened outside the account service, e.g. a settings update.
func (s *Service) Audit(ctx context.Context, actor *int64, action, targetType, targetID, ip string, meta map[string]any) error {
	return audit(ctx, s.db, actor, action, targetType, targetID, ip, meta)
}

func (s *Service) createSession(ctx context.Context, tx *ent.Client, userID int64, remember bool, meta RequestMeta) (*NewSession, error) {
	ttl := SessionTTL
	if remember {
		ttl = RememberSessionTTL
	}
	token := NewToken()
	now := s.now()
	err := tx.Session.Create().
		SetID(HashToken(token)).
		SetUserID(userID).
		SetIP(meta.IP).
		SetUserAgent(truncate(meta.UserAgent, 512)).
		SetCreatedAt(now).
		SetLastSeenAt(now).
		SetExpiresAt(now.Add(ttl)).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &NewSession{Token: token, ExpiresAt: now.Add(ttl), Persistent: remember}, nil
}

func (s *Service) withTx(ctx context.Context, fn func(tx *ent.Client) error) error {
	tx, err := s.db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}

func audit(ctx context.Context, tx *ent.Client, actor *int64, action, targetType, targetID, ip string, meta map[string]any) error {
	c := tx.AuditLog.Create().
		SetNillableActorID(actor).
		SetAction(action).
		SetTargetType(targetType).
		SetTargetID(targetID).
		SetIP(ip)
	if meta != nil {
		c.SetMeta(meta)
	}
	return c.Exec(ctx)
}

// ValidateNewAccount checks the fields of a new account, one FieldError per bad field.
func ValidateNewAccount(email, nickname, password string) error {
	return errors.Join(ValidateEmail(email), ValidateNickname(nickname), ValidatePassword(password))
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
