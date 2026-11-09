package bs

// Review tests (M5 part 1): each one failed on the code as first reviewed; the comment
// above it describes that bug, and the test now keeps it fixed.

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// sqliteTarget is a migrated, empty SkinLoom SQLite database.
func sqliteTarget(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, db.Options{Driver: "sqlite", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := migrate.Up(ctx, d, dir, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	return d
}

// The password-algorithm-transition plugin (blessing-skin-plugins/plugins/
// password-algorithm-transition) is configured with PASSWORD_METHODS=<new>,<old>... and
// SALT=<salt of new>,<salt of old>... (one salt per method, comma separated). The
// importer ignores PASSWORD_METHODS and uses SALT as one string, so every legacy salted
// digest is stored as unsalted (or with the whole comma list as its salt) and those
// users can no longer sign in, without a correct report entry.
func TestReviewTransitionPluginSaltedLegacyPasswords(t *testing.T) {
	for _, tc := range []struct{ name, env string }{
		// The plugin README's own example: moving to BCRYPT from SALTED2MD5.
		{"to bcrypt", "PWD_METHOD=BCRYPT\nPASSWORD_METHODS=BCRYPT,SALTED2MD5\nSALT=,pepper\n"},
		{"to another salted method", "PWD_METHOD=SALTED2SHA256\nPASSWORD_METHODS=SALTED2SHA256,SALTED2MD5\nSALT=newsalt,pepper\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t) // bob's password is md5(md5("salted-password") . "pepper")
			// PASSWORD_METHODS only counts while the plugin is enabled.
			pool, err := sql.Open("sqlite", f.sqlitePath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = pool.Exec(`INSERT INTO options (option_name, option_value) VALUES ('plugins_enabled', '[{"name":"password-algorithm-transition","version":"1.0.0"}]')`)
			pool.Close()
			if err != nil {
				t.Fatal(err)
			}
			envPath := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(envPath, []byte("DB_CONNECTION=sqlite\nDB_DATABASE="+f.sqlitePath+"\n"+tc.env), 0o600); err != nil {
				t.Fatal(err)
			}
			src, err := FromEnv(envPath)
			if err != nil {
				t.Fatal(err)
			}
			src.TexturesDir = f.src.TexturesDir
			d := sqliteTarget(t)
			tg, _ := newTarget(t, d)
			r, err := Run(ctx, src, tg, Options{})
			if err != nil || r.Blocked() {
				t.Fatalf("run: %v %v", err, r)
			}
			bob := d.Ent.User.GetX(ctx, 2)
			ok, _, verr := account.NewHasher(1).Verify(ctx, bob.PasswordAlgo, bob.PasswordHash, "salted-password")
			if !ok {
				t.Errorf("bob's legacy SALTED2MD5 password no longer verifies: stored as %s %q (%v)", bob.PasswordAlgo, bob.PasswordHash, verr)
			}
		})
	}
}

// When SkinLoom's texture directory is the BS directory (the documented bind mount),
// files are not copied. A file whose content does not match its name is then recorded
// under its content hash, but the file on disk still has the BS name: the texture 404s
// at once, and the orphan sweep later deletes the BS-named file (no blob row names it)
// out of the BS directory, which also breaks the documented rollback to BS.
func TestReviewSharedTextureDirAlteredFile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	d := sqliteTarget(t)
	store, err := storage.NewLocal(f.src.TexturesDir)
	if err != nil {
		t.Fatal(err)
	}
	tg := Target{DB: d, Store: store, StoreDir: f.src.TexturesDir, Locales: []string{"zh-CN", "en"}, Settings: testRegistry(t)}
	if _, err := Run(ctx, f.src, tg, Options{}); err != nil {
		t.Fatal(err)
	}
	t6 := d.Ent.Texture.GetX(ctx, 6)
	if _, err := store.Stat(ctx, t6.Hash); err != nil {
		t.Errorf("texture 6 points at blob %s, which has no file in the shared directory: %v", t6.Hash, err)
	}
	if _, err := d.Ent.Blob.Get(ctx, f.altered); err != nil {
		t.Logf("the BS file %s has no blob row, so SweepOrphans will delete it from the BS directory", f.altered)
	}
}

// 0035: "先按名字精确匹配，再不区分大小写". Players are matched one by one in pid order,
// so an earlier player that only matches case-insensitively takes the UUID of a later
// player that matches exactly. Below, "Steve" (pid 2) is the one the Yggdrasil plugin
// gave the UUID to (on PostgreSQL/SQLite its lookups are exact), yet "steve" (pid 1)
// gets it and Steve is given a new UUID: his game data (inventory, claims) is lost.
func TestReviewUUIDExactMatchBeatsEarlierFoldedMatch(t *testing.T) {
	const steves = "0123456789abcdef0123456789abcdef"
	d := &sourceData{
		Users:   []srcUser{{ID: 1, Email: "a@example.com", Nickname: "a", Password: strings.Repeat("0", 32), Permission: 2}},
		Players: []srcPlayer{{ID: 1, UserID: 1, Name: "steve"}, {ID: 2, UserID: 1, Name: "Steve"}},
		UUIDs:   []srcUUID{{ID: 1, Name: "Steve", UUID: steves}},
		Options: map[string]string{},
	}
	p := plan(d, nil, planOptions{Location: shanghai, PasswordMethod: "MD5", Now: time.Now()}, &Report{})
	for _, pl := range p.Players {
		if pl.ID == 2 && pl.UUID != steves {
			t.Errorf("player 2 %q (exact match in the uuid table) got %s, want %s; player 1 took it through the case-insensitive fallback", pl.Name, pl.UUID, steves)
		}
	}
}

// A UUID generated for an earlier player (no uuid row: v3 from its name) wins over a
// later player's own uuid-table row with the same UUID: the later player, who actually
// used that UUID in game, is given a new one. Happens on a site that switched from v3
// to v4 (the rename callback then moves rows): pid 2 "X" logged in under v3 and was
// renamed to "Y" (row becomes Y → v3("X")); pid 1 was later renamed to "X" and never
// logged in. Table UUIDs must be assigned before any is generated.
func TestReviewTableUUIDBeatsEarlierGeneratedUUID(t *testing.T) {
	x := player.OfflineUUID("X")
	d := &sourceData{
		Users:   []srcUser{{ID: 1, Email: "a@example.com", Nickname: "a", Password: strings.Repeat("0", 32), Permission: 2}},
		Players: []srcPlayer{{ID: 1, UserID: 1, Name: "X"}, {ID: 2, UserID: 1, Name: "Y"}},
		UUIDs:   []srcUUID{{ID: 1, Name: "Y", UUID: x}},
		Options: map[string]string{"ygg_uuid_algorithm": "v3"},
	}
	r := &Report{}
	p := plan(d, nil, planOptions{Location: shanghai, PasswordMethod: "MD5", Now: time.Now()}, r)
	for _, pl := range p.Players {
		if pl.ID == 2 && pl.UUID != x {
			t.Errorf("player 2 %q lost its uuid-table UUID %s to player 1's generated one (got %s)\n%s", pl.Name, x, pl.UUID, entries(r, Fixed))
		}
	}
}

// BS rejects nothing at the byte level (Laravel on SQLite stores whatever PHP gives it,
// MySQL keeps NUL bytes in varchar). Text is copied as is, so one invalid UTF-8 sequence
// or NUL byte in any nickname/texture name/closet name/reason/notification aborts the
// whole import on PostgreSQL ("invalid byte sequence for encoding UTF8") and MySQL
// (strict mode "Incorrect string value"); on SQLite it is stored and breaks JSON later.
func TestReviewInvalidTextIsCleaned(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t,
			`UPDATE textures SET name = CAST(X'536B696EFF41' AS TEXT) WHERE tid = 3`,
			`UPDATE users SET nickname = 'Ba' || char(0) || 'd' WHERE uid = 3`)
		tg, _ := newTarget(t, d)
		r, err := Run(ctx, f.src, tg, Options{})
		if err != nil {
			t.Fatalf("import failed on one bad byte: %v", err)
		}
		if r.Blocked() {
			t.Fatal(entries(r, Blocking))
		}
		if name := d.Ent.Texture.GetX(ctx, 3).Name; !utf8.ValidString(name) {
			t.Errorf("texture 3 name stored as invalid UTF-8: %q", name)
		}
		if nick := d.Ent.User.GetX(ctx, 3).Nickname; strings.ContainsRune(nick, 0) {
			t.Errorf("user 3 nickname stored with a NUL byte: %q", nick)
		}
	})
}

