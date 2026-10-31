package account

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/authtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

// superadminLock is the system_state row that transactions removing a super
// administrator write first, so they run one at a time (see AdminUpdateUser).
const superadminLock = "lock.superadmins"

var (
	// ErrForbidden: the acting administrator may not make this change.
	ErrForbidden = errors.New("not allowed")
	// ErrLastSuperadmin protects the site from losing its last super administrator.
	ErrLastSuperadmin = errors.New("this is the last super administrator")
)

// Actor is who performs an administrative change: an administrator's session, or
// nil for the admin API token (treated as a super administrator).
type Actor = *Viewer

func actorRole(a Actor) user.Role {
	if a == nil {
		return user.RoleSuperadmin
	}
	return a.User.Role
}

func actorID(a Actor) *int64 {
	if a == nil {
		return nil
	}
	return &a.User.ID
}

// UserFilter selects users for the admin list.
type UserFilter struct {
	Query  string // email or nickname fragment, or a user id
	Role   string
	Status string
	Before int64 // cursor: users with a smaller id (newest first)
	Offset int   // or skip this many (numbered pages, as Blessing Skin's API pages)
	Limit  int
}

func (f UserFilter) where() []predicate.User {
	var where []predicate.User
	if q := strings.TrimSpace(f.Query); q != "" {
		match := []predicate.User{user.EmailNormalizedContains(strings.ToLower(q)), user.NicknameContainsFold(q)}
		if id, err := strconv.ParseInt(q, 10, 64); err == nil {
			match = append(match, user.ID(id))
		}
		where = append(where, user.Or(match...))
	}
	if f.Role != "" {
		where = append(where, user.RoleEQ(user.Role(f.Role)))
	}
	if f.Status != "" {
		where = append(where, user.StatusEQ(user.Status(f.Status)))
	}
	return where
}

// ListUsers returns a page of users, newest first, and the cursor of the next page
// (0 when this is the last one).
func (s *Service) ListUsers(ctx context.Context, f UserFilter) ([]*ent.User, int64, error) {
	limit := pageSize(f.Limit)
	where := f.where()
	if f.Before > 0 {
		where = append(where, user.IDLT(f.Before))
	}
	list, err := s.db.User.Query().Where(where...).Order(ent.Desc(user.FieldID)).Offset(max(f.Offset, 0)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) > limit {
		list = list[:limit]
		next = list[limit-1].ID
	}
	return list, next, nil
}

// CountUsers is how many users the filter matches (Before, Offset and Limit aside).
func (s *Service) CountUsers(ctx context.Context, f UserFilter) (int, error) {
	return s.db.User.Query().Where(f.where()...).Count(ctx)
}

// GetUser returns one user.
func (s *Service) GetUser(ctx context.Context, id int64) (*ent.User, error) {
	u, err := s.db.User.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return u, err
}

