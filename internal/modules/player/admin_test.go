package player

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

func TestSearch(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		steve, _, _ := f.s.Create(ctx, alice, "Steve", official, rates)
		_, _, _ = f.s.Create(ctx, alice, "Stella", official, rates)
		herobrine, _, _ := f.s.Create(ctx, bob, "Herobrine", official, rates)

		names := func(q Filter) []string {
			t.Helper()
			list, _, err := f.s.Search(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, p := range list {
				if p.Edges.Owner == nil {
					t.Fatal("players come with their owner")
				}
				out = append(out, p.Name)
			}
			return out
		}
		if got := names(Filter{Query: "STE"}); len(got) != 2 || got[0] != "Stella" {
			t.Fatalf("name prefix, ignoring case, newest first: %v", got)
		}
		if got := names(Filter{Query: "brine"}); len(got) != 0 {
			t.Fatalf("only the start of a name matches: %v", got)
		}
		dashed := herobrine.UUID[:8] + "-" + herobrine.UUID[8:12] + "-" + herobrine.UUID[12:16] + "-" + herobrine.UUID[16:20] + "-" + herobrine.UUID[20:]
		if got := names(Filter{Query: dashed}); len(got) != 1 || got[0] != "Herobrine" {
			t.Fatalf("by UUID with dashes: %v", got)
		}
		if got := names(Filter{Query: " " + strconv.FormatInt(steve.ID, 10) + " "}); len(got) != 1 || got[0] != "Steve" {
			t.Fatalf("by id: %v", got)
		}
		if got := names(Filter{UserID: bob}); len(got) != 1 {
			t.Fatalf("by owner: %v", got)
		}
		page, next, _ := f.s.Search(ctx, Filter{Limit: 2})
		rest, end, _ := f.s.Search(ctx, Filter{Limit: 2, Before: next})
		if len(page) != 2 || next == 0 || len(rest) != 1 || end != 0 {
			t.Fatalf("pages: %d (next %d), %d (next %d)", len(page), next, len(rest), end)
		}
	})
}

func TestTransfer(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		gone := f.user(t, "gone@example.com", 0)
		d.Ent.User.UpdateOneID(gone).SetStatus(user.StatusDeleted).ExecX(ctx)
		skin := f.texture(t, alice, "steve", "aa", true)
		cape := f.texture(t, alice, "cape", "bb", true)
		d.Ent.ClosetItem.Create().SetUserID(bob).SetTextureID(cape).SetName("cape").ExecX(ctx) // bob has the cape too
		p, _, _ := f.s.Create(ctx, alice, "Steve", official, rates)
		if _, err := f.s.Wear(ctx, alice, p.ID, &skin, &cape, nil); err != nil {
			t.Fatal(err)
		}

		moved, err := f.s.Transfer(ctx, p.ID, bob)
		if err != nil || moved.UserID != bob || moved.Edges.Owner.ID != bob {
			t.Fatalf("Transfer: %+v, %v", moved, err)
		}
		if moved.SkinID != nil || moved.CapeID == nil || *moved.CapeID != cape {
			t.Fatalf("what bob's closet lacks comes off, the rest stays: skin %v cape %v", moved.SkinID, moved.CapeID)
		}
		if _, err := f.s.Transfer(ctx, p.ID, gone); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("to a deleted account: %v", err)
		}
		if _, err := f.s.Transfer(ctx, p.ID, 999999); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("to nobody: %v", err)
		}
		if _, err := f.s.Transfer(ctx, 999999, bob); !errors.Is(err, ErrNotFound) {
			t.Fatalf("no such player: %v", err)
		}
		// The creation fee stays with alice: bob deleting it gets nothing back.
		if got, err := f.s.Delete(ctx, bob, p.ID, true, nil); err != nil || got != 0 {
			t.Fatalf("delete after transfer: %d, %v", got, err)
		}
	})
}
