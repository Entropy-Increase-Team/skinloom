// Package account owns users, browser sessions, one-time tokens, the first-run setup
// and the audit log. HTTP handlers live in internal/httpapi; this package has no
// knowledge of cookies or requests beyond RequestMeta.
package account

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
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
	setupDone          atomic.Bool
}

// NewService creates the service. Failed logins are limited per 15 minutes:
//   - 50 per client address (IPv6 per /64), whatever the account;
//   - 10 per account from one address, which stops guessing without letting a
//     single attacker lock the real user (e.g. the administrator) out;
//   - 100 per account from all addresses together, so a distributed attack still
//     hits a ceiling. Until captcha lands this one blocks; then it will ask for one.
func NewService(db *ent.Client, log *slog.Logger) *Service {
	return &Service{
		db: db, log: log, hasher: NewHasher(0), now: time.Now,
		ipLimit:            NewLimiter(50, 15*time.Minute),
		clientAccountLimit: NewLimiter(10, 15*time.Minute),
		accountLimit:       NewLimiter(100, 15*time.Minute),
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
	if err := validateNewAccount(in.Email, in.Nickname, in.Password); err != nil {
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

// Login checks an email and password. Every failure looks the same to the client,
// and repeated failures are rate limited (see NewService).
func (s *Service) Login(ctx context.Context, email, password string, remember bool, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	acct, ip := NormalizeEmail(email), ipKey(meta.IP)
	clientAcct := ip + "|" + acct
	limits := []struct {
		l   *Limiter
		key string
	}{{s.ipLimit, ip}, {s.clientAccountLimit, clientAcct}, {s.accountLimit, acct}}
	for _, c := range limits {
		if wait, blocked := c.l.Blocked(c.key); blocked {
			return nil, nil, &RateLimitError{RetryAfter: wait}
		}
	}
	fail := func() (*ent.User, *NewSession, error) {
		for _, c := range limits {
			c.l.Fail(c.key)
		}
		return nil, nil, ErrInvalidCredentials
	}
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
	if !ok || u.Status == user.StatusDeleted {
		return fail()
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
		if err := upd.Exec(ctx); err != nil {
			return err
		}
		var err error
		if sess, err = s.createSession(ctx, tx, u.ID, remember, meta); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "auth.login", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	return u, sess, nil
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

func validateNewAccount(email, nickname, password string) error {
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
