package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var official = Policy{Rule: RuleOfficial, Min: 3, Max: 16}

func TestValidateName(t *testing.T) {
	cases := []struct {
		name string
		p    Policy
		ok   bool
	}{
		{"Steve_01", official, true},
		{"ab", official, false},                    // too short
		{strings.Repeat("a", 17), official, false}, // too long
		{"史蒂夫", official, false},
		{"史蒂夫", Policy{Rule: RuleCJK, Min: 1, Max: 16}, true},
		{"Ünïcødé", Policy{Rule: RuleUTF8, Min: 3, Max: 16}, true},
		{"a b", Policy{Rule: RuleUTF8, Min: 1, Max: 16}, false},
		{"a/b", Policy{Rule: RuleUTF8, Min: 1, Max: 16}, false}, // would break /{name}.json
		{"a%2e", Policy{Rule: RuleUTF8, Min: 1, Max: 16}, false},
		{"tab\t", Policy{Rule: RuleUTF8, Min: 1, Max: 16}, false},
		{"ab-c", Policy{Rule: RuleCustom, Custom: regexp.MustCompile(`^[a-z-]+$`), Min: 1, Max: 16}, true},
		{"AB", Policy{Rule: RuleCustom, Custom: regexp.MustCompile(`^[a-z-]+$`), Min: 1, Max: 16}, false},
		{strings.Repeat("a", 33), Policy{Rule: RuleUTF8, Min: 1, Max: 99}, false}, // the column's limit
	}
	for _, c := range cases {
		err := ValidateName(c.name, c.p)
		if (err == nil) != c.ok {
			t.Errorf("%q %+v: %v", c.name, c.p.Rule, err)
		}
		if err != nil && len(validate.FieldErrors(err)) != 1 {
			t.Errorf("%q: want a field error, got %v", c.name, err)
		}
	}
}

func TestUUIDs(t *testing.T) {
	// The offline-mode UUID of "Notch" (UUID.nameUUIDFromBytes("OfflinePlayer:Notch")).
	if got := OfflineUUID("Notch"); got != "b50ad385829d3141a2167e7d7539ba7f" {
		t.Fatalf("OfflineUUID(Notch) = %s", got)
	}
	if OfflineUUID("notch") == OfflineUUID("Notch") {
		t.Fatal("offline UUIDs depend on the exact name")
	}
	seen := map[string]bool{}
	for range 100 {
		u := RandomUUID()
		if len(u) != 32 || u[12] != '4' || !strings.ContainsRune("89ab", rune(u[16])) || seen[u] {
			t.Fatalf("RandomUUID = %s", u)
		}
		seen[u] = true
	}
}

type fixture struct {
	s  *Service
	db *ent.Client
}

func (f fixture) user(t *testing.T, email string, points int64) int64 {
	t.Helper()
	return f.db.User.Create().SetEmail(email).SetEmailNormalized(email).SetNickname("u").
		SetPasswordHash("x").SetScore(points).SaveX(context.Background()).ID
}

// texture creates a texture of typ in the user's closet (and one outside it).
func (f fixture) texture(t *testing.T, owner int64, typ, hash string, inCloset bool) int64 {
	t.Helper()
	ctx := context.Background()
	f.db.Blob.Create().SetID(hash).SetSize(1).SetWidth(64).SetHeight(64).SetRefCount(1).ExecX(ctx)
	tx := f.db.Texture.Create().SetName(typ).SetType(textureType(typ)).SetHash(hash).SetSize(1).SetUploaderID(owner).SaveX(ctx)
	if inCloset {
		f.db.ClosetItem.Create().SetUserID(owner).SetTextureID(tx.ID).SetName(typ).ExecX(ctx)
	}
	return tx.ID
}

var rates = score.Rates{PerPlayer: 100, ReturnOnDelete: true}

func textureType(s string) tex.Type { return tex.Type(s) }

func TestPlayers(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 250)
		bob := f.user(t, "bob@example.com", 1000)

		p, balance, err := f.s.Create(ctx, alice, " Steve ", official, rates)
		if err != nil || p.Name != "Steve" || p.NameLower != "steve" || p.UUID != OfflineUUID("Steve") || balance != 150 {
			t.Fatalf("Create = %+v, %d, %v", p, balance, err)
		}
		// Names are unique ignoring case, across users.
		if _, _, err := f.s.Create(ctx, bob, "STEVE", official, rates); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("taken name in another case: %v", err)
		}
		if _, _, err := f.s.Create(ctx, alice, "Alex", official, rates); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Create(ctx, alice, "Third", official, rates); !errors.Is(err, score.ErrInsufficient) {
			t.Fatalf("not enough score: %v", err)
		}
		if n := f.db.Player.Query().CountX(ctx); n != 2 {
			t.Fatalf("players %d", n)
		}

		// Rename: v3 UUIDs follow the name, v4 ones stay.
		r, err := f.s.Rename(ctx, alice, p.ID, "Herobrine", official)
		if err != nil || r.Name != "Herobrine" || r.UUID != OfflineUUID("Herobrine") {
			t.Fatalf("rename v3: %+v, %v", r, err)
		}
		v4 := official
		v4.UUIDv4 = true
		r, err = f.s.Rename(ctx, alice, p.ID, "Herobrine2", v4)
		if err != nil || r.UUID != OfflineUUID("Herobrine") {
			t.Fatalf("rename v4 keeps the UUID: %+v, %v", r, err)
		}
		if _, err := f.s.Rename(ctx, alice, p.ID, "alex", official); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("rename onto a taken name: %v", err)
		}
		if _, err := f.s.Rename(ctx, bob, p.ID, "Mine", official); !errors.Is(err, ErrNotFound) {
			t.Fatalf("someone else's player: %v", err)
		}

		// Delete gives back what was paid (the ledger), not today's price.
		if got, err := f.s.Delete(ctx, alice, p.ID, true, nil); err != nil || got != 100 {
			t.Fatalf("Delete refunded %d, %v", got, err)
		}
		if u := f.db.User.GetX(ctx, alice); u.Score != 150 {
			t.Fatalf("score after the refund: %d", u.Score)
		}
		if _, err := f.s.Delete(ctx, alice, p.ID, true, nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted twice: %v", err)
		}
		free, _, _ := f.s.Create(ctx, bob, "FreeOne", official, score.Rates{})
		if got, err := f.s.Delete(ctx, bob, free.ID, true, nil); err != nil || got != 0 {
			t.Fatalf("a free player refunds nothing: %d, %v", got, err)
		}
	})
}

