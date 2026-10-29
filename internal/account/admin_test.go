package account

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// withRole creates a signed-in user with the given role.
func withRole(t *testing.T, s *Service, email string, role user.Role) *Viewer {
	t.Helper()
	_, token := newUser(t, s, email)
	s.db.User.Update().Where(user.EmailNormalized(email)).SetRole(role).ExecX(context.Background())
	return signedIn(t, s, token)
}

func ptr[T any](v T) *T { return &v }

func TestListUsers(t *testing.T) { forEachDB(t, testListUsers) }

func testListUsers(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	for i := range 5 {
		newUser(t, s, fmt.Sprintf("player%d@example.com", i))
	}
	page, next, err := s.ListUsers(ctx, UserFilter{Limit: 4})
	if err != nil || len(page) != 4 || next == 0 || page[0].ID < page[1].ID {
		t.Fatalf("first page: %d users, next %d, %v", len(page), next, err)
	}
	rest, end, _ := s.ListUsers(ctx, UserFilter{Limit: 4, Before: next})
	if len(rest) != 2 || end != 0 {
		t.Fatalf("second page: %d users, next %d", len(rest), end)
	}
	if found, _, _ := s.ListUsers(ctx, UserFilter{Query: "PLAYER3"}); len(found) != 1 || found[0].Email != "player3@example.com" {
		t.Fatalf("search by email: %v", found)
	}
	if found, _, _ := s.ListUsers(ctx, UserFilter{Role: "superadmin"}); len(found) != 1 {
		t.Fatalf("filter by role: %d", len(found))
	}
	if found, _, _ := s.ListUsers(ctx, UserFilter{Query: fmt.Sprint(page[0].ID)}); len(found) != 1 || found[0].ID != page[0].ID {
		t.Fatalf("search by id: %v", found)
	}
}

func TestAdminPermissions(t *testing.T) { forEachDB(t, testAdminPermissions) }

