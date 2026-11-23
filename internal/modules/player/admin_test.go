package player

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/auditlog"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthcode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthdevicecode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggsession"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggtoken"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
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

func TestKeepUUIDOnRenameAndRemapUUID(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice@example.com", 1000)
		deleted := f.user(t, "deleted@example.com", 1000)

		p1, _, err := f.s.Create(ctx, alice, "Steve", official, rates)
		if err != nil {
			t.Fatal(err)
		}
		p2, _, err := f.s.Create(ctx, alice, "Alex", official, rates)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = f.s.Create(ctx, deleted, "Ghost", official, rates)
		if err != nil {
			t.Fatal(err)
		}
		d.Ent.User.UpdateOneID(deleted).SetStatus(user.StatusDeleted).ExecX(ctx)

		origUUID := p1.UUID
		// KeepUUIDOnRename preserves v3 UUID across rename.
		keepPolicy := official
		keepPolicy.KeepUUIDOnRename = true
		renamed, err := f.s.Rename(ctx, alice, p1.ID, "SteveNew", keepPolicy)
		if err != nil {
			t.Fatalf("rename with keep_uuid_on_rename: %v", err)
		}
		if renamed.UUID != origUUID {
			t.Fatalf("UUID changed under KeepUUIDOnRename: want %s, got %s", origUUID, renamed.UUID)
		}

		// FindByUUID accepts dashed uppercase UUIDs.
		dashed := origUUID[:8] + "-" + origUUID[8:12] + "-" + origUUID[12:16] + "-" + origUUID[16:20] + "-" + origUUID[20:]
		found, err := f.s.FindByUUID(ctx, dashed)
		if err != nil || found.ID != p1.ID {
			t.Fatalf("FindByUUID(%s): got %v, err %v", dashed, found, err)
		}

		// ListAll excludes deleted owners and paginates by afterID.
		page1, err := f.s.ListAll(ctx, 0, 1)
		if err != nil || len(page1) != 1 || page1[0].ID != p1.ID {
			t.Fatalf("ListAll page1: %v, err %v", page1, err)
		}
		page2, err := f.s.ListAll(ctx, page1[0].ID, 10)
		if err != nil || len(page2) != 1 || page2[0].ID != p2.ID {
			t.Fatalf("ListAll page2: %v, err %v", page2, err)
		}

		// Seed a stale join session and remap UUID.
		d.Ent.YggSession.Create().SetServerID("srv-1").SetPlayerID(p1.ID).SetExpiresAt(time.Now().Add(time.Minute)).ExecX(ctx)
		newUUID := "0123456789abcdef0123456789abcdef"
		rec := (&audit.Entry{Action: "player.remapUuid"}).Target("player", origUUID)
		remapped, err := f.s.RemapUUID(ctx, origUUID, newUUID, rec)
		if err != nil || remapped.UUID != newUUID {
			t.Fatalf("RemapUUID: %+v, err %v", remapped, err)
		}
		sessCnt, _ := d.Ent.YggSession.Query().Where(yggsession.PlayerID(p1.ID)).Count(ctx)
		if sessCnt != 0 {
			t.Fatalf("expected stale ygg_sessions cleared on RemapUUID, got %d", sessCnt)
		}
		auditCnt, _ := d.Ent.AuditLog.Query().Where(auditlog.Action("player.remapUuid")).Count(ctx)
		if auditCnt != 1 {
			t.Fatalf("expected 1 audit entry for RemapUUID, got %d", auditCnt)
		}
		// Conflict with p2 UUID must be rejected.
		if _, err := f.s.RemapUUID(ctx, newUUID, p2.UUID, nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("expected conflict error remapping to existing UUID, got %v", err)
		}
		// L2: Nil UUID and malformed dashed UUIDs must be rejected.
		if _, err := f.s.RemapUUID(ctx, newUUID, "00000000000000000000000000000000", nil); len(validate.FieldErrors(err)) != 1 {
			t.Fatalf("expected error remapping to nil UUID, got %v", err)
		}
		if _, ok := NormalizeUUID("0123-456789ab-cdef-0123-456789abcdef"); ok {
			t.Fatal("expected malformed dashed UUID to be rejected")
		}
	})
}

