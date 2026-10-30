package yggdrasil

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggtoken"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var policy = Policy{TokenValid: time.Hour, TokenRefresh: 2 * time.Hour, TokensPerUser: 10, SearchMax: 3}

var names = player.Policy{Rule: player.RuleOfficial, Min: 3, Max: 16}

var meta = account.RequestMeta{IP: "203.0.113.7"}

type fixture struct {
	s       *Service
	db      *ent.Client
	players *player.Service
	now     time.Time
}

func newFixture(t *testing.T, d *db.DB) *fixture {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	players := player.NewService(d.Ent, quiet)
	f := &fixture{db: d.Ent, players: players, now: time.Now()}
	f.s = NewService(d.Ent, account.NewService(d.Ent, quiet), players, texture.NewService(d.Ent, store, quiet), quiet)
	f.s.KeyBits = 1024 // 4096 takes seconds to generate
	f.s.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) user(t *testing.T, email string) int64 {
	t.Helper()
	hash, err := account.NewHasher(1).Hash(context.Background(), "password-1")
	if err != nil {
		t.Fatal(err)
	}
	return f.db.User.Create().SetEmail(email).SetEmailNormalized(account.NormalizeEmail(email)).SetNickname("u").
		SetPasswordHash(hash).SetScore(1000).SetLocale("zh-CN").SaveX(context.Background()).ID
}

func (f *fixture) player(t *testing.T, owner int64, name string) *ent.Player {
	t.Helper()
	p, _, err := f.players.Create(context.Background(), owner, name, names, score.Rates{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) login(t *testing.T, username string) *AuthAnswer {
	t.Helper()
	a, err := f.s.Authenticate(context.Background(), AuthRequest{Username: username, Password: "password-1", RequestUser: true}, policy, meta)
	if err != nil {
		t.Fatalf("login %s: %v", username, err)
	}
	return a
}

func isErr(err error, want *Error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == want.Kind && e.Message == want.Message
}

func TestAuthenticate(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "Alice@Example.com")
		steve := f.player(t, alice, "Steve")

		a := f.login(t, "alice@example.com")
		if a.SelectedProfile == nil || a.SelectedProfile.ID != steve.UUID || len(a.AvailableProfiles) != 1 ||
			a.ClientToken == "" || a.User == nil || a.User.ID != userUUID("alice@example.com") ||
			a.User.Properties[0].Value != "zh_CN" || strings.Count(a.AccessToken, ".") != 2 {
			t.Fatalf("answer: %+v", a)
		}
		if _, err := f.s.Authenticate(ctx, AuthRequest{Username: "alice@example.com", Password: "wrong"}, policy, meta); !isErr(err, errInvalidCredentials) {
			t.Fatalf("wrong password: %v", err)
		}
		if _, err := f.s.Authenticate(ctx, AuthRequest{Username: "nobody", Password: "x"}, policy, meta); !isErr(err, errInvalidCredentials) {
			t.Fatalf("unknown player: %v", err)
		}
		if _, err := f.s.Authenticate(ctx, AuthRequest{Password: "x"}, policy, meta); err == nil {
			t.Fatal("no username")
		}

		// With a player name the token is bound to that player, whatever the case
		// (Blessing Skin left it unbound when there were several, #123).
		alex := f.player(t, alice, "Alex")
		if a := f.login(t, "ALEX"); a.SelectedProfile == nil || a.SelectedProfile.ID != alex.UUID || len(a.AvailableProfiles) != 2 {
			t.Fatalf("by player name: %+v", a)
		}
		if a := f.login(t, "alice@example.com"); a.SelectedProfile != nil {
			t.Fatalf("by email with two players: nothing selected, %+v", a.SelectedProfile)
		}

		// Oldest tokens go beyond the limit.
		small := policy
		small.TokensPerUser = 2
		for range 3 {
			if _, err := f.s.Authenticate(ctx, AuthRequest{Username: "Steve", Password: "password-1"}, small, meta); err != nil {
				t.Fatal(err)
			}
		}
		if n := f.db.YggToken.Query().Where(yggtoken.UserID(alice)).CountX(ctx); n != 2 {
			t.Fatalf("tokens kept: %d", n)
		}

		f.db.User.UpdateOneID(alice).SetStatus(user.StatusBanned).ExecX(ctx)
		if _, err := f.s.Authenticate(ctx, AuthRequest{Username: "Steve", Password: "password-1"}, policy, meta); !isErr(err, errBanned) {
			t.Fatalf("banned: %v", err)
		}
	})
}