func testAdminPermissions(t *testing.T, s *Service) {
	ctx := context.Background()
	_, superSess := setupAdmin(t, s)
	super := signedIn(t, s, superSess.Token)
	admin := withRole(t, s, "mod@example.com", user.RoleAdmin)
	player, playerToken := newUser(t, s, "player@example.com")

	// Administrators manage ordinary users, but not other administrators or roles.
	if _, err := s.AdminUpdateUser(ctx, admin, player.User.ID, AdminUpdate{Nickname: ptr("Renamed"), Score: ptr(int64(50))}, meta); err != nil {
		t.Fatalf("admin edits a user: %v", err)
	}
	// newUser registers without an initial score, so the ledger holds only this.
	if e := s.db.ScoreEntry.Query().OnlyX(ctx); e.UserID != player.User.ID || e.Delta != 50 || e.Balance != 50 ||
		e.Reason != "admin" || e.ActorID == nil || *e.ActorID != admin.User.ID {
		t.Fatalf("an administrator's score change must be in the ledger with the actor: %+v", e)
	}
	if _, err := s.AdminUpdateUser(ctx, admin, super.User.ID, AdminUpdate{Nickname: ptr("x")}, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin edits a super admin: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, admin, player.User.ID, AdminUpdate{Role: ptr("admin")}, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin changes a role: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, player, player.User.ID, AdminUpdate{Score: ptr(int64(9999))}, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a user acting as admin: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, super, super.User.ID, AdminUpdate{Role: ptr("user")}, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("changing your own role: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, admin, player.User.ID, AdminUpdate{Score: ptr(int64(-1)), Role: nil}, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("negative score: %v", err)
	}

	// Banning signs the user out everywhere; so does an administrator setting a password.
	if _, err := s.AdminUpdateUser(ctx, admin, player.User.ID, AdminUpdate{Status: ptr("banned")}, meta); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Authenticate(ctx, playerToken); v != nil {
		t.Fatal("a banned user must be signed out")
	}

	// The last super administrator can neither be demoted nor banned, even by the token.
	if _, err := s.AdminUpdateUser(ctx, nil, super.User.ID, AdminUpdate{Role: ptr("admin")}, meta); !errors.Is(err, ErrLastSuperadmin) {
		t.Fatalf("demoting the last super admin: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, super, admin.User.ID, AdminUpdate{Role: ptr("superadmin")}, meta); err != nil {
		t.Fatalf("super admin promotes: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, super.User.ID, AdminUpdate{Role: ptr("admin")}, meta); err != nil {
		t.Fatalf("with two super admins one can step down: %v", err)
	}

	// Deleting: never yourself, never a super administrator.
	admin = signedIn(t, s, otherDevice(t, s, "mod@example.com", "user-pass-1")) // now a super admin
	if err := s.AdminDeleteUser(ctx, admin, admin.User.ID, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deleting yourself: %v", err)
	}
	if err := s.AdminDeleteUser(ctx, nil, admin.User.ID, meta); !errors.Is(err, ErrForbidden) {
		t.Fatalf("deleting a super admin: %v", err)
	}
	if err := s.AdminDeleteUser(ctx, admin, player.User.ID, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetUser(ctx, player.User.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user: %v", err)
	}

	recs, _, err := s.ListAudit(ctx, AuditFilter{Action: "admin."})
	if err != nil || len(recs) < 5 || recs[0].Action != "admin.user_deleted" {
		t.Fatalf("audit: %d records, %v", len(recs), err)
	}
}

func TestLastSuperadminConcurrent(t *testing.T) { forEachDB(t, testLastSuperadminConcurrent) }

// Two super administrators demote each other at the same moment: one must win and
// one must get ErrLastSuperadmin, never both succeed (which READ COMMITTED allowed).
func testLastSuperadminConcurrent(t *testing.T, s *Service) {
	ctx := context.Background()
	_, sess := setupAdmin(t, s)
	a := signedIn(t, s, sess.Token)
	b := withRole(t, s, "second@example.com", user.RoleSuperadmin)
	for round := range 10 {
		start := make(chan struct{})
		errs := make(chan error, 2)
		for _, pair := range [][2]*Viewer{{a, b}, {b, a}} {
			go func() {
				<-start
				_, err := s.AdminUpdateUser(ctx, pair[0], pair[1].User.ID, AdminUpdate{Role: ptr("admin")}, meta)
				errs <- err
			}()
		}
		close(start)
		var won, refused int
		for range 2 {
			switch err := <-errs; {
			case err == nil:
				won++
			case errors.Is(err, ErrLastSuperadmin):
				refused++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		left := s.db.User.Query().Where(user.RoleEQ(user.RoleSuperadmin), user.StatusEQ(user.StatusActive)).CountX(ctx)
		if won != 1 || refused != 1 || left != 1 {
			t.Fatalf("round %d: %d won, %d refused, %d super administrators left", round, won, refused, left)
		}
		s.db.User.Update().Where(user.RoleEQ(user.RoleAdmin)).SetRole(user.RoleSuperadmin).ExecX(ctx)
	}
}

func TestAdminChangesEmail(t *testing.T) { forEachDB(t, testAdminChangesEmail) }

func testAdminChangesEmail(t *testing.T, s *Service) {
	ctx := context.Background()
	setupAdmin(t, s)
	newUser(t, s, "taken@example.com")
	player, _ := newUser(t, s, "player@example.com")
	id := player.User.ID
	verify := func() { s.db.User.UpdateOneID(id).SetEmailVerifiedAt(time.Now()).ExecX(ctx) }
	verified := func() bool { return s.db.User.GetX(ctx, id).EmailVerifiedAt != nil }

	verify()
	if _, err := s.AdminUpdateUser(ctx, nil, id, AdminUpdate{Email: ptr("Player@Example.com")}, meta); err != nil || !verified() {
		t.Fatalf("a case-only change keeps verification: %v", err)
	}
	_ = s.RequestPasswordReset(ctx, "player@example.com", withMail, meta)
	oldReset, _ := queuedLink(t, s, "/auth/reset/")
	if _, err := s.AdminUpdateUser(ctx, nil, id, AdminUpdate{Email: ptr("new@example.com")}, meta); err != nil || verified() {
		t.Fatalf("a new address starts unverified: %v", err)
	}
	if err := s.ResetPassword(ctx, oldReset, "taken-over-1", meta); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("links mailed to the old address must stop working: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, id, AdminUpdate{Email: ptr("newer@example.com"), EmailVerified: ptr(true)}, meta); err != nil || !verified() {
		t.Fatalf("an explicit emailVerified wins: %v", err)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, id, AdminUpdate{Email: ptr("TAKEN@example.com")}, meta); len(FieldErrors(err)) != 1 {
		t.Fatalf("an address in use: %v", err)
	}

	recs, _, _ := s.ListAudit(ctx, AuditFilter{Action: "admin.user_updated"})
	if got := fmt.Sprint(recs[0].Meta["email"]); got != "[new@example.com newer@example.com]" {
		t.Fatalf("audit meta email = %s", got)
	}
}

func TestBeforeDeleteHook(t *testing.T) { forEachDB(t, testBeforeDeleteHook) }

// The hook (deleting private textures) runs only once deleting is allowed.
func testBeforeDeleteHook(t *testing.T, s *Service) {
	ctx := context.Background()
	_, sess := setupAdmin(t, s)
	super := signedIn(t, s, sess.Token)
	var called []int64
	s.SetBeforeDelete(func(_ context.Context, id int64) error { called = append(called, id); return nil })
	if err := s.AdminDeleteUser(ctx, super, super.User.ID, meta); !errors.Is(err, ErrForbidden) || len(called) != 0 {
		t.Fatalf("a refused delete must not run the hook: %v, %v", err, called)
	}
	v, _ := newUser(t, s, "gone@example.com")
	if err := s.AdminDeleteUser(ctx, super, v.User.ID, meta); err != nil || len(called) != 1 || called[0] != v.User.ID {
		t.Fatalf("admin delete: %v, %v", err, called)
	}
	w, _ := newUser(t, s, "expired@example.com")
	s.db.User.UpdateOneID(w.User.ID).SetStatus(user.StatusDeleted).SetDeletedAt(time.Now().Add(-DeleteGrace - time.Hour)).ExecX(ctx)
	if n, err := s.PurgeDeleted(ctx); err != nil || n != 1 || len(called) != 2 || called[1] != w.User.ID {
		t.Fatalf("purge: %d, %v, %v", n, err, called)
	}
}

func TestMayManageOwned(t *testing.T) { forEachDB(t, testMayManageOwned) }

// Players and closets: your own, or those of a user you may manage.
func testMayManageOwned(t *testing.T, s *Service) {
	ctx := context.Background()
	_, sess := setupAdmin(t, s)
	super := signedIn(t, s, sess.Token)
	mod := withRole(t, s, "mod@example.com", user.RoleAdmin)
	other := withRole(t, s, "other@example.com", user.RoleAdmin)
	player, _ := newUser(t, s, "player@example.com")

	for _, c := range []struct {
		name  string
		a     Actor
		owner int64
		ok    bool
	}{
		{"an administrator, an ordinary user's", mod, player.User.ID, true},
		{"an administrator, their own", mod, mod.User.ID, true},
		{"an administrator, another administrator's", mod, other.User.ID, false},
		{"a super administrator, an administrator's", super, mod.User.ID, true},
		{"the admin token, anyone's", nil, super.User.ID, true},
		{"a user, someone else's", player, mod.User.ID, false},
	} {
		owner, err := s.MayManageOwned(ctx, c.a, c.owner)
		if c.ok != (err == nil) || (c.ok && owner.ID != c.owner) {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: want ErrForbidden, got %v", c.name, err)
		}
	}
	if _, err := s.MayManageOwned(ctx, super, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no such user: %v", err)
	}
}
