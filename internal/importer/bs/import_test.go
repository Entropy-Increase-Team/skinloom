package bs

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	"github.com/Entropy-Increase-Team/skinloom/ent/notification"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/sitesettings"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"

	_ "modernc.org/sqlite"
)

// bsSchema is Blessing Skin 6's schema as Laravel creates it on SQLite, with the
// Yggdrasil plugin's uuid table.
const bsSchema = `
CREATE TABLE users (uid integer primary key autoincrement, email varchar(100) not null, nickname varchar(50) not null default '',
  locale varchar null, score integer not null, avatar integer not null default 0, password varchar(255) not null, ip varchar(45) not null,
  is_dark_mode tinyint(1) not null default 0, permission integer not null default 0, last_sign_at datetime not null, register_at datetime not null,
  verified tinyint(1) not null default 0, verification_token varchar not null default '', remember_token varchar null);
CREATE TABLE players (pid integer primary key autoincrement, uid integer not null, name varchar(50) not null, tid_cape integer not null default 0,
  last_modified datetime not null, tid_skin integer not null default -1);
CREATE TABLE textures (tid integer primary key autoincrement, name varchar(50) not null, type varchar(10) not null, hash varchar(64) not null,
  size integer not null, uploader integer not null, public tinyint(1) not null, upload_at datetime not null, likes integer not null default 0);
CREATE TABLE options (id integer primary key autoincrement, option_name varchar(50) not null, option_value text not null);
CREATE TABLE user_closet (user_uid integer not null, texture_tid integer not null, item_name text null);
CREATE TABLE reports (id integer primary key autoincrement, tid integer not null, uploader integer not null, reporter integer not null,
  reason text not null, status integer not null, report_at datetime not null);
CREATE TABLE notifications (id varchar not null primary key, type varchar not null, notifiable_type varchar not null, notifiable_id integer not null,
  data text not null, read_at datetime null, created_at datetime null, updated_at datetime null);
CREATE TABLE uuid (id integer primary key autoincrement, name varchar not null, uuid varchar(255) not null);
CREATE TABLE language_lines (id integer primary key autoincrement, "group" varchar not null, "key" varchar not null, text text not null,
  created_at datetime null, updated_at datetime null);
CREATE TABLE oauth_clients (id integer primary key autoincrement, user_id integer null, name varchar not null, secret varchar(100) null,
  provider varchar null, redirect text not null, personal_access_client tinyint(1) not null, password_client tinyint(1) not null,
  revoked tinyint(1) not null, created_at datetime null, updated_at datetime null);
`