// Repeated failures are locked out (a launcher cannot show a captcha) per client and
// per account, so one client cannot lock a player out from everywhere (Blessing Skin
// throttled by the username sent, which anyone could use against anyone).
func TestAuthenticateRateLimit(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		bob := f.user(t, "bob@example.com")
		f.player(t, bob, "Bobby")
		var last error
		for range 30 {
			_, last = f.s.Authenticate(ctx, AuthRequest{Username: "Bobby", Password: "nope-nope"}, policy, meta)
		}
		if last == nil || !strings.Contains(last.Error(), "Too many attempts") {
			t.Fatalf("after many failures: %v", last)
		}
		other := account.RequestMeta{IP: "198.51.100.9"}
		if _, err := f.s.Authenticate(ctx, AuthRequest{Username: "Bobby", Password: "password-1"}, policy, other); err != nil &&
			!strings.Contains(err.Error(), "Too many attempts") {
			t.Fatalf("another client: %v", err)
		}
	})
}

func TestRefreshValidateSignout(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		steve := f.player(t, alice, "Steve")
		alex := f.player(t, alice, "Alex")
		bob := f.user(t, "bob@example.com")
		bobby := f.player(t, bob, "Bobby")

		a := f.login(t, "alice@example.com") // unbound: two players
		if err := f.s.Validate(ctx, a.AccessToken, a.ClientToken); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Validate(ctx, a.AccessToken, "other-client"); !isErr(err, errInvalidToken) {
			t.Fatalf("other client token: %v", err)
		}
		if _, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: a.AccessToken, SelectedProfile: &ProfileRef{ID: bobby.UUID}}, policy); err == nil {
			t.Fatal("someone else's profile")
		}
		r, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: a.AccessToken, ClientToken: a.ClientToken,
			SelectedProfile: &ProfileRef{ID: alex.UUID, Name: "Alex"}, RequestUser: true}, policy)
		if err != nil || r.SelectedProfile == nil || r.SelectedProfile.ID != alex.UUID || r.ClientToken != a.ClientToken || r.User == nil {
			t.Fatalf("refresh: %+v, %v", r, err)
		}
		if err := f.s.Validate(ctx, a.AccessToken, ""); !isErr(err, errInvalidToken) {
			t.Fatal("the old token is revoked")
		}
		if _, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: r.AccessToken, SelectedProfile: &ProfileRef{ID: steve.UUID}}, policy); err == nil ||
			!strings.Contains(err.Error(), "already has a profile") {
			t.Fatalf("rebinding: %v", err)
		}

		// Expired: no longer valid, still refreshable; then neither.
		f.now = f.now.Add(policy.TokenValid + time.Minute)
		if err := f.s.Validate(ctx, r.AccessToken, ""); !isErr(err, errInvalidToken) {
			t.Fatalf("expired token valid: %v", err) // Blessing Skin never checked the JWT's expiry
		}
		r2, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: r.AccessToken}, policy)
		if err != nil || r2.SelectedProfile == nil || r2.SelectedProfile.ID != alex.UUID {
			t.Fatalf("refresh of an expired token: %+v, %v", r2, err)
		}
		f.now = f.now.Add(policy.TokenRefresh)
		if _, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: r2.AccessToken}, policy); !isErr(err, errInvalidToken) {
			t.Fatalf("past the refresh window: %v", err)
		}

		// Invalidate one; signout with a player name in another case revokes all
		// (Blessing Skin revoked nothing then).
		f.now = time.Now()
		b1, b2 := f.login(t, "Steve"), f.login(t, "Alex")
		if err := f.s.Invalidate(ctx, b1.AccessToken); err != nil || f.s.Validate(ctx, b1.AccessToken, "") == nil {
			t.Fatalf("invalidate: %v", err)
		}
		if err := f.s.Invalidate(ctx, "not-a-token"); err != nil {
			t.Fatalf("an unknown token is fine: %v", err)
		}
		if err := f.s.Signout(ctx, "aLeX", "password-1", meta); err != nil || f.s.Validate(ctx, b2.AccessToken, "") == nil {
			t.Fatalf("signout: %v", err)
		}
		if n, _ := f.s.Tokens(ctx, alice); n != 0 {
			t.Fatalf("tokens after signout: %d", n)
		}
	})
}

