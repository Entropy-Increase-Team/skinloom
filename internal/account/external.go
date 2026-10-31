package account

import (
	"cmp"
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

// ErrAccountGone is a sign-in to an account deleted for good (its grace period is
// over), which only waits to be purged.
var ErrAccountGone = errors.New("the account has been deleted")

// SignIn creates a session for u, who proved who they are some other way than a
// password (method names it, e.g. a sign-in provider, for the audit log). Like a
// password login, it restores an account deleted within the grace period and refuses
// banned ones.
func (s *Service) SignIn(ctx context.Context, u *ent.User, remember bool, method string, meta RequestMeta) (*NewSession, error) {
	meta = meta.clipped()
	restore := false
	switch u.Status {
	case user.StatusBanned:
		return nil, ErrAccountDisabled
	case user.StatusDeleted:
		if u.DeletedAt == nil || !s.now().Before(u.DeletedAt.Add(DeleteGrace)) {
			return nil, ErrAccountGone
		}
		restore = true
	}
	var sess *NewSession
	err := s.withTx(ctx, func(tx *ent.Client) error {
		upd := tx.User.UpdateOneID(u.ID).SetLastLoginAt(s.now())
		if restore {
			upd.SetStatus(user.StatusActive).ClearDeletedAt()
		}
		if err := upd.Exec(ctx); err != nil {
			return err
		}
		var err error
		if sess, err = s.createSession(ctx, tx, u.ID, remember, meta); err != nil {
			return err
		}
		id := strconv.FormatInt(u.ID, 10)
		if restore {
			if err := audit(ctx, tx, &u.ID, "account.restored", "user", id, meta.IP, nil); err != nil {
				return err
			}
		}
		return audit(ctx, tx, &u.ID, "auth.login", "user", id, meta.IP, map[string]any{"method": method})
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// ExternalInput is a new account vouched for by a sign-in provider. It has no
// password until the user sets one. EmailVerified says the provider confirmed the
// address, which then needs no verification mail.
type ExternalInput struct {
	Email         string
	Nickname      string
	EmailVerified bool
	Remember      bool
	// Method names the provider, for the audit log.
	Method string
	// Created runs in the transaction that creates the account (to link it).
	Created func(ctx context.Context, tx *ent.Client, u *ent.User) error
}

// RegisterExternal creates an ordinary account without a password and signs it in,
// under the same rules as Register (open registration, accounts per address). A
// provider stands in for the captcha.
func (s *Service) RegisterExternal(ctx context.Context, in ExternalInput, p RegisterPolicy, mc MailContext, meta RequestMeta) (*ent.User, *NewSession, error) {
	meta = meta.clipped()
	if !p.Open {
		return nil, nil, ErrRegistrationClosed
	}
	if needs, err := s.NeedsSetup(ctx); err != nil || needs {
		return nil, nil, cmp.Or(err, ErrSetupPending)
	}
	if err := errors.Join(ValidateEmail(in.Email), ValidateNickname(in.Nickname)); err != nil {
		return nil, nil, err
	}
	if wait, err := s.registrationWait(ctx, meta.IP, p.PerIPPerDay); err != nil || wait > 0 {
		return nil, nil, cmp.Or(err, error(&RateLimitError{RetryAfter: wait}))
	}
	var (
		u    *ent.User
		sess *NewSession
	)
	now := s.now()
	err := s.withTx(ctx, func(tx *ent.Client) error {
		c := tx.User.Create().
			SetEmail(strings.TrimSpace(in.Email)).
			SetEmailNormalized(NormalizeEmail(in.Email)).
			SetNickname(strings.TrimSpace(in.Nickname)).
			SetPasswordHash("").
			SetRegisterIP(meta.IP).
			SetLastLoginAt(now)
		if in.EmailVerified {
			c.SetEmailVerifiedAt(now)
		}
		var err error
		if u, err = c.Save(ctx); err != nil {
			if db.IsConstraint(err) {
				return &FieldError{Field: "email", Message: "already registered"}
			}
			return err
		}
		if p.InitialScore > 0 {
			if u.Score, err = score.Apply(ctx, tx, score.Change{UserID: u.ID, Delta: p.InitialScore, Reason: score.ReasonRegister}); err != nil {
				return err
			}
		}
		if in.Created != nil {
			if err := in.Created(ctx, tx, u); err != nil {
				return err
			}
		}
		if sess, err = s.createSession(ctx, tx, u.ID, in.Remember, meta); err != nil {
			return err
		}
		if mc.Enabled && !in.EmailVerified {
			if err := s.sendVerification(ctx, tx, u, mc); err != nil {
				return err
			}
		}
		if err := hook.OnUserRegistered.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: u.ID, Method: in.Method}); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "auth.register", "user", strconv.FormatInt(u.ID, 10), meta.IP, map[string]any{"method": in.Method})
	})
	if err != nil {
		return nil, nil, err
	}
	s.registrations.Fail(ipKey(meta.IP)) // counts a registration, not a failure
	if mc.Enabled && !in.EmailVerified {
		s.kick()
	}
	return u, sess, nil
}

// HasPassword reports whether u can sign in with a password. Accounts created through
// a sign-in provider have none until the user sets one.
func HasPassword(u *ent.User) bool { return u.PasswordHash != "" }