// BS reads .env with vlucas/phpdotenv v5.6 and Laravel's env(): an unquoted value ends
// at "#" even without a space before it (EntryParser: '#' in UNQUOTED_STATE starts a
// comment), and the literal null/(null) means null, which config('secure.salt') turns
// into "". ReadEnv only cuts at " #" and keeps "null", so SALT differs from what BS
// hashed with and every salted password silently fails to verify.
func TestReviewEnvMatchesLaravel(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ line, want string }{
		{"SALT=abc#def", "abc"},
		{"SALT=null", ""},
		{"SALT=(empty)", ""},
	} {
		p := filepath.Join(dir, ".env")
		if err := os.WriteFile(p, []byte("DB_CONNECTION=sqlite\n"+tc.line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := FromEnv(p)
		if err != nil {
			t.Fatal(err)
		}
		if s.Salt != tc.want {
			t.Errorf("%s: salt %q, BS uses %q", tc.line, s.Salt, tc.want)
		}
	}
}

// The probe for optional tables and columns swallows every error. With the read-only
// account the docs recommend, a missing GRANT on user_closet (or reports, uuid...) is
// reported as "table not found (fine if the site never had it)" and every closet is
// silently dropped; a failing probe for users.verified silently un-verifies everyone.
// Only "table does not exist" should mean missing; any other error must stop the import.
func TestReviewProbeErrorIsNotMissingTable(t *testing.T) {
	dsn := os.Getenv("SKINLOOM_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("SKINLOOM_TEST_POSTGRES not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	tables := []string{"bsr_users", "bsr_players", "bsr_textures", "bsr_options", "bsr_user_closet"}
	cleanup := func() {
		for _, tb := range tables {
			_, _ = admin.Exec("DROP TABLE IF EXISTS " + tb)
		}
		_, _ = admin.Exec("DROP OWNED BY bs_review_reader")
		_, _ = admin.Exec("DROP ROLE IF EXISTS bs_review_reader")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := admin.Exec("CREATE ROLE bs_review_reader LOGIN"); err != nil {
		t.Skipf("cannot create a role here: %v", err)
	}
	for _, s := range []string{
		`CREATE TABLE bsr_users (uid serial primary key, email varchar(100) not null, nickname varchar(50) not null default '', score integer not null,
		  avatar integer not null default 0, password varchar(255) not null, ip varchar(45) not null, permission integer not null default 0,
		  last_sign_at timestamp(0) not null, register_at timestamp(0) not null, verified boolean not null default false)`,
		`CREATE TABLE bsr_players (pid serial primary key, uid integer not null, name varchar(50) not null, tid_cape integer not null default 0,
		  last_modified timestamp(0) not null, tid_skin integer not null default -1)`,
		`CREATE TABLE bsr_textures (tid serial primary key, name varchar(50) not null, type varchar(10) not null, hash varchar(64) not null, size integer not null,
		  uploader integer not null, public smallint not null, upload_at timestamp(0) not null)`,
		`CREATE TABLE bsr_options (id serial primary key, option_name varchar(50) not null, option_value text not null)`,
		`CREATE TABLE bsr_user_closet (user_uid integer not null, texture_tid integer not null, item_name text null)`,
		`INSERT INTO bsr_users (uid, email, nickname, score, password, ip, permission, last_sign_at, register_at, verified) VALUES
		  (1, 'root@example.com', 'root', 0, '` + strings.Repeat("0", 32) + `', '', 2, '2024-01-01 00:00:00', '2020-01-01 00:00:00', true)`,
		`INSERT INTO bsr_textures (tid, name, type, hash, size, uploader, public, upload_at) VALUES (1, 's', 'steve', '` + strings.Repeat("ab", 32) + `', 1, 1, 1, '2020-01-01 00:00:00')`,
		`INSERT INTO bsr_user_closet VALUES (1, 1, 'mine')`,
		"GRANT USAGE ON SCHEMA public TO bs_review_reader",
		// The operator forgot one table.
		"GRANT SELECT ON bsr_users, bsr_players, bsr_textures, bsr_options TO bs_review_reader",
	} {
		if _, err := admin.Exec(s); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
	readerDSN := strings.Replace(dsn, "postgres://postgres@", "postgres://bs_review_reader@", 1)
	if readerDSN == dsn {
		t.Skip("cannot derive a DSN for the reader role from SKINLOOM_TEST_POSTGRES")
	}
	d := sqliteTarget(t)
	tg, _ := newTarget(t, d)
	src := Source{Driver: "postgres", DSN: readerDSN, Prefix: "bsr_", PasswordMethod: "MD5", TexturesDir: t.TempDir(), Location: shanghai}
	r, err := Run(ctx, src, tg, Options{DryRun: true})
	if err == nil {
		t.Errorf("permission denied on bsr_user_closet was taken for a missing table; the import went on without closets:\n%s", entries(r, Warning))
	}
}

// Player names are trimmed before the uuid-table lookup, but the table is keyed by the
// untrimmed name: a player "Steve " (BS's utf8 rule is /.*/) silently gets a new UUID,
// and the rename (trimming) is neither reported nor notified.
func TestReviewTrimmedPlayerNameKeepsUUID(t *testing.T) {
	const u = "0123456789abcdef0123456789abcdef"
	d := &sourceData{
		Users:   []srcUser{{ID: 1, Email: "a@example.com", Nickname: "a", Password: strings.Repeat("0", 32), Permission: 2}},
		Players: []srcPlayer{{ID: 1, UserID: 1, Name: "Steve "}},
		UUIDs:   []srcUUID{{ID: 1, Name: "Steve ", UUID: u}},
		Options: map[string]string{},
	}
	r := &Report{}
	p := plan(d, nil, planOptions{Location: shanghai, PasswordMethod: "MD5", Now: time.Now()}, r)
	if len(p.Players) != 1 || p.Players[0].UUID != u {
		t.Errorf("player %q lost its UUID %s: %+v", "Steve ", u, p.Players)
	}
}
