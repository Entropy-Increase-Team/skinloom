package account

import (
	"cmp"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

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
		if wait, blocked := s.registerAttempts.Take(ip); blocked {
			return nil, nil, &RateLimitError{RetryAfter: wait}
		}
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
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		var err error
		u, err = tx.User.Create().
			SetEmail(strings.TrimSpace(in.Email)).
			SetEmailNormalized(NormalizeEmail(in.Email)).
			SetNickname(strings.TrimSpace(in.Nickname)).
			SetPasswordHash(hash).
			SetPasswordAlgo(AlgoArgon2id).
			SetRegisterIP(meta.IP).
			SetLastLoginAt(s.now()).
			Save(ctx)
		if err != nil {
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
		if sess, err = s.createSession(ctx, tx, u.ID, false, meta); err != nil {
			return err
		}
		if mc.Enabled {
			if err := s.sendVerification(ctx, tx, u, mc); err != nil {
				return err
			}
		}
		if err := hook.OnUserRegistered.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: u.ID, Method: "password"}); err != nil {
			return err
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

// ValidateNewAccount checks the fields of a new account, one FieldError per bad field.
func ValidateNewAccount(email, nickname, password string) error {
	return errors.Join(ValidateEmail(email), ValidateNickname(nickname), ValidatePassword(password))
}