func verify(t *testing.T, f *fixture, p Property) {
	t.Helper()
	pemText, _, err := f.s.PublicKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(pemText))
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := base64.StdEncoding.DecodeString(p.Signature)
	sum := sha1.Sum([]byte(p.Value))
	if err := rsa.VerifyPKCS1v15(pub.(*rsa.PublicKey), crypto.SHA1, sum[:], sig); err != nil {
		t.Fatalf("signature of %s: %v", p.Name, err)
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{byte(x * 3), byte(y * 5), 77, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

var uploads = texture.Policy{MaxBytes: 1 << 20, MaxWidth: texture.MaxWidth, PrivateStatus: 403}

func TestJoinAndProfiles(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		steve := f.player(t, alice, "Steve")
		bob := f.user(t, "bob@example.com")
		f.player(t, bob, "Bobby")
		a := f.login(t, "alice@example.com")

		// A skin through the texture API, then the profile shows it.
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "skin", Slim: true, Name: "mine", Data: pngOf(t, 64, 64)}, uploads, score.Rates{}); err != nil {
			t.Fatal(err)
		}
		p, err := f.s.ProfileByUUID(ctx, strings.ToUpper(steve.UUID[:8])+"-"+steve.UUID[8:12]+"-"+steve.UUID[12:16]+"-"+steve.UUID[16:20]+"-"+steve.UUID[20:], "https://skins.example.com", false)
		if err != nil || p == nil || p.Name != "Steve" || p.Properties[0].Signature != "" || p.Properties[1].Value != "skin,cape" {
			t.Fatalf("unsigned profile: %+v, %v", p, err)
		}
		raw, _ := base64.StdEncoding.DecodeString(p.Properties[0].Value)
		var payload texturesPayload
		_ = json.Unmarshal(raw, &payload)
		skin := payload.Textures["SKIN"]
		if payload.ProfileID != steve.UUID || !strings.HasPrefix(skin.URL, "https://skins.example.com/textures/") ||
			skin.Metadata["model"] != "slim" || payload.SignatureRequired {
			t.Fatalf("payload: %+v", payload)
		}

		// join → hasJoined: signed, names compared ignoring case, the IP checked if given.
		if err := f.s.Join(ctx, a.AccessToken, steve.UUID, "server-1", "203.0.113.7"); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.HasJoined(ctx, "steve", "server-1", "", "https://skins.example.com")
		if err != nil || got == nil {
			t.Fatalf("hasJoined: %+v, %v", got, err)
		}
		for _, prop := range got.Properties {
			verify(t, f, prop)
		}
		if got, _ := f.s.HasJoined(ctx, "Steve", "server-1", "198.51.100.1", ""); got != nil {
			t.Fatal("another IP")
		}
		if got, _ := f.s.HasJoined(ctx, "Bobby", "server-1", "", ""); got != nil {
			t.Fatal("another name")
		}
		f.now = f.now.Add(JoinWindow + time.Second)
		if got, _ := f.s.HasJoined(ctx, "Steve", "server-1", "", ""); got != nil {
			t.Fatal("the join window passed")
		}
		f.now = time.Now()

		// Wrong profile, unbound token, deleted player in between (BS: 500).
		if err := f.s.Join(ctx, a.AccessToken, strings.Repeat("0", 32), "server-2", ""); !isErr(err, errInvalidToken) {
			t.Fatalf("another profile: %v", err)
		}
		if err := f.s.Join(ctx, a.AccessToken, steve.UUID, "server-3", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.players.Delete(ctx, alice, steve.ID, false); err != nil {
			t.Fatal(err)
		}
		if got, err := f.s.HasJoined(ctx, "Steve", "server-3", "", ""); got != nil || err != nil {
			t.Fatalf("deleted player: %v, %v", got, err)
		}
		if err := f.s.Validate(ctx, a.AccessToken, ""); err == nil {
			t.Fatal("tokens bound to a deleted player go with it")
		}

		// Lookups: one query, ignoring case, banned owners left out, a limit.
		refs, err := f.s.LookupNames(ctx, []string{"bobby", "BOBBY", "nobody"}, policy)
		if err != nil || len(refs) != 1 || refs[0].Name != "Bobby" {
			t.Fatalf("lookup: %v, %v", refs, err)
		}
		if _, err := f.s.LookupNames(ctx, []string{"a1a", "b2b", "c3c", "d4d"}, policy); err == nil {
			t.Fatal("over the limit")
		}
		if r, _ := f.s.LookupName(ctx, "BoBbY"); r == nil || r.Name != "Bobby" {
			t.Fatalf("single lookup: %v", r)
		}
		f.db.User.UpdateOneID(bob).SetStatus(user.StatusBanned).ExecX(ctx)
		if r, _ := f.s.LookupName(ctx, "Bobby"); r != nil {
			t.Fatal("banned owner")
		}
	})
}

