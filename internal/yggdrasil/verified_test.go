package yggdrasil

import (
	"context"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

// With auth.require_email_verification on, an account that is not verified does not
// keep a launcher session alive. A token issued before the switch (or before the user
// changed their email, which clears the verification) used to be refreshed into a new
// one with a new refresh window, again and again, so it never expired.
func TestRefreshRequiresVerification(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		f.player(t, alice, "Steve")

		a := f.login(t, "Steve") // before the requirement, unverified
		strict := policy
		strict.RequireVerified = true
		if _, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: a.AccessToken, ClientToken: a.ClientToken}, strict); !isErr(err, errUnverified) {
			t.Fatalf("refresh of an unverified account's token with verification required: err = %v, want errUnverified", err)
		}
	})
}

// The texture API of a launcher token is gated the same way: an account that has to
// verify its email may not upload or reset textures through it.
func TestTextureAPIRequiresVerification(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		steve := f.player(t, alice, "Steve")

		a := f.login(t, "Steve")
		strict := policy
		strict.RequireVerified = true
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "skin", Data: pngOf(t, 64, 64)}, strict, uploads, score.Rates{}); !isErr(err, errUnverified) {
			t.Errorf("upload by an unverified account: err = %v, want errUnverified", err)
		}
		if err := f.s.ResetTexture(ctx, a.AccessToken, steve.UUID, "skin", strict); !isErr(err, errUnverified) {
			t.Errorf("reset by an unverified account: err = %v, want errUnverified", err)
		}
	})
}
