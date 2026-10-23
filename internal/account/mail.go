package account

import (
	"cmp"
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/session"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
)

// MailJobKind is the background job that sends one rendered mail.Message.
const MailJobKind = "mail.send"

// Token lifetimes (Blessing Skin's links never expired or lasted an hour and could be
// reused, S02/S03).
const (
	VerifyEmailTTL   = 24 * time.Hour
	ResetPasswordTTL = 30 * time.Minute
)

// MailContext is what the account service needs to write a mail: whether mail is
// configured, where links point, and the language and site name to use.
type MailContext struct {
	Enabled bool
	BaseURL string // e.g. https://skins.example.com, no trailing slash
	Locale  string // used when the user has no language preference
	Site    string // localized site name
}

// SetJobNotifier sets a function called after a transaction that queued mail
// commits, so the job worker sends it at once instead of at its next poll.
func (s *Service) SetJobNotifier(f func()) { s.notify = f }

func (s *Service) kick() {
	if s.notify != nil {
		s.notify()
	}
}

// issueToken creates a one-time token, spending earlier unused ones of the same
// purpose for the user: only the newest link works.
func (s *Service) issueToken(ctx context.Context, tx *ent.Client, userID int64, purpose authtoken.Purpose, ttl time.Duration) (string, error) {
	now := s.now()
	if err := spendTokens(ctx, tx, userID, now, purpose); err != nil {
		return "", err
	}
	token := NewToken()
	err := tx.AuthToken.Create().
		SetUserID(userID).
		SetPurpose(purpose).
		SetTokenHash(HashToken(token)).
		SetExpiresAt(now.Add(ttl)).
		SetCreatedAt(now).
		Exec(ctx)
	return token, err
}

// spendTokens voids a user's unused one-time links of the given purposes. Links are
// not tied to an address, so a change of address (or password) must void the ones
// still sitting in the old inbox.
func spendTokens(ctx context.Context, tx *ent.Client, userID int64, now time.Time, purposes ...authtoken.Purpose) error {
	return tx.AuthToken.Update().
		Where(authtoken.UserID(userID), authtoken.PurposeIn(purposes...), authtoken.UsedAtIsNil()).
		SetUsedAt(now).Exec(ctx)
}

// queueMail renders kind for u and enqueues it in tx.
func queueMail(ctx context.Context, tx *ent.Client, kind string, mc MailContext, u *ent.User, path string) error {
	msg := mail.Render(kind, cmp.Or(u.Locale, mc.Locale), u.Email, mail.Data{
		Site: mc.Site, Nickname: u.Nickname, Link: strings.TrimRight(mc.BaseURL, "/") + path,
	})
	return enqueue(ctx, tx, msg)
}

// enqueue queues a rendered mail in tx.
func enqueue(ctx context.Context, tx *ent.Client, msg mail.Message) error {
	return jobs.Enqueue(ctx, tx, MailJobKind, msg)
}

// sendVerification issues a verification token and queues the mail, inside tx.
func (s *Service) sendVerification(ctx context.Context, tx *ent.Client, u *ent.User, mc MailContext) error {
	token, err := s.issueToken(ctx, tx, u.ID, authtoken.PurposeVerifyEmail, VerifyEmailTTL)
	if err != nil {
		return err
	}
	return queueMail(ctx, tx, mail.KindVerifyEmail, mc, u, "/auth/verify/"+token)
}

// ResendVerification sends a new verification link to a signed-in user (at most one
// a minute and five a day).
func (s *Service) ResendVerification(ctx context.Context, userID int64, mc MailContext) error {
	if !mc.Enabled {
		return ErrMailDisabled
	}
	u, err := s.db.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	if u.EmailVerifiedAt != nil {
		return ErrAlreadyVerified
	}
	key := strconv.FormatInt(userID, 10)
	for _, l := range []*Limiter{s.resendMinute, s.resendDay} {
		if wait, blocked := l.Blocked(key); blocked {
			return &RateLimitError{RetryAfter: wait}
		}
	}
	if err := s.withTx(ctx, func(tx *ent.Client) error { return s.sendVerification(ctx, tx, u, mc) }); err != nil {
		return err
	}
	s.resendMinute.Fail(key)
	s.resendDay.Fail(key)
	s.kick()
	return nil
}

