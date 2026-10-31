package account

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/session"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// DeleteGrace is how long a deleted account can still be restored by signing in.
const DeleteGrace = 30 * 24 * time.Hour

var (
	ErrNotFound = errors.New("not found")
	// ErrAdminCannotDelete: administrators must be demoted before deleting their account
	// (as in Blessing Skin), so a site never loses its administrators by accident.
	ErrAdminCannotDelete = errors.New("administrators cannot delete their own account")
)

// ProfileUpdate changes the fields that are set.
type ProfileUpdate struct {
	Nickname *string
	Locale   *string
}

// UpdateProfile changes a user's nickname and language preference. The language must
// be one the site offers, or empty for "follow the browser" (S04: Blessing Skin stored
// any ?lang value and then failed on every request).
func (s *Service) UpdateProfile(ctx context.Context, userID int64, up ProfileUpdate, locales []string) (*ent.User, error) {
	upd := s.db.User.UpdateOneID(userID)
	var errs []error
	if up.Nickname != nil {
		if err := ValidateNickname(*up.Nickname); err != nil {
			errs = append(errs, err)
		}
		upd.SetNickname(strings.TrimSpace(*up.Nickname))
	}
	if up.Locale != nil {
		if *up.Locale != "" && !slices.Contains(locales, *up.Locale) {
			errs = append(errs, &FieldError{Field: "locale", Message: "unsupported language"})
		}
		upd.SetLocale(*up.Locale)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return upd.Save(ctx)
}

// checkPassword verifies the signed-in user's password before a sensitive change.
// Wrong guesses are limited per user, so a stolen session cannot be used to find the
// password. An account without a password (created through a sign-in provider) has
// nothing to check: its session is all there is, and setting a first password needs
// no current one.
func (s *Service) checkPassword(ctx context.Context, u *ent.User, password string) error {
	if !HasPassword(u) {
		return nil
	}
	key := strconv.FormatInt(u.ID, 10)
	if wait, blocked := s.passwordChecks.Take(key); blocked { // counted before checking: see Limiter.Take
		return &RateLimitError{RetryAfter: wait}
	}
	ok, _, err := s.hasher.Verify(ctx, u.PasswordAlgo, u.PasswordHash, password)
	if err != nil && ctx.Err() != nil {
		s.passwordChecks.Release(key)
		return ctx.Err()
	}
	if !ok {
		return &FieldError{Field: "password", Message: "incorrect password"}
	}
	s.passwordChecks.Reset(key)
	return nil
}

// ChangePassword sets a new password and signs out every other session (S08); the
// session making the change stays.
func (s *Service) ChangePassword(ctx context.Context, v *Viewer, current, next string, meta RequestMeta) error {
	if err := s.checkPassword(ctx, v.User, current); err != nil {
		return err
	}
	if err := ValidatePassword(next); err != nil {
		return &FieldError{Field: "newPassword", Message: err.(*FieldError).Message}
	}
	hash, err := s.hasher.Hash(ctx, next)
	if err != nil {
		return err
	}
	meta = meta.clipped()
	now := s.now()
	return s.withTx(ctx, func(tx *ent.Client) error {
		if err := tx.User.UpdateOneID(v.User.ID).SetPasswordHash(hash).SetPasswordAlgo(AlgoArgon2id).Exec(ctx); err != nil {
			return err
		}
		if err := revokeOtherSessions(ctx, tx, v, now); err != nil {
			return err
		}
		if err := spendTokens(ctx, tx, v.User.ID, now, authtoken.PurposeResetPassword); err != nil {
			return err
		}
		return audit(ctx, tx, &v.User.ID, "account.password_changed", "user", strconv.FormatInt(v.User.ID, 10), meta.IP, nil)
	})
}

// ChangeEmail moves the account to a new address. Other sessions are signed out (S08),
// the old address is told about the change, and the new one must be verified again.
func (s *Service) ChangeEmail(ctx context.Context, v *Viewer, password, email string, mc MailContext, meta RequestMeta) (*ent.User, error) {
	if err := s.checkPassword(ctx, v.User, password); err != nil {
		return nil, err
	}
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	old := v.User
	if email == old.Email {
		return nil, &FieldError{Field: "email", Message: "this is already your email"}
	}
	sameAddress := NormalizeEmail(email) == old.EmailNormalized // only the case changed
	// "Already registered" tells whether an address has an account; without a budget
	// a signed-in user could ask about any number of addresses (registration has a
	// captcha and per-address limits for the same answer).
	key := strconv.FormatInt(old.ID, 10)
	if wait, blocked := s.emailChanges.Take(key); blocked {
		return nil, &RateLimitError{RetryAfter: wait}
	}
	meta = meta.clipped()
	var u *ent.User
	err := s.withTx(ctx, func(tx *ent.Client) error {
		upd := tx.User.UpdateOneID(old.ID).SetEmail(email).SetEmailNormalized(NormalizeEmail(email))
		if !sameAddress {
			upd.ClearEmailVerifiedAt()
		}
		var err error
		if u, err = upd.Save(ctx); err != nil {
			if db.IsConstraint(err) {
				return &FieldError{Field: "email", Message: "already registered"}
			}
			return err
		}
		if !sameAddress {
			now := s.now()
			if err := revokeOtherSessions(ctx, tx, v, now); err != nil {
				return err
			}
			if err := spendTokens(ctx, tx, old.ID, now, authtoken.PurposeVerifyEmail, authtoken.PurposeResetPassword); err != nil {
				return err
			}
			if mc.Enabled {
				notice := mail.Render(mail.KindEmailChanged, cmp.Or(old.Locale, mc.Locale), old.Email,
					mail.Data{Site: mc.Site, Nickname: old.Nickname, NewEmail: email})
				if err := enqueue(ctx, tx, notice); err != nil {
					return err
				}
				if err := s.sendVerification(ctx, tx, u, mc); err != nil {
					return err
				}
			}
		}
		return audit(ctx, tx, &old.ID, "account.email_changed", "user", strconv.FormatInt(old.ID, 10), meta.IP,
			map[string]any{"from": old.Email, "to": email})
	})
	if err != nil {
		return nil, err
	}
	if mc.Enabled && !sameAddress {
		s.kick()
	}
	return u, nil
}

// SessionInfo is one live login of a user, for the "devices" list.
type SessionInfo struct {
	ID         string
	IP         string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	Current    bool
}

func (s *Service) liveSessions(userID int64) *ent.SessionQuery {
	return s.db.Session.Query().Where(session.UserID(userID), session.RevokedAtIsNil(), session.ExpiresAtGT(s.now()))
}

// ListSessions returns the user's live sessions, most recently used first.
func (s *Service) ListSessions(ctx context.Context, v *Viewer) ([]SessionInfo, error) {
	list, err := s.liveSessions(v.User.ID).Order(ent.Desc(session.FieldLastSeenAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(list))
	for _, x := range list {
		out = append(out, SessionInfo{ID: x.ID, IP: x.IP, UserAgent: x.UserAgent, CreatedAt: x.CreatedAt,
			LastSeenAt: x.LastSeenAt, Current: x.ID == v.SessionID})
	}
	// The current device first, then the rest by recent activity.
	slices.SortStableFunc(out, func(a, b SessionInfo) int {
		switch {
		case a.Current == b.Current:
			return 0
		case a.Current:
			return -1
		}
		return 1
	})
	return out, nil
}

// RevokeSession signs one of the user's own sessions out.
func (s *Service) RevokeSession(ctx context.Context, v *Viewer, id string) error {
	n, err := s.db.Session.Update().
		Where(session.ID(id), session.UserID(v.User.ID), session.RevokedAtIsNil()).
		SetRevokedAt(s.now()).Save(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// RevokeOtherSessions signs out every session of the user except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, v *Viewer) error {
	return revokeOtherSessions(ctx, s.db, v, s.now())
}

// revokeOtherSessions also revokes launcher, application and personal access tokens:
// they are other devices too.
func revokeOtherSessions(ctx context.Context, c *ent.Client, v *Viewer, now time.Time) error {
	if err := revokeTokens(ctx, c, v.User.ID); err != nil {
		return err
	}
	return c.Session.Update().
		Where(session.UserID(v.User.ID), session.IDNEQ(v.SessionID), session.RevokedAtIsNil()).
		SetRevokedAt(now).Exec(ctx)
}

// DeleteAccount schedules the user's account for deletion: it stops working at once
// and is removed after DeleteGrace unless the user signs in again before then.
func (s *Service) DeleteAccount(ctx context.Context, v *Viewer, password string, meta RequestMeta) error {
	if v.IsAdmin() {
		return ErrAdminCannotDelete
	}
	if err := s.checkPassword(ctx, v.User, password); err != nil {
		return err
	}
	meta = meta.clipped()
	now := s.now()
	return s.withTx(ctx, func(tx *ent.Client) error {
		if err := tx.User.UpdateOneID(v.User.ID).SetStatus(user.StatusDeleted).SetDeletedAt(now).Exec(ctx); err != nil {
			return err
		}
		if err := revokeSessions(ctx, tx, v.User.ID, now); err != nil {
			return err
		}
		return audit(ctx, tx, &v.User.ID, "account.deleted", "user", strconv.FormatInt(v.User.ID, 10), meta.IP, nil)
	})
}

// PurgeDeleted removes accounts whose grace period is over. The before-delete hook
// runs for each first; sessions, tokens, players, closets and the score ledger go
// with the account (foreign keys); public textures stay without an uploader; audit
// records stay. Past the grace period nobody can restore the account any more.
func (s *Service) PurgeDeleted(ctx context.Context) (int, error) {
	expired := []predicate.User{user.StatusEQ(user.StatusDeleted), user.DeletedAtLT(s.now().Add(-DeleteGrace))}
	ids, err := s.db.User.Query().Where(expired...).IDs(ctx)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, id := range ids {
		// One transaction per account, the hook included: a user who signs in again
		// meanwhile (restoring the account) makes the delete match nothing, and the
		// rollback keeps their private textures, which the hook had already deleted
		// when it ran on its own.
		err := s.withTx(ctx, func(tx *ent.Client) error {
			if err := s.beforeDelete(ctx, tx, id); err != nil {
				return err
			}
			n, err := tx.User.Delete().Where(append(expired, user.ID(id))...).Exec(ctx)
			if err == nil && n == 0 {
				return errRestored
			}
			if err != nil {
				return err
			}
			return hook.OnUserDeleted.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: id})
		})
		switch {
		case errors.Is(err, errRestored):
		case err != nil:
			return purged, err
		default:
			purged++
		}
	}
	if purged > 0 {
		s.kick() // the deletions may have queued file sweeps
	}
	return purged, nil
}

var errRestored = errors.New("restored meanwhile")

// SetBeforeDelete sets a function that runs inside the transaction that removes an
// account for good (purge or an administrator's delete), before the user's row goes:
// to delete the user's private textures, which nobody else could ever see, and to
// lock what the deletion changes in the one order (see score.Lock).
func (s *Service) SetBeforeDelete(f func(ctx context.Context, tx *ent.Client, userID int64) error) {
	s.onDelete = f
}

func (s *Service) beforeDelete(ctx context.Context, tx *ent.Client, userID int64) error {
	if s.onDelete == nil {
		return nil
	}
	return s.onDelete(ctx, tx, userID)
}
