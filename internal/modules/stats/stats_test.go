package stats

import (
	"context"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

func TestCompute(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		shanghai, _ := time.LoadLocation("Asia/Shanghai")
		now := time.Date(2026, 10, 24, 10, 0, 0, 0, shanghai)

		empty, err := Compute(ctx, d.Ent, now, shanghai)
		if err != nil || empty.Users != 0 || empty.StorageBytes != 0 || len(empty.Days) != Days {
			t.Fatalf("empty site: %+v, %v", empty, err)
		}
		if first, last := empty.Days[0].Date, empty.Days[Days-1].Date; first != "2026-09-24" || last != "2026-10-24" {
			t.Fatalf("31 days ending today: %s … %s", first, last)
		}

		n := 0
		user := func(at time.Time) int64 {
			n++
			email := string(rune('a'+n)) + "@example.com"
			return d.Ent.User.Create().SetEmail(email).SetEmailNormalized(email).SetNickname("u").
				SetPasswordHash("x").SetCreatedAt(at.In(time.Local)).SaveX(ctx).ID // as time.Now writes it
		}
		// 00:10 in Shanghai is still the previous day in UTC: it counts for the 24th.
		user(time.Date(2026, 10, 24, 0, 10, 0, 0, shanghai))
		user(time.Date(2026, 10, 23, 23, 50, 0, 0, shanghai))
		uploader := user(time.Date(2026, 8, 1, 0, 0, 0, 0, shanghai)) // before the chart
		blob := func(hash string, size int64) {
			d.Ent.Blob.Create().SetID(hash).SetSize(size).SetWidth(64).SetHeight(64).SetRefCount(1).ExecX(ctx)
		}
		blob("aa", 1000)
		blob("bb", 24)
		tex := func(hash string, vis texture.Visibility, at time.Time) *ent.Texture {
			return d.Ent.Texture.Create().SetName("t").SetType(texture.TypeSteve).SetHash(hash).SetSize(1).
				SetVisibility(vis).SetUploaderID(uploader).SetCreatedAt(at.In(time.Local)).SaveX(ctx)
		}
		tex("aa", texture.VisibilityPublic, time.Date(2026, 10, 24, 9, 0, 0, 0, shanghai))
		tex("bb", texture.VisibilityPrivate, time.Date(2026, 9, 24, 0, 0, 0, 0, shanghai)) // first second of the chart

		o, err := Compute(ctx, d.Ent, now, shanghai)
		if err != nil {
			t.Fatal(err)
		}
		if o.Users != 3 || o.Textures != 2 || o.PublicTextures != 1 || o.Files != 2 || o.StorageBytes != 1024 {
			t.Fatalf("totals: %+v", o)
		}
		day := func(date string) Day {
			for _, d := range o.Days {
				if d.Date == date {
					return d
				}
			}
			t.Fatalf("no day %s", date)
			return Day{}
		}
		if got := day("2026-10-24"); got.Registrations != 1 || got.Uploads != 1 {
			t.Fatalf("today: %+v", got)
		}
		if got := day("2026-10-23"); got.Registrations != 1 || got.Uploads != 0 {
			t.Fatalf("yesterday: %+v", got)
		}
		if got := day("2026-09-24"); got.Uploads != 1 {
			t.Fatalf("first day: %+v", got)
		}
		sum := 0
		for _, d := range o.Days {
			sum += d.Registrations
		}
		if sum != 2 {
			t.Fatalf("registrations before the chart are not counted: %d", sum)
		}
	})
}

// Rows get created_at from time.Now, in the server's zone; the site's zone can be
// another. SQLite compares the stored text, so day bounds in the site's zone put rows
// in the wrong day: 23:30 on the 23rd, ten hours west of the server, is written as
// the 24th and was counted there.
func TestComputeInAnotherZone(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		_, off := time.Now().Zone()
		west := time.FixedZone("server-10h", off-10*3600)
		now := time.Date(2026, 10, 24, 12, 0, 0, 0, west)
		late := time.Date(2026, 10, 23, 23, 30, 0, 0, west).In(time.Local)
		d.Ent.User.Create().SetEmail("a@example.com").SetEmailNormalized("a@example.com").SetNickname("u").
			SetPasswordHash("x").SetCreatedAt(late).ExecX(ctx)
		o, err := Compute(ctx, d.Ent, now, west)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for _, day := range o.Days {
			if day.Registrations > 0 {
				got[day.Date] = day.Registrations
			}
		}
		if len(got) != 1 || got["2026-10-23"] != 1 {
			t.Fatalf("registrations by day: %v", got)
		}
	})
}