// A token bound to a player an admin then gives to someone else no longer plays it.
func TestTransferredPlayerLeavesTokens(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		steve := f.player(t, alice, "Steve")
		bob := f.user(t, "bob@example.com")
		a := f.login(t, "alice@example.com")
		if _, err := f.players.Transfer(ctx, steve.ID, bob); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Join(ctx, a.AccessToken, steve.UUID, "server-1", ""); !isErr(err, errInvalidToken) {
			t.Fatalf("join with the old owner's token: %v", err)
		}
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "skin", Data: pngOf(t, 64, 64)}, uploads, score.Rates{}); !isErr(err, forbidden("Profile not owned by this token.")) {
			t.Fatalf("upload with the old owner's token: %v", err)
		}
		r, err := f.s.Refresh(ctx, RefreshRequest{AccessToken: a.AccessToken, ClientToken: a.ClientToken}, policy)
		if err != nil || r.SelectedProfile != nil {
			t.Fatalf("refresh: %+v, %v", r, err)
		}
	})
}

func TestTextureAPI(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com")
		steve := f.player(t, alice, "Steve")
		bob := f.user(t, "bob@example.com")
		bobby := f.player(t, bob, "Bobby")
		a := f.login(t, "alice@example.com")
		skin := Upload{Kind: "skin", Data: pngOf(t, 64, 64)}

		// Blessing Skin let any valid token change any player's textures.
		if err := f.s.UploadTexture(ctx, a.AccessToken, bobby.UUID, skin, uploads, score.Rates{}); err == nil || !strings.Contains(err.Error(), "not owned") {
			t.Fatalf("someone else's player: %v", err)
		}
		if err := f.s.ResetTexture(ctx, a.AccessToken, bobby.UUID, "skin"); err == nil {
			t.Fatal("reset someone else's")
		}
		var e *Error
		if err := f.s.UploadTexture(ctx, "bogus", steve.UUID, skin, uploads, score.Rates{}); !errors.As(err, &e) || e.Status != 401 {
			t.Fatalf("bad token: %v", err)
		}
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "cape", Data: pngOf(t, 64, 64)}, uploads, score.Rates{}); !errors.As(err, &e) || e.Status != 400 {
			t.Fatalf("a square cape: %v", err)
		}
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, skin, uploads, score.Rates{}); err != nil {
			t.Fatal(err)
		}
		p := f.db.Player.GetX(ctx, steve.ID)
		if p.SkinID == nil {
			t.Fatal("the uploaded skin is worn")
		}
		if tx := f.db.Texture.GetX(ctx, *p.SkinID); tx.Visibility != "private" || tx.Name != "Steve skin" {
			t.Fatalf("texture: %+v", tx)
		}
		if err := f.s.ResetTexture(ctx, a.AccessToken, steve.UUID, "skin"); err != nil || f.db.Player.GetX(ctx, steve.ID).SkinID != nil {
			t.Fatalf("reset: %v", err)
		}

		// Charged like any private upload: a cape alice can afford, then one she cannot.
		before := f.db.User.GetX(ctx, alice).Score
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "cape", Data: pngOf(t, 64, 32)}, uploads, score.Rates{PrivatePerStorage: 1}); err != nil {
			t.Fatal(err)
		}
		if after := f.db.User.GetX(ctx, alice).Score; after >= before {
			t.Fatalf("score %d → %d", before, after)
		}
		if err := f.s.UploadTexture(ctx, a.AccessToken, steve.UUID, Upload{Kind: "cape", Data: pngOf(t, 128, 64)}, uploads, score.Rates{PrivatePerStorage: 1_000_000}); !isErr(err, forbidden("Not enough score.")) {
			t.Fatalf("not enough score: %v", err)
		}
	})
}

func TestKeysSharedAndRevokedWithSessions(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f, g := newFixture(t, d), newFixture(t, d)
		k1, _, err := f.s.PublicKey(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if k2, _, _ := g.s.PublicKey(ctx); k1 != k2 {
			t.Fatal("instances of one site must share the signing key")
		}

		// Banning signs launchers out with the web sessions.
		alice := f.user(t, "alice@example.com")
		f.player(t, alice, "Steve")
		a := f.login(t, "Steve")
		ban := "banned"
		if _, err := account.NewService(d.Ent, quiet).AdminUpdateUser(ctx, nil, alice, account.AdminUpdate{Status: &ban}, meta); err != nil {
			t.Fatal(err)
		}
		if n := f.db.YggToken.Query().Where(yggtoken.UserID(alice)).CountX(ctx); n != 0 || f.s.Validate(ctx, a.AccessToken, "") == nil {
			t.Fatalf("tokens after a ban: %d", n)
		}

		// Cleanup removes what can no longer be used.
		f.db.User.UpdateOneID(alice).SetStatus(user.StatusActive).ExecX(ctx)
		f.login(t, "Steve")
		f.now = f.now.Add(policy.TokenRefresh + time.Minute)
		if n, err := f.s.Cleanup(ctx); err != nil || n != 1 {
			t.Fatalf("cleanup: %d, %v", n, err)
		}
	})
}