func TestPlayerSecurityRegressions(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := fixture{s: NewService(d.Ent, quiet), db: d.Ent}
		alice := f.user(t, "alice-reg@example.com", 1000)

		// M4: With UUIDv4=false + KeepUUIDOnRename=true, renaming "Steve" -> "SteveRenamed"
		// and creating a new "Steve" must NOT fail with a UUID collision.
		keepPolicy := official
		keepPolicy.UUIDv4 = false
		keepPolicy.KeepUUIDOnRename = true

		p1, _, err := f.s.Create(ctx, alice, "Steve", keepPolicy, rates)
		if err != nil {
			t.Fatalf("create initial Steve: %v", err)
		}
		if _, err := f.s.Rename(ctx, alice, p1.ID, "SteveRenamed", keepPolicy); err != nil {
			t.Fatalf("rename Steve: %v", err)
		}
		pReclaimed, _, err := f.s.Create(ctx, alice, "Steve", keepPolicy, rates)
		if err != nil {
			t.Fatalf("reclaim name Steve after KeepUUIDOnRename failed: %v", err)
		}
		if pReclaimed.UUID == p1.UUID {
			t.Fatalf("reclaimed player must receive distinct UUID, got duplicate %s", pReclaimed.UUID)
		}

		// H5: RemapUUID must revoke YggToken, OAuthToken, OAuthCode, and OAuthDeviceCode bound to the player.
		clientRow := d.Ent.OAuthClient.Create().
			SetName("RemapCli").
			SetRedirectUris([]string{"http://127.0.0.1/cb"}).
			SetUserID(alice).SaveX(ctx)
		exp := time.Now().Add(time.Hour)
		d.Ent.YggToken.Create().SetTokenHash("ygg-hash-1").SetClientToken("ct").SetUserID(alice).SetPlayerID(p1.ID).SetExpiresAt(exp).SetRefreshUntil(exp).ExecX(ctx)
		d.Ent.OAuthToken.Create().SetKind(oauthtoken.KindOauth).SetTokenHash("oa-hash-1").SetClientID(clientRow.ID).SetUserID(alice).SetPlayerID(p1.ID).SetScopes([]string{"openid"}).SetExpiresAt(exp).ExecX(ctx)
		d.Ent.OAuthCode.Create().SetCodeHash("oc-hash-1").SetClientID(clientRow.ID).SetUserID(alice).SetPlayerID(p1.ID).SetRedirectURI("http://127.0.0.1/cb").SetScopes([]string{"openid"}).SetExpiresAt(exp).ExecX(ctx)
		d.Ent.OAuthDeviceCode.Create().SetDeviceCodeHash("od-hash-1").SetUserCodeHash("ou-hash-1").SetClientID(clientRow.ID).SetUserID(alice).SetPlayerID(p1.ID).SetScopes([]string{"openid"}).SetStatus(oauthdevicecode.StatusApproved).SetExpiresAt(exp).ExecX(ctx)

		targetUUID := "fedcba9876543210fedcba9876543210"
		if _, err := f.s.RemapUUID(ctx, p1.UUID, targetUUID, nil); err != nil {
			t.Fatalf("RemapUUID: %v", err)
		}
		if n, _ := d.Ent.YggToken.Query().Where(yggtoken.PlayerID(p1.ID)).Count(ctx); n != 0 {
			t.Fatalf("expected YggToken revoked on RemapUUID, got %d", n)
		}
		if n, _ := d.Ent.OAuthToken.Query().Where(oauthtoken.PlayerID(p1.ID)).Count(ctx); n != 0 {
			t.Fatalf("expected OAuthToken revoked on RemapUUID, got %d", n)
		}
		if n, _ := d.Ent.OAuthCode.Query().Where(oauthcode.PlayerID(p1.ID)).Count(ctx); n != 0 {
			t.Fatalf("expected OAuthCode revoked on RemapUUID, got %d", n)
		}
		if n, _ := d.Ent.OAuthDeviceCode.Query().Where(oauthdevicecode.PlayerID(p1.ID)).Count(ctx); n != 0 {
			t.Fatalf("expected OAuthDeviceCode revoked on RemapUUID, got %d", n)
		}
	})
}
