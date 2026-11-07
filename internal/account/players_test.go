package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
)

func TestStatusChangesAnnouncePlayers(t *testing.T) { forEachDB(t, testStatusChangesAnnouncePlayers) }

// Banning, unbanning, deleting and restoring an account change what game clients get
// for each of its players, and removing it removes them: each player is announced by
// name, so the CDN drops what it cached (ADR 0039; it used to keep serving a banned
// player's skin).
func testStatusChangesAnnouncePlayers(t *testing.T, s *Service) {
	ctx := context.Background()
	bus := hook.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	t.Cleanup(func() { bus.Close(ctx) })
	s.SetHooks(bus)
	var got []string
	record := func(event string) func(context.Context, hook.PlayerEvent) error {
		return func(_ context.Context, e hook.PlayerEvent) error {
			got = append(got, event+" "+e.Name)
			return nil
		}
	}
	if err := bus.Replace("test", hook.OnPlayerUpdated.Handle(0, record("updated")), hook.OnPlayerDeleted.Handle(0, record("deleted"))); err != nil {
		t.Fatal(err)
	}
	// deliver hands the outbox to the bus, as the job runner would, and returns what
	// the listener heard.
	deliver := func() string {
		t.Helper()
		got = nil
		for _, j := range s.db.Job.Query().Where(job.Kind(hook.OutboxJobKind)).AllX(ctx) {
			if err := bus.Deliver(ctx, json.RawMessage(j.Payload)); err != nil {
				t.Fatal(err)
			}
			s.db.Job.DeleteOneID(j.ID).ExecX(ctx)
		}
		slices.Sort(got)
		return strings.Join(got, ", ")
	}
	player := func(userID int64, name string) {
		t.Helper()
		now := time.Now()
		s.db.Player.Create().SetUserID(userID).SetName(name).SetNameLower(strings.ToLower(name)).
			SetUUID(fmt.Sprintf("%032x", len(name)*7919+int(userID))).SetCreatedAt(now).SetUpdatedAt(now).ExecX(ctx)
	}

	setupAdmin(t, s)
	alice, _ := newUser(t, s, "alice@example.com")
	bob, _ := newUser(t, s, "bob@example.com")
	player(alice.User.ID, "Steve")
	player(alice.User.ID, "Alex")
	player(bob.User.ID, "Bob")
	deliver()

	both := func(event string) string { return event + " Alex, " + event + " Steve" }
	if _, err := s.AdminUpdateUser(ctx, nil, alice.User.ID, AdminUpdate{Status: ptr("banned")}, meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != both("updated") {
		t.Fatalf("ban: %q", h)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, alice.User.ID, AdminUpdate{Status: ptr("banned"), Nickname: ptr("A")}, meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != "" {
		t.Fatalf("banned again, nothing changed for the players: %q", h)
	}
	if _, err := s.AdminUpdateUser(ctx, nil, alice.User.ID, AdminUpdate{Status: ptr("active")}, meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != both("updated") {
		t.Fatalf("unban: %q", h)
	}

	v := signedIn(t, s, otherDevice(t, s, "alice@example.com", "user-pass-1"))
	if err := s.DeleteAccount(ctx, v, "user-pass-1", meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != both("updated") {
		t.Fatalf("deleted by the user: %q", h)
	}
	if _, _, err := s.Login(ctx, LoginInput{Email: "alice@example.com", Password: "user-pass-1"}, noCaptcha, meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != both("updated") {
		t.Fatalf("restored by signing in: %q", h)
	}
	if err := s.AdminDeleteUser(ctx, nil, alice.User.ID, meta); err != nil {
		t.Fatal(err)
	}
	if h := deliver(); h != both("deleted") {
		t.Fatalf("deleted by an administrator: %q", h)
	}

	// The end of the grace period.
	if err := s.DeleteAccount(ctx, bob, "user-pass-1", meta); err != nil {
		t.Fatal(err)
	}
	deliver()
	real := s.now
	s.now = func() time.Time { return real().Add(DeleteGrace + time.Hour) }
	defer func() { s.now = real }()
	if n, err := s.PurgeDeleted(ctx); n != 1 || err != nil {
		t.Fatalf("PurgeDeleted = %d, %v", n, err)
	}
	if h := deliver(); h != "deleted Bob" {
		t.Fatalf("purged after the grace period: %q", h)
	}
}
