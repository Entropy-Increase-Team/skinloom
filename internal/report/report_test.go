package report

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entreport "github.com/Entropy-Increase-Team/skinloom/ent/report"
	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	enttexture "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// charged: a report costs 5 and an upheld one earns 10.
var charged = score.Rates{ReporterModification: -5, ReporterReward: 10, ReturnOnDelete: true}

type fixture struct {
	s  *Service
	tx *texture.Service
	db *ent.Client
}

func newFixture(t *testing.T, d *db.DB) *fixture {
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	textures := texture.NewService(d.Ent, store, quiet)
	return &fixture{s: NewService(d.Ent, textures, quiet), tx: textures, db: d.Ent}
}

func (f *fixture) user(t *testing.T, email string, points int64) int64 {
	t.Helper()
	return f.db.User.Create().SetEmail(email).SetEmailNormalized(email).SetNickname(email[:1]).
		SetPasswordHash("x").SetScore(points).SaveX(context.Background()).ID
}

func (f *fixture) score(id int64) int64 { return f.db.User.GetX(context.Background(), id).Score }

func (f *fixture) upload(t *testing.T, owner int64, public bool, seed byte) *ent.Texture {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x) + seed, G: byte(y), B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	r, err := f.tx.Upload(context.Background(), owner, texture.UploadInput{Name: "skin", Type: "steve", Public: public, Data: buf.Bytes()},
		texture.Policy{MaxBytes: 1 << 20, MaxWidth: texture.MaxWidth, PrivateStatus: 403}, score.Rates{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Texture
}

func (f *fixture) submit(t *testing.T, reporter, textureID int64) *ent.Report {
	t.Helper()
	rep, _, err := f.s.Submit(context.Background(), texture.Viewer{UserID: reporter}, textureID, "stolen skin", charged)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func noBan(context.Context, int64) error { panic("ban must not be called") }

func TestSubmit(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		poor := f.user(t, "poor@example.com", 3)
		skin := f.upload(t, alice, true, 1)
		secret := f.upload(t, alice, false, 2)
		asBob := texture.Viewer{UserID: bob}

		rep, balance, err := f.s.Submit(ctx, asBob, skin.ID, "  stolen from my server  ", charged)
		if err != nil || balance != 95 || rep.Reason != "stolen from my server" || rep.Status != entreport.StatusPending ||
			rep.UploaderID == nil || *rep.UploaderID != alice || *rep.ReporterID != bob {
			t.Fatalf("Submit: %+v, %d, %v", rep, balance, err)
		}
		if _, _, err := f.s.Submit(ctx, asBob, skin.ID, "again", charged); !errors.Is(err, ErrDuplicate) || f.score(bob) != 95 {
			t.Fatalf("twice: %v, score %d", err, f.score(bob))
		}
		if _, _, err := f.s.Submit(ctx, texture.Viewer{UserID: alice}, skin.ID, "mine", charged); !errors.Is(err, ErrOwnTexture) {
			t.Fatalf("own texture: %v", err)
		}
		if _, _, err := f.s.Submit(ctx, asBob, secret.ID, "hidden", charged); !errors.Is(err, texture.ErrPrivate) {
			t.Fatalf("a private texture of someone else cannot be reported: %v", err)
		}
		for _, reason := range []string{"   ", strings.Repeat("长", MaxReason+1)} {
			if _, _, err := f.s.Submit(ctx, asBob, secret.ID, reason, charged); len(validate.FieldErrors(err)) != 1 {
				t.Fatalf("reason %q: %v", reason[:3], err)
			}
		}
		if _, _, err := f.s.Submit(ctx, texture.Viewer{UserID: poor}, skin.ID, "no money", charged); !errors.Is(err, score.ErrInsufficient) {
			t.Fatalf("a charge needs the score: %v", err)
		}
		if n := f.db.Report.Query().CountX(ctx); n != 1 {
			t.Fatalf("refused reports must leave nothing behind: %d reports", n)
		}
		mine, next, err := f.s.List(ctx, Filter{ReporterID: bob})
		if err != nil || len(mine) != 1 || next != 0 || mine[0].Edges.Texture == nil || mine[0].Edges.Texture.ID != skin.ID {
			t.Fatalf("List mine: %v, %d, %v", mine, next, err)
		}
	})
}

// Ten submissions of the same report at once: one report, one charge.
func TestSubmitConcurrent(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		skin := f.upload(t, alice, true, 1)
		var wg sync.WaitGroup
		errs := make(chan error, 10)
		for range 10 {
			wg.Go(func() {
				_, _, err := f.s.Submit(ctx, texture.Viewer{UserID: bob}, skin.ID, "dup", charged)
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		ok := 0
		for err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, ErrDuplicate):
				t.Fatal(err)
			}
		}
		if ok != 1 || f.score(bob) != 95 || f.db.Report.Query().CountX(ctx) != 1 {
			t.Fatalf("%d succeeded, score %d", ok, f.score(bob))
		}
	})
}

