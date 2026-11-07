package bs

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

// A BS site on PostgreSQL or MySQL returns datetimes and booleans differently from
// SQLite (timestamps as time.Time, booleans as bool or tinyint). These tests build a
// small BS schema with the bs_ prefix next to SkinLoom's tables in the test database
// CI provides, as Laravel creates it on that database.
func TestServerSources(t *testing.T) {
	type dialect struct {
		driver, env, dsnForSource string
		ddl                       []string
	}
	var dialects []dialect
	if dsn := os.Getenv("SKINLOOM_TEST_POSTGRES"); dsn != "" {
		dialects = append(dialects, dialect{"postgres", dsn, dsn, []string{
			`CREATE TABLE bs_users (uid serial primary key, email varchar(100) not null, nickname varchar(50) not null default '', locale varchar(255) null,
			  score integer not null, avatar integer not null default 0, password varchar(255) not null, ip varchar(45) not null, is_dark_mode boolean not null default false,
			  permission integer not null default 0, last_sign_at timestamp(0) without time zone not null, register_at timestamp(0) without time zone not null,
			  verified boolean not null default false, verification_token varchar(255) not null default '')`,
			`CREATE TABLE bs_players (pid serial primary key, uid integer not null, name varchar(50) not null, tid_cape integer not null default 0,
			  last_modified timestamp(0) without time zone not null, tid_skin integer not null default -1)`,
			`CREATE TABLE bs_textures (tid serial primary key, name varchar(50) not null, type varchar(10) not null, hash varchar(64) not null, size integer not null,
			  uploader integer not null, public smallint not null, upload_at timestamp(0) without time zone not null, likes integer not null default 0)`,
			`CREATE TABLE bs_options (id serial primary key, option_name varchar(50) not null, option_value text not null)`,
			`CREATE TABLE bs_language_lines (id serial primary key, "group" varchar(255) not null, "key" varchar(255) not null, text text not null,
			  created_at timestamp(0) without time zone null, updated_at timestamp(0) without time zone null)`,
			`CREATE TABLE bs_oauth_clients (id bigserial primary key, user_id bigint null, name varchar(255) not null, secret varchar(100) null,
			  provider varchar(255) null, redirect text not null, personal_access_client boolean not null, password_client boolean not null,
			  revoked boolean not null, created_at timestamp(0) without time zone null, updated_at timestamp(0) without time zone null)`,
			`INSERT INTO bs_language_lines ("group", "key", text) VALUES ('general', 'skinlib', '{"zh_CN":"材质库"}')`,
			`INSERT INTO bs_oauth_clients (id, user_id, name, secret, redirect, personal_access_client, password_client, revoked, created_at)
			  VALUES (9, 1, 'App', 'sec', 'https://a.example/cb', false, false, false, '2022-02-02 02:02:02')`,
		}})
	}
	if dsn := os.Getenv("SKINLOOM_TEST_MYSQL"); dsn != "" {
		dialects = append(dialects, dialect{"mysql", dsn, dsn, []string{
			"CREATE TABLE bs_users (uid int unsigned auto_increment primary key, email varchar(100) not null, nickname varchar(50) not null default '', locale varchar(255) null," +
				" score int not null, avatar int not null default 0, password varchar(255) not null, ip varchar(45) not null, is_dark_mode tinyint(1) not null default 0," +
				" permission int not null default 0, last_sign_at datetime not null, register_at datetime not null, verified tinyint(1) not null default 0," +
				" verification_token varchar(255) not null default '') DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
			"CREATE TABLE bs_players (pid int unsigned auto_increment primary key, uid int not null, name varchar(50) not null, tid_cape int not null default 0," +
				" last_modified datetime not null, tid_skin int not null default -1) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
			"CREATE TABLE bs_textures (tid int unsigned auto_increment primary key, name varchar(50) not null, type varchar(10) not null, hash varchar(64) not null," +
				" size int not null, uploader int not null, public tinyint not null, upload_at datetime not null, likes int unsigned not null default 0) DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE bs_options (id int unsigned auto_increment primary key, option_name varchar(50) not null, option_value longtext not null) DEFAULT CHARSET=utf8mb4",
			"CREATE TABLE bs_language_lines (id int unsigned auto_increment primary key, `group` varchar(255) not null, `key` varchar(255) not null, text text not null," +
				" created_at timestamp null, updated_at timestamp null) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
			"CREATE TABLE bs_oauth_clients (id bigint unsigned auto_increment primary key, user_id bigint unsigned null, name varchar(255) not null, secret varchar(100) null," +
				" provider varchar(255) null, redirect text not null, personal_access_client tinyint(1) not null, password_client tinyint(1) not null," +
				" revoked tinyint(1) not null, created_at timestamp null, updated_at timestamp null) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
			"INSERT INTO bs_language_lines (`group`, `key`, text) VALUES ('general', 'skinlib', '{\"zh_CN\":\"材质库\"}')",
			"INSERT INTO bs_oauth_clients (id, user_id, name, secret, redirect, personal_access_client, password_client, revoked, created_at)" +
				" VALUES (9, 1, 'App', 'sec', 'https://a.example/cb', 0, 0, 0, '2022-02-02 02:02:02')",
		}})
	}
	if len(dialects) == 0 {
		t.Skip("SKINLOOM_TEST_POSTGRES / SKINLOOM_TEST_MYSQL not set")
	}
	for _, dl := range dialects {
		t.Run(dl.driver, func(t *testing.T) {
			ctx := context.Background()
			driverName := map[string]string{"postgres": "pgx", "mysql": "mysql"}[dl.driver]
			pool, err := sql.Open(driverName, dl.env)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			drop := func() {
				for _, tb := range []string{"bs_users", "bs_players", "bs_textures", "bs_options", "bs_language_lines", "bs_oauth_clients", "bs_plugin_data"} {
					_, _ = pool.Exec("DROP TABLE IF EXISTS " + tb)
				}
			}
			drop()
			t.Cleanup(drop)
			hash := strings.Repeat("ab", 32)
			for _, s := range append(dl.ddl,
				`INSERT INTO bs_users (uid, email, nickname, locale, score, password, ip, permission, last_sign_at, register_at, verified) VALUES
				 (1, 'Root@Example.com', '管理员', 'en', 7, '`+strings.Repeat("0", 32)+`', '::1', 2, '2024-06-01 12:00:00', '2019-01-02 03:04:05', true)`,
				`INSERT INTO bs_textures (tid, name, type, hash, size, uploader, public, upload_at) VALUES (1, '皮肤', 'alex', '`+hash+`', 2, 1, 1, '2020-01-01 08:00:00')`,
				`INSERT INTO bs_players (pid, uid, name, tid_skin, tid_cape, last_modified) VALUES (1, 1, 'Root', 1, 0, '2021-01-01 00:00:00')`,
				`INSERT INTO bs_options (option_name, option_value) VALUES ('ygg_uuid_algorithm', 'v4')`,
				// A plugin's table: listed from the server's catalog as left behind.
				`CREATE TABLE bs_plugin_data (id integer primary key)`,
				`INSERT INTO bs_plugin_data (id) VALUES (1), (2)`,
			) {
				if dl.driver == "mysql" {
					s = strings.ReplaceAll(s, "true)", "1)")
				}
				if _, err := pool.Exec(s); err != nil {
					t.Fatalf("%v\n%s", err, s)
				}
			}

			// The SkinLoom side is SQLite: only the reading differs.
			dir := t.TempDir()
			target, err := db.Open(ctx, db.Options{Driver: "sqlite", DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			if err := migrate.Up(ctx, target, dir, slog.New(slog.DiscardHandler)); err != nil {
				t.Fatal(err)
			}
			tg, _ := newTarget(t, target)
			src := Source{Driver: dl.driver, DSN: dl.dsnForSource, Prefix: "bs_", PasswordMethod: "MD5", TexturesDir: t.TempDir(), Location: shanghai}
			r, err := Run(ctx, src, tg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Blocked() {
				t.Fatal(entries(r, Blocking))
			}
			u := target.Ent.User.GetX(ctx, 1)
			if want := time.Date(2019, 1, 2, 3, 4, 5, 0, shanghai); !u.CreatedAt.Equal(want) || u.EmailVerifiedAt == nil || u.Role != "superadmin" || u.Nickname != "管理员" || u.PasswordAlgo != "md5" || u.Locale != "en" {
				t.Errorf("user = %+v (created %v)", u, u.CreatedAt)
			}
			if u.LastSignAt == nil || !u.LastSignAt.Equal(time.Date(2024, 6, 1, 12, 0, 0, 0, shanghai)) {
				t.Errorf("last sign = %v", u.LastSignAt)
			}
			tex := target.Ent.Texture.GetX(ctx, 1)
			if tex.Visibility != "public" || tex.Name != "皮肤" || tex.Size != 2048 {
				t.Errorf("texture = %+v", tex)
			}
			p := target.Ent.Player.GetX(ctx, 1)
			if p.SkinID == nil || p.UUID == "" {
				t.Errorf("player = %+v", p)
			}
			if c, err := target.Ent.OAuthClient.Get(ctx, 9); err != nil || c.Name != "App" || !c.CreatedAt.Equal(time.Date(2022, 2, 2, 2, 2, 2, 0, shanghai)) {
				t.Errorf("client = %+v, %v", c, err)
			}
			if ov, err := target.Ent.Setting.Get(ctx, "i18n.overrides"); err != nil || !strings.Contains(ov.Value, "材质库") {
				t.Errorf("overrides = %+v, %v", ov, err)
			}
			if !strings.Contains(entries(r, Warning), "bs_user_closet : table not found") {
				t.Errorf("missing tables not reported:\n%s", entries(r, Warning))
			}
			left := r.LeftBehind()
			if len(left) != 1 || left[0].Table != "bs_plugin_data" || !strings.HasPrefix(left[0].Message, "2 rows: ") {
				t.Errorf("left behind: %+v", left)
			}
		})
	}
}
