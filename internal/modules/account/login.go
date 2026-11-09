package account

import (
	"context"
	"strconv"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// accountCaptchaAt is the number of failures on one account, from all clients
// together, after which every client must solve a captcha (a distributed attack).
const accountCaptchaAt = 20

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

// attempt is one password check, counted against the failure limits (see NewService)
// before it runs. A failure leaves it counted; succeed and cancel take it back.
type attempt struct {
	s        *Service
	ip, acct string
}

func (a *attempt) clientAcct() string { return a.ip + "|" + a.acct }

// takeAttempt counts a password check from ip on acct, or refuses it with a
// RateLimitError once a limit is reached. Counting first is what keeps a burst of
// simultaneous requests within the limits (see Limiter.Take). lockAccount says
// whether the account-wide limit refuses or, when a captcha answers attacks spread
// over many clients instead, only counts.
func (s *Service) takeAttempt(ip, acct string, lockAccount bool) (*attempt, error) {
	a := &attempt{s: s, ip: ip, acct: acct}
	var undo []func()
	for _, c := range []struct {
		l      *Limiter
		key    string
		refuse bool
	}{{s.ipLimit, ip, true}, {s.clientAccountLimit, a.clientAcct(), true}, {s.accountLimit, acct, lockAccount}} {
		if !c.refuse {
			c.l.Fail(c.key)
		} else if wait, blocked := c.l.Take(c.key); blocked {
			for _, f := range undo {
				f()
			}
			return nil, &RateLimitError{RetryAfter: wait}
		}
		undo = append(undo, func() { c.l.Release(c.key) })
	}
	return a, nil
}

// succeed: the password was right. Successes never count against the address or the
// account; of the failures, only this client's on this account are forgiven, so the
// account-wide count of a distributed attack keeps running.
func (a *attempt) succeed() {
	a.s.ipLimit.Release(a.ip)
	a.s.accountLimit.Release(a.acct)
	a.s.clientAccountLimit.Reset(a.clientAcct())
}

// cancel: the check did not happen (a database error) or does not count (a banned
// account, answered only after the right password).
func (a *attempt) cancel() {
	a.s.ipLimit.Release(a.ip)
	a.s.clientAccountLimit.Release(a.clientAcct())
	a.s.accountLimit.Release(a.acct)
}

// Login checks an email and password. Every failure looks the same to the client,
// and repeated failures are rate limited (see NewService).
func (s *Service) Login(ctx context.Context, in LoginInput, p LoginPolicy, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	acct, ip := NormalizeEmail(in.Email), ipKey(meta.IP)
	// Decided before this attempt is counted, so it asks after CaptchaAfter failures.
	needCaptcha := !in.CaptchaSolved && s.CaptchaRequired(in.Email, meta.IP, p)
	att, err := s.takeAttempt(ip, acct, !p.CaptchaEnabled)
	if err != nil {
		return nil, nil, err
	}
	if needCaptcha {
		att.cancel()
		return nil, nil, ErrCaptchaRequired
	}
	password := in.Password
	if utf8Len := len([]rune(password)); utf8Len == 0 || utf8Len > MaxPasswordLen {
		return nil, nil, ErrInvalidCredentials // the attempt stays counted
	}

	u, err := s.db.User.Query().Where(user.EmailNormalized(acct)).Only(ctx)
	if ent.IsNotFound(err) {
		s.hasher.burn(ctx, password)
		return nil, nil, ErrInvalidCredentials
	}
	if err != nil {
		att.cancel()
		return nil, nil, err
	}
	if !HasPassword(u) { // signs in through a provider only
		s.hasher.burn(ctx, password)
		return nil, nil, ErrInvalidCredentials
	}
	ok, rehash, err := s.hasher.Verify(ctx, u.PasswordAlgo, u.PasswordHash, password)
	if err != nil {
		if ctx.Err() != nil {
			att.cancel()
			return nil, nil, ctx.Err()
		}
		s.log.Warn("stored password hash cannot be verified", "user", u.ID, "algo", u.PasswordAlgo, "err", err)
	}
	if !ok {
		return nil, nil, ErrInvalidCredentials
	}
	restore := false
	if u.Status == user.StatusDeleted {
		if u.DeletedAt == nil || !s.now().Before(u.DeletedAt.Add(DeleteGrace)) {
			return nil, nil, ErrInvalidCredentials // gone for good, just not purged yet
		}
		restore = true // signing in within the grace period cancels the deletion
	}
	if u.Status == user.StatusBanned {
		att.cancel()
		return nil, nil, ErrAccountDisabled
	}
	att.succeed()

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
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
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
			if err := s.announcePlayers(ctx, tx, u.ID, hook.OnPlayerUpdated); err != nil {
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

// VerifyCredentials checks an email and password for a client that cannot show a
// captcha (a game launcher's Yggdrasil login): the same rate limits as Login, with the
// per-account lockout always on, and no session. Banned accounts give
// ErrAccountDisabled; deleted ones (even within the grace period) fail like a wrong
// password, since only the website can restore an account.
func (s *Service) VerifyCredentials(ctx context.Context, email, password string, meta RequestMeta) (*ent.User, error) {
	meta = meta.clipped()
	acct, ip := NormalizeEmail(email), ipKey(meta.IP)
	att, err := s.takeAttempt(ip, acct, true)
	if err != nil {
		return nil, err
	}
	if n := len([]rune(password)); n == 0 || n > MaxPasswordLen {
		return nil, ErrInvalidCredentials
	}
	u, err := s.db.User.Query().Where(user.EmailNormalized(acct)).Only(ctx)
	if ent.IsNotFound(err) {
		s.hasher.burn(ctx, password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		att.cancel()
		return nil, err
	}
	if !HasPassword(u) {
		s.hasher.burn(ctx, password)
		return nil, ErrInvalidCredentials
	}
	ok, rehash, err := s.hasher.Verify(ctx, u.PasswordAlgo, u.PasswordHash, password)
	if err != nil {
		if ctx.Err() != nil {
			att.cancel()
			return nil, ctx.Err()
		}
		s.log.Warn("stored password hash cannot be verified", "user", u.ID, "algo", u.PasswordAlgo, "err", err)
	}
	if !ok || u.Status == user.StatusDeleted {
		return nil, ErrInvalidCredentials
	}
	if u.Status == user.StatusBanned {
		att.cancel()
		return nil, ErrAccountDisabled
	}
	att.succeed()
	if rehash {
		if h, err := s.hasher.Hash(ctx, password); err == nil {
			_ = s.db.User.UpdateOneID(u.ID).SetPasswordHash(h).SetPasswordAlgo(AlgoArgon2id).Exec(ctx)
		}
	}
	return u, nil
}