func TestReviewDelete(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		carol := f.user(t, "carol@example.com", 100)
		admin := f.user(t, "admin@example.com", 0)
		skin := f.upload(t, alice, true, 1)
		other := f.upload(t, alice, true, 2)
		first := f.submit(t, bob, skin.ID)
		second := f.submit(t, carol, skin.ID)
		unrelated := f.submit(t, carol, other.ID)

		got, err := f.s.Review(ctx, &admin, first.ID, ActionDelete, charged, true, noBan, nil)
		if err != nil || got.Status != entreport.StatusResolved || *got.Action != entreport.ActionDelete ||
			*got.HandlerID != admin || got.HandledAt == nil || got.TextureID != nil {
			t.Fatalf("Review: %+v, %v", got, err)
		}
		if _, err := f.db.Texture.Get(ctx, skin.ID); !ent.IsNotFound(err) {
			t.Fatalf("the texture must be deleted: %v", err)
		}
		if f.score(bob) != 100+10 {
			t.Fatalf("the reporter gets the charge back and the reward: %d", f.score(bob))
		}
		// carol's report of the same texture is settled with it: charge back, no reward.
		if r := f.db.Report.GetX(ctx, second.ID); r.Status != entreport.StatusResolved || *r.HandlerID != admin {
			t.Fatalf("other reports of the texture: %+v", r)
		}
		if f.score(carol) != 100-5 { // her report of the other texture is still pending
			t.Fatalf("carol: %d", f.score(carol))
		}
		if r := f.db.Report.GetX(ctx, unrelated.ID); r.Status != entreport.StatusPending {
			t.Fatalf("reports of other textures stay pending: %s", r.Status)
		}
		if _, err := f.s.Review(ctx, &admin, first.ID, ActionReject, charged, true, noBan, nil); !errors.Is(err, ErrReviewed) {
			t.Fatalf("a second review: %v", err)
		}
		if n := f.db.ScoreEntry.Query().Where(scoreentry.Reason(score.ReasonReportReward)).CountX(ctx); n != 1 {
			t.Fatalf("one reward: %d", n)
		}

		// The uploader deleted the texture before the review: charge back, no reward.
		late := f.submit(t, bob, other.ID)
		if err := f.tx.Delete(ctx, texture.Viewer{UserID: alice}, other.ID, true, nil); err != nil {
			t.Fatal(err)
		}
		before := f.score(bob)
		if got, err := f.s.Review(ctx, nil, late.ID, ActionDelete, charged, true, noBan, nil); err != nil ||
			got.Status != entreport.StatusResolved || got.HandlerID != nil {
			t.Fatalf("texture gone: %+v, %v", got, err)
		}
		if f.score(bob) != before+5 {
			t.Fatalf("charge back only: %d → %d", before, f.score(bob))
		}
	})
}

func TestReviewReject(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		skin := f.upload(t, alice, true, 1)
		other := f.upload(t, alice, true, 2)

		// A charge is kept on rejection.
		rep := f.submit(t, bob, skin.ID)
		if got, err := f.s.Review(ctx, nil, rep.ID, ActionReject, charged, true, noBan, nil); err != nil || got.Status != entreport.StatusRejected {
			t.Fatalf("reject: %+v, %v", got, err)
		}
		if f.score(bob) != 95 {
			t.Fatalf("charge kept: %d", f.score(bob))
		}
		if _, err := f.db.Texture.Get(ctx, skin.ID); err != nil {
			t.Fatalf("a rejected report leaves the texture: %v", err)
		}

		// A submit reward is taken back, but not below zero.
		rewarded := score.Rates{ReporterModification: 3}
		rep2, balance, err := f.s.Submit(ctx, texture.Viewer{UserID: bob}, other.ID, "reward", rewarded)
		if err != nil || balance != 98 {
			t.Fatalf("rewarded submit: %d, %v", balance, err)
		}
		f.db.User.UpdateOneID(bob).SetScore(1).ExecX(ctx)
		if _, err := f.s.Review(ctx, nil, rep2.ID, ActionReject, rewarded, true, noBan, nil); err != nil || f.score(bob) != 1 {
			t.Fatalf("spent reward: %v, %d", err, f.score(bob))
		}
	})
}