// Regression for Blessing Skin C01: two requests for one name at once made two
// players with the same name.
func TestCreateSameNameConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		var users []int64
		for _, e := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"} {
			users = append(users, f.user(t, e, 1000))
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		won := 0
		for i, u := range users {
			wg.Go(func() {
				name := []string{"Notch", "notch", "NOTCH", "nOtCh"}[i]
				_, _, err := f.s.Create(ctx, u, name, official, rates)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					won++
				case len(validate.FieldErrors(err)) == 1:
				default:
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if won != 1 || f.db.Player.Query().CountX(ctx) != 1 || f.db.ScoreEntry.Query().CountX(ctx) != 1 {
			t.Fatalf("%d won, %d players, %d charges", won, f.db.Player.Query().CountX(ctx), f.db.ScoreEntry.Query().CountX(ctx))
		}
	})
}

func TestWearAndProfile(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 1000)
		bob := f.user(t, "bob@example.com", 1000)
		slim := f.texture(t, alice, "alex", strings.Repeat("a", 64), true)
		cape := f.texture(t, alice, "cape", strings.Repeat("c", 64), true)
		notMine := f.texture(t, bob, "steve", strings.Repeat("b", 64), true)
		p, _, _ := f.s.Create(ctx, alice, "Steve", official, rates)

		if _, err := f.s.Profile(ctx, "steve"); err != nil {
			t.Fatalf("a new player: %v", err)
		}
		// A cape is not a skin (C07), and only closet textures can be worn.
		_, err := f.s.Wear(ctx, alice, p.ID, &cape, &slim, nil)
		if fe := validate.FieldErrors(err); len(fe) != 2 {
			t.Fatalf("wrong kinds: %v", err)
		}
		if _, err := f.s.Wear(ctx, alice, p.ID, &notMine, nil, nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("not in the closet: %v", err)
		}
		missing := int64(999999)
		if _, err := f.s.Wear(ctx, alice, p.ID, &missing, nil, nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("no such texture: %v", err)
		}
		worn, err := f.s.Wear(ctx, alice, p.ID, &slim, &cape, nil)
		if err != nil || worn.Edges.Skin == nil || worn.Edges.Cape == nil {
			t.Fatalf("Wear: %+v, %v", worn, err)
		}

		pr, err := f.s.Profile(ctx, "STEVE")
		if err != nil || pr.Name != "Steve" || !pr.Slim || pr.Model() != "slim" || pr.Skin != strings.Repeat("a", 64) ||
			pr.Cape != strings.Repeat("c", 64) || pr.UUID != OfflineUUID("Steve") {
			t.Fatalf("Profile = %+v, %v", pr, err)
		}

		// Taking the cape off leaves the skin.
		zero := int64(0)
		if _, err := f.s.Wear(ctx, alice, p.ID, nil, &zero, nil); err != nil {
			t.Fatal(err)
		}
		if pr, _ := f.s.Profile(ctx, "Steve"); pr.Cape != "" || pr.Skin == "" {
			t.Fatalf("after taking the cape off: %+v", pr)
		}
		// Deleting the texture takes it off the player (foreign key).
		f.db.ClosetItem.Delete().ExecX(ctx)
		f.db.Texture.DeleteOneID(slim).ExecX(ctx)
		if pr, _ := f.s.Profile(ctx, "Steve"); pr.Skin != "" || pr.Model() != "default" {
			t.Fatalf("after the skin was deleted: %+v", pr)
		}

		if _, err := f.s.Profile(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown name: %v", err)
		}
		f.db.User.UpdateOneID(alice).SetStatus(user.StatusBanned).ExecX(ctx)
		if _, err := f.s.Profile(ctx, "Steve"); !errors.Is(err, ErrBanned) {
			t.Fatalf("banned owner: %v", err)
		}
		f.db.User.UpdateOneID(alice).SetStatus(user.StatusDeleted).ExecX(ctx)
		if _, err := f.s.Profile(ctx, "Steve"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted owner: %v", err)
		}
	})
}

// One user creating several players at once: each is created and charged once
// (MySQL deadlocked on the user's row; see score.Lock).
func TestCreateSeveralConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		u := f.user(t, "a@example.com", 1000)
		var wg sync.WaitGroup
		for i := range 4 {
			wg.Go(func() {
				if _, _, err := f.s.Create(ctx, u, fmt.Sprintf("Player%d", i), official, rates); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if n := f.db.Player.Query().CountX(ctx); n != 4 || f.db.User.GetX(ctx, u).Score != 1000-4*rates.PerPlayer {
			t.Fatalf("%d players, score %d", n, f.db.User.GetX(ctx, u).Score)
		}
	})
}