// skinPNG is a valid w×h PNG; seed makes files differ.
func skinPNG(t *testing.T, w, h int, seed uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{seed, 1, 2, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func hashOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func md5hex(s string) string { b := md5.Sum([]byte(s)); return hex.EncodeToString(b[:]) }

type fixture struct {
	src                         Source
	skinA, skinB, cape, altered string // BS hashes
	alteredContent              string // hash of the altered file's content
	sqlitePath                  string
}

// newFixture writes a small, dirty BS site: duplicate player names, a texture uploaded
// twice by one user, dangling references, a missing and an altered file, duplicate
// closet rows and reports.
func newFixture(t *testing.T, extra ...string) fixture {
	t.Helper()
	dir := t.TempDir()
	texDir := filepath.Join(dir, "textures")
	if err := os.Mkdir(texDir, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(b []byte, name string) string {
		if name == "" {
			name = hashOf(b)
		}
		if err := os.WriteFile(filepath.Join(texDir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return name
	}
	f := fixture{sqlitePath: filepath.Join(dir, "bs.sqlite")}
	f.skinA = put(skinPNG(t, 64, 64, 1), "")
	f.skinB = put(skinPNG(t, 64, 32, 2), "")
	f.cape = put(skinPNG(t, 64, 32, 3), "")
	alteredBytes := skinPNG(t, 64, 64, 4)
	f.altered = hashOf(skinPNG(t, 64, 64, 5)) // the name says one thing, the content another
	put(alteredBytes, f.altered)
	f.alteredContent = hashOf(alteredBytes)
	missing := hashOf([]byte("never uploaded"))

	pool, err := sql.Open("sqlite", f.sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bcryptHash, _ := bcrypt.GenerateFromPassword([]byte("bcrypt-password"), bcrypt.MinCost)
	salted := md5hex(md5hex("salted-password") + "pepper")
	stmts := []string{bsSchema,
		`INSERT INTO users (uid, email, nickname, locale, score, avatar, password, ip, permission, last_sign_at, register_at, verified) VALUES
		 (1, 'Admin@Example.com', 'Admin', 'zh_CN', 1000, 2, '` + string(bcryptHash) + `', '127.0.0.1', 2, '2024-01-02 03:04:05', '2020-05-06 07:08:09', 1),
		 (2, 'bob@example.com', '', 'ja_JP', 50, 0, '` + salted + `', '10.0.0.1', 0, '0000-00-00 00:00:00', '2021-01-01 00:00:00', 0),
		 (3, 'banned@example.com', 'Bad', NULL, 0, 99, 'not-a-hash', '', -1, '2021-01-01 00:00:00', '2021-01-01 00:00:00', 0)`,
		`INSERT INTO textures (tid, name, type, hash, size, uploader, public, upload_at) VALUES
		 (1, 'Skin A', 'steve', '` + f.skinA + `', 1, 1, 1, '2021-02-01 00:00:00'),
		 (2, 'Skin A again', 'steve', '` + f.skinA + `', 1, 1, 0, '2021-02-02 00:00:00'),
		 (3, 'Bob skin', 'alex', '` + f.skinB + `', 1, 2, 0, '2021-02-03 00:00:00'),
		 (4, 'Cape', 'cape', '` + f.cape + `', 1, 42, 1, '2021-02-04 00:00:00'),
		 (5, 'Lost', 'steve', '` + missing + `', 3, 2, 1, '2021-02-05 00:00:00'),
		 (6, 'Altered', 'steve', '` + f.altered + `', 1, 2, 1, '2021-02-06 00:00:00'),
		 (7, 'Weird', 'elytra', '` + f.skinA + `', 1, 2, 1, '2021-02-07 00:00:00')`,
		`INSERT INTO players (pid, uid, name, tid_skin, tid_cape, last_modified) VALUES
		 (1, 1, 'Steve', 2, 4, '2022-01-01 00:00:00'),
		 (2, 2, 'steve', 3, 0, '2022-01-02 00:00:00'),
		 (3, 2, 'Bob', 99, 1, '2022-01-03 00:00:00'),
		 (4, 77, 'Ghost', -1, 0, '2022-01-04 00:00:00'),
		 (5, 2, 'ThisNameIsWayTooLongForSkinLoom_1234567', -1, 0, '2022-01-05 00:00:00')`,
		`INSERT INTO uuid (id, name, uuid) VALUES (1, 'Steve', '0123456789abcdef0123456789abcdef'), (2, 'Bob', 'FEDCBA98-7654-3210-FEDC-BA9876543210'),
		 (3, 'Deleted', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'), (4, 'steve', '0123456789abcdef0123456789abcdef')`,
		`INSERT INTO user_closet (user_uid, texture_tid, item_name) VALUES (1, 1, 'mine'), (1, 2, NULL), (2, 3, ''), (2, 1, 'liked'), (2, 1, 'again'), (2, 999, 'gone'), (5, 1, 'nobody')`,
		`INSERT INTO reports (id, tid, uploader, reporter, reason, status, report_at) VALUES
		 (1, 4, 42, 2, 'stolen', 0, '2023-01-01 00:00:00'), (2, 4, 42, 2, 'still stolen', 0, '2023-01-02 00:00:00'), (3, 999, 1, 2, '', 1, '2023-01-03 00:00:00')`,
		`INSERT INTO notifications (id, type, notifiable_type, notifiable_id, data, read_at, created_at) VALUES
		 ('a', 'App\Notifications\SiteMessage', 'App\Models\User', 2, '{"title":"Hello","content":"**hi**"}', NULL, '2023-02-01 00:00:00'),
		 ('b', 'App\Notifications\SiteMessage', 'App\Models\User', 2, '{"title":"Old","content":"read"}', '2023-02-02 00:00:00', '2023-02-01 00:00:00')`,
		`INSERT INTO options (option_name, option_value) VALUES ('ygg_uuid_algorithm', 'v3'), ('site_name', 'Old site')`,
	}
	stmts = append(stmts, extra...)
	for _, s := range stmts {
		if _, err := pool.Exec(s); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
	f.src = Source{Driver: "sqlite", DSN: f.sqlitePath, PasswordMethod: "SALTED2MD5", Salt: "pepper", TexturesDir: texDir, Location: shanghai}
	return f
}

func newTarget(t *testing.T, d *db.DB) (Target, *storage.Local) {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Target{DB: d, Store: store, Locales: []string{"zh-CN", "en"}, Settings: testRegistry(t)}, store
}

func testRegistry(t *testing.T) *settings.Registry {
	t.Helper()
	reg, err := sitesettings.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func entries(r *Report, sev Severity) string {
	var b strings.Builder
	for _, e := range r.Entries {
		if e.Severity == sev {
			b.WriteString(e.Table + " " + e.ID + ": " + e.Message + "\n")
		}
	}
	return b.String()
}

func TestImport(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t)
		before, _ := os.ReadFile(f.sqlitePath)
		tg, store := newTarget(t, d)
		r, err := Run(ctx, f.src, tg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Blocked() {
			t.Fatalf("blocked:\n%s", entries(r, Blocking))
		}
		if after, _ := os.ReadFile(f.sqlitePath); !bytes.Equal(before, after) {
			t.Error("the BS database file changed")
		}
		c := d.Ent

		// Accounts: roles, normalised email, filled-in nickname, passwords that verify.
		admin := c.User.GetX(ctx, 1)
		if admin.Role != "superadmin" || admin.EmailNormalized != "admin@example.com" || admin.Email != "Admin@Example.com" || admin.Locale != "zh-CN" || admin.EmailVerifiedAt == nil {
			t.Errorf("admin = %+v", admin)
		}
		if want := time.Date(2020, 5, 6, 7, 8, 9, 0, shanghai); !admin.CreatedAt.Equal(want) {
			t.Errorf("registered at %v, want %v", admin.CreatedAt, want)
		}
		bob := c.User.GetX(ctx, 2)
		if bob.Nickname != "bob" || bob.Locale != "" || bob.LastSignAt != nil || bob.EmailVerifiedAt != nil || bob.Score != 50 {
			t.Errorf("bob = %+v", bob)
		}
		banned := c.User.GetX(ctx, 3)
		if banned.Status != "banned" || banned.AvatarTextureID != nil || banned.PasswordAlgo != "unknown" {
			t.Errorf("banned = %+v", banned)
		}
		h := account.NewHasher(1)
		for _, tc := range []struct {
			u  *ent.User
			pw string
		}{{admin, "bcrypt-password"}, {bob, "salted-password"}} {
			if ok, rehash, err := h.Verify(ctx, tc.u.PasswordAlgo, tc.u.PasswordHash, tc.pw); !ok || !rehash || err != nil {
				t.Errorf("user %d (%s): verify = %v %v %v", tc.u.ID, tc.u.PasswordAlgo, ok, rehash, err)
			}
		}
		if admin.AvatarTextureID == nil || *admin.AvatarTextureID != 1 { // tid 2, merged into 1
			t.Errorf("admin avatar = %v", admin.AvatarTextureID)
		}

		// Textures: tid 2 merged into tid 1 (same user, same file), which became public
		// already; the elytra dropped; the altered file stored under its content hash.
		if n := c.Texture.Query().CountX(ctx); n != 5 {
			t.Errorf("%d textures, want 5", n)
		}
		if _, err := c.Texture.Get(ctx, 2); !ent.IsNotFound(err) {
			t.Errorf("texture 2 should be merged into 1: %v", err)
		}
		t1 := c.Texture.GetX(ctx, 1)
		if t1.Visibility != "public" || t1.Likes != 2 || t1.Size != int64(len(skinPNG(t, 64, 64, 1))) {
			t.Errorf("texture 1 = %+v", t1)
		}
		if t4 := c.Texture.GetX(ctx, 4); t4.UploaderID != nil {
			t.Errorf("texture 4 uploader = %v, want none (user 42 is gone)", *t4.UploaderID)
		}
		if t6 := c.Texture.GetX(ctx, 6); t6.Hash != f.alteredContent {
			t.Errorf("altered texture hash = %s, want the content's %s", t6.Hash, f.alteredContent)
		}
		lost := c.Texture.GetX(ctx, 5)
		if b := c.Blob.GetX(ctx, lost.Hash); b.Width != 0 || b.Size != 3*1024 {
			t.Errorf("missing file blob = %+v", b)
		}
		if b := c.Blob.GetX(ctx, f.skinA); b.RefCount != 1 || b.Width != 64 || b.Height != 64 {
			t.Errorf("blob A = %+v", b)
		}
		for _, k := range []string{f.skinA, f.skinB, f.cape, f.alteredContent} {
			if _, err := store.Stat(ctx, k); err != nil {
				t.Errorf("file %s not copied: %v", k, err)
			}
		}
		if _, err := store.Stat(ctx, f.altered); err == nil {
			t.Error("the altered file was stored under the name that does not match it")
		}

		// Players: "steve" clashes with "Steve" and is renamed, the owner is told; the
		// over-long name is cut; the ghost's owner is gone.
		players := c.Player.Query().Order(ent.Asc(entplayer.FieldID)).AllX(ctx)
		names := map[int64]string{}
		for _, p := range players {
			names[p.ID] = p.Name
		}
		if names[1] != "Steve" || names[2] != "steve_2" || names[3] != "Bob" || len(names[5]) != player.MaxNameLen || names[4] != "" {
			t.Errorf("players = %v", names)
		}
		p1, p2, p3 := c.Player.GetX(ctx, 1), c.Player.GetX(ctx, 2), c.Player.GetX(ctx, 3)
		if p1.UUID != "0123456789abcdef0123456789abcdef" || p3.UUID != "fedcba9876543210fedcba9876543210" {
			t.Errorf("uuids = %s %s", p1.UUID, p3.UUID)
		}
		if p2.UUID != player.OfflineUUID("steve_2") {
			t.Errorf("renamed player uuid = %s, want the offline UUID of its new name", p2.UUID)
		}
		if p1.SkinID == nil || *p1.SkinID != 1 || p1.CapeID == nil || *p1.CapeID != 4 {
			t.Errorf("player 1 skin %v cape %v: tid 2 merged into 1", p1.SkinID, p1.CapeID)
		}
		if p3.SkinID != nil || p3.CapeID != nil {
			t.Errorf("player 3 skin %v cape %v: a gone texture and a skin in the cape slot", p3.SkinID, p3.CapeID)
		}
		if n := c.Notification.Query().Where(notification.UserID(2), notification.TitleContains("Player renamed")).CountX(ctx); n != 2 {
			t.Errorf("%d rename notices to bob, want 2", n)
		}

		// Closet: dangling and duplicate rows dropped, empty names filled.
		items := c.ClosetItem.Query().Where(closetitem.UserID(2)).AllX(ctx)
		if len(items) != 2 {
			t.Errorf("bob's closet = %d items, want 2", len(items))
		}
		for _, it := range items {
			if it.TextureID == 3 && it.Name != "Bob skin" {
				t.Errorf("closet item without a name = %q", it.Name)
			}
		}

		// Reports: the repeat dropped; a gone texture kept as none.
		if n := c.Report.Query().CountX(ctx); n != 2 {
			t.Errorf("%d reports, want 2", n)
		}
		if r3 := c.Report.GetX(ctx, 3); r3.TextureID != nil || r3.Status != "resolved" || r3.Reason != "-" {
			t.Errorf("report 3 = %+v", r3)
		}

		// Only the unread notification.
		if n := c.Notification.Query().Where(notification.UserID(2), notification.Title("Hello")).CountX(ctx); n != 1 {
			t.Errorf("unread notification imported %d times", n)
		}
		if n := c.Notification.Query().Where(notification.Title("Old")).CountX(ctx); n != 0 {
			t.Error("a read notification was imported")
		}

		// The super administrator replaces the setup page.
		if !c.SystemState.Query().Where(systemstate.ID(setupKey)).ExistX(ctx) {
			t.Error("setup not marked complete")
		}

		// New rows get IDs after the imported ones (PostgreSQL sequences).
		u := c.User.Create().SetEmail("new@example.com").SetEmailNormalized("new@example.com").SetNickname("new").SetPasswordHash("x").SaveX(ctx)
		if u.ID <= 3 {
			t.Errorf("new user got id %d", u.ID)
		}

		for sev, want := range map[Severity][]string{
			Warning: {"users 3: password hash in an unknown format", "file missing"},
			Fixed:   {"no nickname", "renamed to \"steve_2\"", "merged into it", "does not match its name", "uploader 42 no longer exists", "wrong slot"},
			Dropped: {"owner 77 no longer exists", "unknown type \"elytra\"", "closet twice", "already reported", "2 rows were not used"},
		} {
			got := entries(r, sev)
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("%s entries lack %q:\n%s", sev, w, got)
				}
			}
		}

		// A second run is refused: the site is no longer empty.
		if _, err := Run(ctx, f.src, tg, Options{}); !errors.Is(err, ErrNotEmpty) {
			t.Errorf("second import: %v, want ErrNotEmpty", err)
		}
	})
}

func TestDryRunWritesNothing(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t)
		tg, store := newTarget(t, d)
		r, err := Run(ctx, f.src, tg, Options{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if r.Imported["users"] != 3 || r.Imported["textures"] != 5 || r.FilesCopied != 0 {
			t.Errorf("report: %+v", r)
		}
		if d.Ent.User.Query().ExistX(ctx) || d.Ent.Blob.Query().ExistX(ctx) {
			t.Error("the dry run wrote rows")
		}
		n := 0
		_ = store.Walk(ctx, func(string, time.Time) error { n++; return nil })
		if n != 0 {
			t.Errorf("the dry run copied %d files", n)
		}
		// ...and the real import still runs afterwards.
		if _, err := Run(ctx, f.src, tg, Options{}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDuplicateEmailsBlock(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, `INSERT INTO users (uid, email, score, password, ip, last_sign_at, register_at) VALUES (9, 'ADMIN@example.com ', 0, '', '', '2021-01-01', '2021-01-01')`)
		tg, _ := newTarget(t, d)
		r, err := Run(ctx, f.src, tg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Blocked() || !strings.Contains(entries(r, Blocking), "users 9: same email address as user 1") {
			t.Fatalf("not blocked:\n%s", entries(r, Blocking))
		}
		if d.Ent.User.Query().ExistX(ctx) {
			t.Error("a refused import wrote rows")
		}
		var out bytes.Buffer
		_ = r.WriteText(&out)
		if !strings.HasPrefix(out.String(), "Import refused") {
			t.Errorf("report starts %q", out.String()[:40])
		}
	})
}

func TestNonEmptySiteRefused(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		d.Ent.User.Create().SetEmail("a@b.c").SetEmailNormalized("a@b.c").SetNickname("a").SetPasswordHash("x").ExecX(ctx)
		f := newFixture(t)
		tg, _ := newTarget(t, d)
		if _, err := Run(ctx, f.src, tg, Options{}); !errors.Is(err, ErrNotEmpty) {
			t.Fatalf("err = %v, want ErrNotEmpty", err)
		}
	})
}

// TestConcurrentImports: two imports started together write the site once. The second
// waits on the first's lock row, then finds the site taken (ADR 0026: every new
// transaction gets a concurrency test).
func TestConcurrentImports(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t)
		errs := make(chan error, 2)
		for range 2 {
			tg, _ := newTarget(t, d)
			go func() {
				_, err := Run(ctx, f.src, tg, Options{})
				errs <- err
			}()
		}
		var ok, refused int
		for range 2 {
			switch err := <-errs; {
			case err == nil:
				ok++
			case errors.Is(err, ErrNotEmpty):
				refused++
			default:
				t.Errorf("import: %v", err)
			}
		}
		if ok != 1 || refused != 1 {
			t.Fatalf("%d imports succeeded and %d were refused, want 1 and 1", ok, refused)
		}
		if n := d.Ent.User.Query().CountX(ctx); n != 3 {
			t.Errorf("%d users, want 3", n)
		}
		if left, _ := d.Ent.SystemState.Query().Where(systemstate.ID(importLock)).Exist(ctx); left {
			t.Error("the import lock row was committed")
		}
	})
}
