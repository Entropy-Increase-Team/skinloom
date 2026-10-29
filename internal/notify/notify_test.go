package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/notification"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func newUser(t *testing.T, d *db.DB, email string, role user.Role, created time.Time) int64 {
	t.Helper()
	return d.Ent.User.Create().SetEmail(email).SetEmailNormalized(email).SetNickname("u").SetPasswordHash("x").
		SetRole(role).SetCreatedAt(created).SaveX(context.Background()).ID
}

func TestInbox(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		s := NewService(d.Ent, quiet)
		alice := newUser(t, d, "alice@example.com", user.RoleUser, time.Now())
		bob := newUser(t, d, "bob@example.com", user.RoleUser, time.Now())

		if _, err := s.Send(ctx, alice, "  ", "x"); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("empty title: %v", err)
		}
		if _, err := s.Send(ctx, alice, strings.Repeat("长", MaxTitle+1), strings.Repeat("x", MaxContent+1)); len(validate.FieldErrors(err)) != 2 {
			t.Fatalf("too long: %v", err)
		}
		first, _ := s.Send(ctx, alice, "Hello", "**welcome**")
		second, _ := s.Send(ctx, alice, "Again", "")
		if n, err := s.Unread(ctx, alice); err != nil || n != 2 {
			t.Fatalf("unread: %d, %v", n, err)
		}
		list, next, err := s.List(ctx, alice, Filter{Limit: 1})
		if err != nil || len(list) != 1 || list[0].ID != second.ID || next == 0 {
			t.Fatalf("newest first: %v, %d, %v", list, next, err)
		}
		if err := s.MarkRead(ctx, bob, first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("someone else's notification: %v", err)
		}
		if err := s.MarkRead(ctx, alice, first.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.MarkRead(ctx, alice, first.ID); err != nil {
			t.Fatalf("reading twice is fine: %v", err)
		}
		if unread, _, _ := s.List(ctx, alice, Filter{Unread: true}); len(unread) != 1 || unread[0].ID != second.ID {
			t.Fatalf("unread only: %v", unread)
		}
		if n, err := s.MarkAllRead(ctx, alice); err != nil || n != 1 {
			t.Fatalf("mark all: %d, %v", n, err)
		}
		if n, _ := s.Unread(ctx, alice); n != 0 {
			t.Fatalf("after mark all: %d", n)
		}
	})
}

func TestAnnouncements(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		s := NewService(d.Ent, quiet)
		old := newUser(t, d, "old@example.com", user.RoleUser, time.Now().Add(-time.Hour))
		admin := newUser(t, d, "admin@example.com", user.RoleAdmin, time.Now().Add(-time.Hour))

		if _, err := s.Announce(ctx, nil, "everyone", "x", ""); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("bad audience: %v", err)
		}
		if _, err := s.Announce(ctx, &admin, AudienceAll, "Maintenance", "Tonight"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Announce(ctx, nil, AudienceUsers, "Users only", ""); err != nil {
			t.Fatal(err)
		}
		late := newUser(t, d, "late@example.com", user.RoleUser, time.Now().Add(time.Second))

		count := func(id int64) int {
			t.Helper()
			n, err := s.Unread(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		if count(old) != 2 || count(admin) != 1 || count(late) != 0 {
			t.Fatalf("delivered: old %d admin %d late %d", count(old), count(admin), count(late))
		}
		// Delivering again adds nothing; ten deliveries at once add one copy each.
		if count(old) != 2 {
			t.Fatal("delivered twice")
		}
		_, _ = s.Announce(ctx, nil, AudienceAll, "Third", "")
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				if _, err := s.Unread(ctx, old); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if n := d.Ent.Notification.Query().Where(notification.UserID(old)).CountX(ctx); n != 3 {
			t.Fatalf("one copy per announcement: %d", n)
		}
		list, _, _ := s.List(ctx, old, Filter{})
		if list[0].Title != "Third" || list[0].AnnouncementID == nil {
			t.Fatalf("copies keep the announcement: %+v", list[0])
		}
		if got := d.Ent.User.GetX(ctx, old).AnnouncementsSeen; got != *list[0].AnnouncementID {
			t.Fatalf("announcements_seen %d", got)
		}
	})
}

func TestMarkdown(t *testing.T) {
	for _, c := range []struct{ in, want, not string }{
		{"**bold**", "<strong>bold</strong>", ""},
		{"line one\nline two", "<br", ""},
		{"<script>alert(1)</script>", "", "<script"},
		{"[click](javascript:alert(1))", "", "javascript:"},
		{"<img src=x onerror=alert(1)>", "", "onerror"},
		{"| a | b |\n|---|---|\n| 1 | 2 |", "<table>", ""},
	} {
		got := Markdown(c.in)
		if c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
		if c.not != "" && strings.Contains(got, c.not) {
			t.Errorf("%q → %q must not contain %q", c.in, got, c.not)
		}
	}
}