// GetUserByEmail returns the user with this address (compared as stored: normalized).
func (s *Service) GetUserByEmail(ctx context.Context, email string) (*ent.User, error) {
	u, err := s.db.User.Query().Where(user.EmailNormalized(NormalizeEmail(email))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return u, err
}

// AdminUpdate changes the fields that are set.
type AdminUpdate struct {
	Nickname      *string
	Email         *string
	Role          *string // user | admin | superadmin
	Status        *string // active | banned
	Score         *int64
	Password      *string
	EmailVerified *bool
}

// mayManage applies the rules for who manages whom: administrators manage ordinary
// users; super administrators manage everyone; nobody changes their own role or status.
func mayManage(a Actor, target *ent.User, up AdminUpdate) error {
	role := actorRole(a)
	if role == user.RoleUser {
		return ErrForbidden
	}
	if target.Role != user.RoleUser && role != user.RoleSuperadmin {
		return fmt.Errorf("%w: only super administrators manage administrators", ErrForbidden)
	}
	if up.Role != nil && role != user.RoleSuperadmin {
		return fmt.Errorf("%w: only super administrators change roles", ErrForbidden)
	}
	if a != nil && a.User.ID == target.ID && (up.Role != nil || up.Status != nil) {
		return fmt.Errorf("%w: you cannot change your own role or status", ErrForbidden)
	}
	return nil
}

// MayManageOwned returns the user whose things (players, closet) a wants to change, if
// a may: their own things, or those of a user a may manage (administrators manage
// ordinary users, super administrators everyone, as Blessing Skin's player
// management did).
func (s *Service) MayManageOwned(ctx context.Context, a Actor, ownerID int64) (*ent.User, error) {
	owner, err := s.GetUser(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if a != nil && a.User.ID == owner.ID {
		return owner, nil
	}
	return owner, mayManage(a, owner, AdminUpdate{})
}

// AdminUpdateUser applies an administrator's change. Banning a user or setting their
// password signs them out everywhere.
func (s *Service) AdminUpdateUser(ctx context.Context, a Actor, id int64, up AdminUpdate, meta RequestMeta) (*ent.User, error) {
	target, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := mayManage(a, target, up); err != nil {
		return nil, err
	}
	var errs []error
	var changed []string
	var hash string
	if up.Nickname != nil {
		errs = append(errs, ValidateNickname(*up.Nickname))
		changed = append(changed, "nickname")
	}
	if up.Email != nil {
		errs = append(errs, ValidateEmail(*up.Email))
		changed = append(changed, "email")
	}
	if up.Role != nil {
		if !slices.Contains([]string{"user", "admin", "superadmin"}, *up.Role) {
			errs = append(errs, &FieldError{Field: "role", Message: "want user, admin or superadmin"})
		}
		changed = append(changed, "role")
	}
	if up.Status != nil {
		if *up.Status != "active" && *up.Status != "banned" {
			errs = append(errs, &FieldError{Field: "status", Message: "want active or banned"})
		}
		changed = append(changed, "status")
	}
	if up.Score != nil {
		if *up.Score < 0 {
			errs = append(errs, &FieldError{Field: "score", Message: "must be 0 or more"})
		}
		changed = append(changed, "score")
	}
	if up.EmailVerified != nil {
		changed = append(changed, "emailVerified")
	}
	if up.Password != nil {
		if err := ValidatePassword(*up.Password); err != nil {
			errs = append(errs, err)
		} else if hash, err = s.hasher.Hash(ctx, *up.Password); err != nil {
			return nil, err
		}
		changed = append(changed, "password")
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	meta = meta.clipped()
	now := s.now()
	var u *ent.User
	err = s.withTx(ctx, func(tx *ent.Client) error {
		// Losing the last active super administrator would lock everyone out of the
		// admin panel. A plain count is not enough: under READ COMMITTED (PostgreSQL,
		// MySQL) two super administrators demoting each other at once would both see
		// the other one. Writing a shared row first makes such transactions queue, and
		// the count, a statement after the lock, sees the earlier one's result.
		demoted := up.Role != nil && *up.Role != "superadmin"
		disabled := up.Status != nil && *up.Status != "active"
		if target.Role == user.RoleSuperadmin && (demoted || disabled) {
			if err := tx.SystemState.Create().SetID(superadminLock).SetIntValue(1).
				OnConflictColumns(systemstate.FieldID).
				Update(func(u *ent.SystemStateUpsert) { u.AddIntValue(1); u.UpdateUpdatedAt() }).
				Exec(ctx); err != nil {
				return err
			}
			others, err := tx.User.Query().
				Where(user.RoleEQ(user.RoleSuperadmin), user.StatusEQ(user.StatusActive), user.IDNEQ(id)).Count(ctx)
			if err != nil {
				return err
			}
			if others == 0 {
				return ErrLastSuperadmin
			}
		}
		upd := tx.User.UpdateOneID(id)
		if up.Nickname != nil {
			upd.SetNickname(strings.TrimSpace(*up.Nickname))
		}
		if up.Email != nil {
			upd.SetEmail(strings.TrimSpace(*up.Email)).SetEmailNormalized(NormalizeEmail(*up.Email))
			// Nobody has verified a new address; an explicit emailVerified in the same
			// request wins.
			if NormalizeEmail(*up.Email) != target.EmailNormalized && up.EmailVerified == nil {
				upd.ClearEmailVerifiedAt()
			}
		}
		if up.Role != nil {
			upd.SetRole(user.Role(*up.Role))
		}
		if up.Status != nil {
			// Also cancels a pending deletion: an administrator can restore an account.
			upd.SetStatus(user.Status(*up.Status)).ClearDeletedAt()
		}
		if up.EmailVerified != nil {
			if *up.EmailVerified {
				upd.SetEmailVerifiedAt(now)
			} else {
				upd.ClearEmailVerifiedAt()
			}
		}
		if hash != "" {
			upd.SetPasswordHash(hash).SetPasswordAlgo(AlgoArgon2id)
		}
		var err error
		if u, err = upd.Save(ctx); err != nil {
			if db.IsConstraint(err) {
				return &FieldError{Field: "email", Message: "already registered"}
			}
			return err
		}
		// The administrator sets a balance; the ledger records the difference, with
		// the administrator as the actor (design 02 F31).
		if up.Score != nil && *up.Score != u.Score {
			if u.Score, err = score.Apply(ctx, tx, score.Change{UserID: id, Delta: *up.Score - u.Score,
				Reason: score.ReasonAdmin, Actor: actorID(a)}); err != nil {
				return err
			}
		}
		if hash != "" || (up.Status != nil && *up.Status == "banned") {
			if err := revokeSessions(ctx, tx, id, now); err != nil {
				return err
			}
		}
		var spend []authtoken.Purpose
		if up.Email != nil && u.EmailNormalized != target.EmailNormalized {
			spend = append(spend, authtoken.PurposeVerifyEmail, authtoken.PurposeResetPassword)
		} else if hash != "" {
			spend = append(spend, authtoken.PurposeResetPassword)
		}
		if len(spend) > 0 {
			if err := spendTokens(ctx, tx, id, now, spend...); err != nil {
				return err
			}
		}
		auditMeta := map[string]any{"fields": changed}
		if up.Email != nil {
			auditMeta["email"] = []string{target.Email, u.Email}
		}
		if up.Role != nil {
			auditMeta["role"] = []string{string(target.Role), *up.Role}
		}
		if up.Status != nil {
			auditMeta["status"] = []string{string(target.Status), *up.Status}
		}
		if up.Score != nil {
			auditMeta["score"] = []int64{target.Score, *up.Score}
		}
		if up.Status != nil && *up.Status == "banned" && target.Status != user.StatusBanned {
			if err := hook.OnUserBanned.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: id}); err != nil {
				return err
			}
		}
		return audit(ctx, tx, actorID(a), "admin.user_updated", "user", strconv.FormatInt(id, 10), meta.IP, auditMeta)
	})
	return u, err
}

// AdminDeleteUser removes an account at once (no grace period). Super administrators
// must be demoted first; nobody deletes themselves here.
func (s *Service) AdminDeleteUser(ctx context.Context, a Actor, id int64, meta RequestMeta) error {
	target, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if err := mayManage(a, target, AdminUpdate{}); err != nil {
		return err
	}
	if a != nil && a.User.ID == id {
		return fmt.Errorf("%w: you cannot delete your own account here", ErrForbidden)
	}
	if target.Role == user.RoleSuperadmin {
		return fmt.Errorf("%w: demote the super administrator before deleting the account", ErrForbidden)
	}
	meta = meta.clipped()
	err = s.withTx(ctx, func(tx *ent.Client) error {
		if err := s.beforeDelete(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.User.DeleteOneID(id).Exec(ctx); err != nil {
			return err
		}
		if err := hook.OnUserDeleted.Publish(ctx, s.hooks, tx, hook.UserEvent{UserID: id}); err != nil {
			return err
		}
		return audit(ctx, tx, actorID(a), "admin.user_deleted", "user", strconv.FormatInt(id, 10), meta.IP,
			map[string]any{"email": target.Email, "nickname": target.Nickname})
	})
	if err == nil {
		s.kick() // the deletion may have queued file sweeps
	}
	return err
}

// AuditFilter selects audit records.
type AuditFilter struct {
	Action  string // exact action, or a prefix ending in "." (e.g. "auth.")
	ActorID int64
	Before  int64
	Limit   int
}

// ListAudit returns audit records, newest first, and the next cursor (0 at the end).
func (s *Service) ListAudit(ctx context.Context, f AuditFilter) ([]*ent.AuditLog, int64, error) {
	limit := pageSize(f.Limit)
	var where []predicate.AuditLog
	if a := f.Action; a != "" {
		if strings.HasSuffix(a, ".") {
			where = append(where, auditlog.ActionHasPrefix(a))
		} else {
			where = append(where, auditlog.Action(a))
		}
	}
	if f.ActorID > 0 {
		where = append(where, auditlog.ActorID(f.ActorID))
	}
	if f.Before > 0 {
		where = append(where, auditlog.IDLT(f.Before))
	}
	list, err := s.db.AuditLog.Query().Where(where...).Order(ent.Desc(auditlog.FieldID)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) > limit {
		list = list[:limit]
		next = list[limit-1].ID
	}
	return list, next, nil
}

// pageSize is the page size for list endpoints: 50 when not given, at most 100.
func pageSize(n int) int {
	if n <= 0 {
		return 50
	}
	return min(n, 100)
}
