package account

import (
	"cmp"
	"context"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// setupKey is the system_state row written by the transaction that completes setup.
// Its primary key makes a second, concurrent setup fail instead of creating a second
// superadmin, and it stays even if that superadmin is later removed, so the setup
// page can never be reopened to take over the site (S11).
const setupKey = "setup.completed"

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
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		// Checked again inside the transaction: the token may have been spent meanwhile.
		found, err := tx.AuthToken.Query().Where(live...).Exist(ctx)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidToken
		}
		if err := tx.SystemState.Create().SetID(setupKey).SetIntValue(1).Exec(ctx); err != nil {
			if db.IsConstraint(err) {
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
			if db.IsConstraint(err) {
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
		if err := hook.OnUserRegistered.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: u.ID, Method: "setup"}); err != nil {
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