func TestReviewBan(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		skin := f.upload(t, alice, true, 1)
		rep := f.submit(t, bob, skin.ID)

		refused := errors.New("not allowed")
		if _, err := f.s.Review(ctx, nil, rep.ID, ActionBan, charged, true,
			func(context.Context, int64) error { return refused }, nil); !errors.Is(err, refused) {
			t.Fatalf("a refused ban: %v", err)
		}
		if r := f.db.Report.GetX(ctx, rep.ID); r.Status != entreport.StatusPending || f.score(bob) != 95 {
			t.Fatalf("a refused ban changes nothing: %s, %d", r.Status, f.score(bob))
		}
		var banned int64
		got, err := f.s.Review(ctx, nil, rep.ID, ActionBan, charged, true, func(_ context.Context, id int64) error { banned = id; return nil }, nil)
		if err != nil || banned != alice || got.Status != entreport.StatusResolved || f.score(bob) != 110 {
			t.Fatalf("ban: %+v, %v, banned %d, bob %d", got, err, banned, f.score(bob))
		}
		if _, err := f.db.Texture.Get(ctx, skin.ID); err != nil {
			t.Fatalf("a ban leaves the texture: %v", err)
		}

		// The uploader's account is gone: nobody to ban.
		carol := f.user(t, "carol@example.com", 100)
		mine := f.upload(t, carol, true, 3)
		rep2 := f.submit(t, bob, mine.ID)
		f.db.User.DeleteOneID(carol).ExecX(ctx)
		if _, err := f.s.Review(ctx, nil, rep2.ID, ActionBan, charged, true, noBan, nil); !errors.Is(err, ErrNoUploader) {
			t.Fatalf("no uploader: %v", err)
		}
		if _, err := f.s.Review(ctx, nil, rep2.ID, "explode", charged, true, noBan, nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("unknown action: %v", err)
		}
	})
}

// Two administrators review one report at once: one review wins, the reporter is
// settled once.
func TestReviewConcurrent(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		bob := f.user(t, "bob@example.com", 100)
		for round := range 5 {
			skin := f.upload(t, alice, true, byte(10+round))
			rep := f.submit(t, bob, skin.ID)
			start := make(chan struct{})
			errs := make(chan error, 2)
			for _, action := range []string{ActionDelete, ActionReject} {
				go func() {
					<-start
					_, err := f.s.Review(ctx, nil, rep.ID, action, charged, true, noBan, nil)
					errs <- err
				}()
			}
			close(start)
			won := 0
			for range 2 {
				switch err := <-errs; {
				case err == nil:
					won++
				case !errors.Is(err, ErrReviewed):
					t.Fatalf("round %d: %v", round, err)
				}
			}
			if won != 1 {
				t.Fatalf("round %d: %d reviews won", round, won)
			}
		}
		rewards := f.db.ScoreEntry.Query().Where(scoreentry.UserID(bob), scoreentry.Reason(score.ReasonReportReward)).CountX(ctx)
		refunds := f.db.ScoreEntry.Query().Where(scoreentry.UserID(bob), scoreentry.Reason(score.ReasonReportRefund)).CountX(ctx)
		if rewards != refunds {
			t.Fatalf("each upheld report settles once: %d rewards, %d refunds", rewards, refunds)
		}
		if want := 100 - 5*5 + 15*rewards; f.score(bob) != int64(want) {
			t.Fatalf("bob %d, want %d", f.score(bob), want)
		}
	})
}

// Administrators handling reports of one texture at the same time: one deletes it and
// settles the rest, the others find their report reviewed. They used to claim each
// other's report in opposite orders and deadlock (PostgreSQL included), and a
// report rejected meanwhile made the deletion fail.
func TestReviewsOfOneTextureConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", 100)
		for round := range 3 {
			skin := f.upload(t, alice, true, byte(10+round))
			var reps []*ent.Report
			for i := range 4 {
				reporter := f.user(t, fmt.Sprintf("r%d-%d@example.com", round, i), 100)
				reps = append(reps, f.submit(t, reporter, skin.ID))
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			won := 0
			for i, rep := range reps {
				action := ActionDelete
				if i == 3 {
					action = ActionReject
				}
				wg.Go(func() {
					_, err := f.s.Review(ctx, nil, rep.ID, action, charged, true, noBan, nil)
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						won++
					case !errors.Is(err, ErrReviewed):
						t.Errorf("round %d, %s: %v", round, action, err)
					}
				})
			}
			wg.Wait()
			if won == 0 || f.db.Report.Query().Where(entreport.StatusEQ(entreport.StatusPending)).CountX(ctx) != 0 {
				t.Fatalf("round %d: %d reviews won, some reports still pending", round, won)
			}
			if f.db.Texture.Query().Where(enttexture.ID(skin.ID)).ExistX(ctx) {
				t.Fatalf("round %d: the texture is still there", round)
			}
		}
	})
}

// The reporter's account is deleted between reading the report and settling it (the
// ban runs first, outside the transaction). Review decided from the report read
// before its transaction and paid the reward to the deleted account: "user not
// found", HTTP 500. It reads the report again under its locks now.
func TestReviewReporterDeletedMeanwhile(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		uploader := f.user(t, "up@example.com", 100)
		reporter := f.user(t, "rep@example.com", 100)
		skin := f.upload(t, uploader, true, 7)
		rep := f.submit(t, reporter, skin.ID)
		deleteReporter := func(ctx context.Context, _ int64) error {
			return f.db.User.DeleteOneID(reporter).Exec(ctx) // what an administrator does meanwhile
		}
		got, err := f.s.Review(ctx, nil, rep.ID, ActionBan, charged, true, deleteReporter, nil)
		if err != nil {
			t.Fatalf("review: %v", err)
		}
		if got.Status != "resolved" || got.ReporterID != nil {
			t.Fatalf("report %+v", got)
		}
	})
}