// liveToken matches an unused, unexpired token for purpose.
func (s *Service) liveToken(token string, purpose authtoken.Purpose) []predicate.AuthToken {
	return []predicate.AuthToken{
		authtoken.TokenHash(HashToken(token)), authtoken.PurposeEQ(purpose),
		authtoken.UsedAtIsNil(), authtoken.ExpiresAtGT(s.now()), authtoken.UserIDNotNil(),
	}
}

// VerifyEmail spends a verification token and marks the address verified.
func (s *Service) VerifyEmail(ctx context.Context, token string, meta RequestMeta) (*ent.User, error) {
	if !wellFormedToken(token) {
		return nil, ErrInvalidToken
	}
	meta = meta.clipped()
	var u *ent.User
	err := s.withTx(ctx, func(tx *ent.Client) error {
		t, err := tx.AuthToken.Query().Where(s.liveToken(token, authtoken.PurposeVerifyEmail)...).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		now := s.now()
		if err := spendTokens(ctx, tx, *t.UserID, now, authtoken.PurposeVerifyEmail); err != nil {
			return err
		}
		if u, err = tx.User.UpdateOneID(*t.UserID).SetEmailVerifiedAt(now).Save(ctx); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "auth.email_verified", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	return u, err
}

// RequestPasswordReset mails a reset link if email belongs to an active account. It
// answers the same whether or not the account exists (S05); only rate limits (per
// address and per email, counted for every request) and configuration errors show.
func (s *Service) RequestPasswordReset(ctx context.Context, email string, mc MailContext, meta RequestMeta) error {
	if !mc.Enabled {
		return ErrMailDisabled
	}
	meta = meta.clipped()
	acct, ip := NormalizeEmail(email), ipKey(meta.IP)
	for _, c := range []struct {
		l   *Limiter
		key string
	}{{s.forgotIP, ip}, {s.forgotAccount, acct}} {
		if wait, blocked := c.l.Blocked(c.key); blocked {
			return &RateLimitError{RetryAfter: wait}
		}
		c.l.Fail(c.key)
	}
	u, err := s.db.User.Query().Where(user.EmailNormalized(acct), user.StatusEQ(user.StatusActive)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.withTx(ctx, func(tx *ent.Client) error {
		token, err := s.issueToken(ctx, tx, u.ID, authtoken.PurposeResetPassword, ResetPasswordTTL)
		if err != nil {
			return err
		}
		if err := queueMail(ctx, tx, mail.KindResetPassword, mc, u, "/auth/reset/"+token); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "auth.password_reset_requested", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil)
	})
	if err == nil {
		s.kick()
	}
	return err
}

// ResetPassword spends a reset token, sets the new password and signs the user out
// everywhere (S02: Blessing Skin kept every session and token alive).
func (s *Service) ResetPassword(ctx context.Context, token, password string, meta RequestMeta) error {
	if !wellFormedToken(token) {
		return ErrInvalidToken
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	meta = meta.clipped()
	// Cheap check first: do not hash for a token that cannot work.
	if ok, err := s.db.AuthToken.Query().Where(s.liveToken(token, authtoken.PurposeResetPassword)...).Exist(ctx); err != nil || !ok {
		return cmp.Or(err, ErrInvalidToken)
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *ent.Client) error {
		t, err := tx.AuthToken.Query().Where(s.liveToken(token, authtoken.PurposeResetPassword)...).Only(ctx)
		if ent.IsNotFound(err) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		userID, now := *t.UserID, s.now()
		if err := spendTokens(ctx, tx, userID, now, authtoken.PurposeResetPassword); err != nil {
			return err
		}
		u, err := tx.User.Get(ctx, userID)
		if err != nil {
			return err
		}
		upd := tx.User.UpdateOneID(userID).SetPasswordHash(hash).SetPasswordAlgo(AlgoArgon2id)
		if u.EmailVerifiedAt == nil {
			upd.SetEmailVerifiedAt(now) // the link reached this inbox: the address is real
		}
		if err := upd.Exec(ctx); err != nil {
			return err
		}
		if err := revokeSessions(ctx, tx, userID, now); err != nil {
			return err
		}
		return audit(ctx, tx, &userID, "auth.password_reset", "user", strconv.FormatInt(userID, 10), meta.IP, nil)
	})
}

// revokeSessions ends every live session of a user.
func revokeSessions(ctx context.Context, tx *ent.Client, userID int64, now time.Time) error {
	return tx.Session.Update().
		Where(session.UserID(userID), session.RevokedAtIsNil()).
		SetRevokedAt(now).Exec(ctx)
}
