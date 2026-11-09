package bs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
)

func TestReadEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "\ufeff# comment\n" +
		"APP_ENV=production\n" +
		"DB_PASSWORD=pa$word #not part of it\n" +
		"SALT='lit${ERAL} # kept'\n" +
		"MAIL_FROM_NAME=\"Blessing \\\"Skin\\\"\"\n" +
		"export DB_HOST=db.local\n" +
		"DB_URL=\"${DB_HOST}:3306\"\n" +
		"EMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := ReadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"APP_ENV": "production", "DB_PASSWORD": "pa$word", "SALT": "lit${ERAL} # kept",
		"MAIL_FROM_NAME": `Blessing "Skin"`, "DB_HOST": "db.local", "DB_URL": "db.local:3306", "EMPTY": "",
	} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
	if err := os.WriteFile(path, []byte("SALT=\"open\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEnv(path); err == nil {
		t.Error("an unterminated quote was accepted")
	}
}

func TestFromEnv(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, ".env")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	s, err := FromEnv(write("DB_CONNECTION=mysql\nDB_HOST=db\nDB_DATABASE=bs\nDB_USERNAME=u\nDB_PASSWORD=p@ss/w:rd\nDB_PREFIX=bs_\nPWD_METHOD=SALTED2SHA256\nSALT=x\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(s.DSN)
	if err != nil || cfg.Passwd != "p@ss/w:rd" || cfg.Addr != "db:3306" || cfg.DBName != "bs" || cfg.User != "u" {
		t.Errorf("mysql dsn %q: %+v %v", s.DSN, cfg, err)
	}
	if s.Prefix != "bs_" || s.PasswordMethod != "SALTED2SHA256" || s.Salt != "x" || s.TexturesDir != filepath.Join(dir, "storage", "textures") {
		t.Errorf("source = %+v", s)
	}

	s, err = FromEnv(write("DB_CONNECTION=pgsql\nDB_DATABASE=bs\nDB_USERNAME=u\nDB_PASSWORD=p@ss\nTEXTURES_DIR=/srv/tex\n"))
	if err != nil || s.Driver != "postgres" || s.DSN != "postgres://u:p%40ss@127.0.0.1:5432/bs" || s.TexturesDir != filepath.FromSlash("/srv/tex") && !strings.HasSuffix(s.TexturesDir, "tex") {
		t.Errorf("pgsql: %+v %v", s, err)
	}

	s, err = FromEnv(write("DB_CONNECTION=sqlite\nFS_DRIVER=oss\n"))
	if err != nil || s.Driver != "sqlite" || s.DSN != filepath.Join(dir, "database", "database.sqlite") || s.TexturesDir != "" {
		t.Errorf("sqlite: %+v %v", s, err)
	}

	if _, err := FromEnv(write("DB_CONNECTION=sqlsrv\n")); err == nil {
		t.Error("sqlsrv accepted")
	}
}

func TestPasswordOf(t *testing.T) {
	md5 := strings.Repeat("ab", 16)
	sha := strings.Repeat("cd", 32)
	for _, tc := range []struct {
		stored, method, salt string
		algo, hash           string
		problem              bool
	}{
		{"$2y$10$abcdefghijklmnopqrstuuvwxyzabcdefghijklmnopqrstuvwxyz1", "", "", account.AlgoBcrypt, "", false},
		{"$argon2i$v=19$m=1024,t=2,p=2$c2FsdA$aGFzaA", "PHP_PASSWORD_HASH", "", account.AlgoArgon2i, "", false},
		{"$argon2id$v=19$m=1024,t=2,p=2$c2FsdA$aGFzaA", "BCRYPT", "", account.AlgoArgon2id, "", false},
		{md5, "MD5", "", account.AlgoMD5, md5, false},
		{md5, "salted2md5", "s", account.AlgoSalted2MD5, md5 + ":s", false},
		{sha, "SALTED2SHA256", "", account.AlgoSalted2SHA256, sha + ":", false},
		{sha, "BCRYPT", "", account.AlgoSHA256, sha, true}, // a transition left a digest behind
		{md5, "SALTED2SHA512", "s", account.AlgoMD5, md5, true},
		{"plaintext", "MD5", "", "unknown", "plaintext", true},
		{"", "BCRYPT", "", "unknown", "", true},
	} {
		algo, hash, problem := passwordOf(tc.stored, []Method{{tc.method, tc.salt}})
		if tc.hash == "" && tc.stored != "" {
			tc.hash = tc.stored
		}
		if algo != tc.algo || hash != tc.hash || (problem != "") != tc.problem {
			t.Errorf("passwordOf(%q, %q) = %q, %q, %q", tc.stored, tc.method, algo, hash, problem)
		}
	}
}

// TestPasswordOfTransition: with the password-algorithm-transition plugin a site has
// digests of several methods, each with its own salt.
func TestPasswordOfTransition(t *testing.T) {
	md5 := strings.Repeat("ab", 16)
	sha := strings.Repeat("cd", 32)
	methods := []Method{{"SALTED2SHA256", "new"}, {"MD5", ""}}
	for _, tc := range []struct {
		stored     string
		methods    []Method
		algo, hash string
		problem    bool
	}{
		{md5, methods, account.AlgoMD5, md5, false},
		{sha, methods, account.AlgoSalted2SHA256, sha + ":new", false},
		{"$2y$10$abcdefghijklmnopqrstuuvwxyzabcdefghijklmnopqrstuvwxyz1", methods, account.AlgoBcrypt, "", false},
		// Two methods could have made the digest: the first (the newer) is taken, and said.
		{md5, []Method{{"SALTED2MD5", "a"}, {"MD5", ""}}, account.AlgoSalted2MD5, md5 + ":a", true},
		{strings.Repeat("ef", 64), methods, account.AlgoSHA512, strings.Repeat("ef", 64), true},
	} {
		algo, hash, problem := passwordOf(tc.stored, tc.methods)
		if tc.hash == "" {
			tc.hash = tc.stored
		}
		if algo != tc.algo || hash != tc.hash || (problem != "") != tc.problem {
			t.Errorf("passwordOf(%q, %v) = %q, %q, %q", tc.stored, tc.methods, algo, hash, problem)
		}
	}
}

func TestPluginEnabled(t *testing.T) {
	for opt, want := range map[string]bool{
		`[{"name":"password-algorithm-transition","version":"1.0.0"}]`:        true,
		`[{"name":"yggdrasil-api"},{"name":"password-algorithm-transition"}]`: true,
		`[{"name":"yggdrasil-api"}]`:                                          false,
		``:                                                                    false,
		`not json`:                                                            false,
	} {
		if got := pluginEnabled(opt, "password-algorithm-transition"); got != want {
			t.Errorf("pluginEnabled(%q) = %v", opt, got)
		}
	}
}

func TestWallScan(t *testing.T) {
	want := time.Date(2021, 3, 4, 5, 6, 7, 0, shanghai)
	for _, v := range []any{"2021-03-04 05:06:07", []byte("2021-03-04 05:06:07"), time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC), "2021-03-04T05:06:07Z"} {
		var w wall
		if err := w.Scan(v); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		if got, ok := w.in(shanghai); !ok || !got.Equal(want) {
			t.Errorf("%v → %v, want %v", v, got, want)
		}
	}
	for _, v := range []any{nil, "", "0000-00-00 00:00:00", []byte("0000-00-00")} {
		var w wall
		if err := w.Scan(v); err != nil || w.Valid {
			t.Errorf("%v: valid %v, err %v", v, w.Valid, err)
		}
	}
	var w wall
	if err := w.Scan("yesterday"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestFreeName(t *testing.T) {
	taken := map[string]int64{"steve": 1, "steve_2": 2}
	if got := freeName("STEVE", taken); got != "STEVE_3" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("名", 32)
	taken[strings.ToLower(long)] = 3
	if got := freeName(long, taken); got != strings.Repeat("名", 30)+"_2" {
		t.Errorf("long name → %q", got)
	}
	if got := mdEscape("**x_y**"); got != `\*\*x\_y\*\*` {
		t.Errorf("mdEscape = %q", got)
	}
}
