package bs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromEnvBytesAndRedaction(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	envBytes := []byte(`
DB_CONNECTION=mysql
DB_HOST=127.0.0.1
DB_PORT=3306
DB_DATABASE=bs_prod
DB_USERNAME=bsuser
DB_PASSWORD=super_secret_db_pw
DB_PREFIX=bs_
PWD_METHOD=SALTED2MD5
PASSWORD_METHODS=BCRYPT, SALTED2MD5
SALT="first_salt, second_salt"
FS_DRIVER=s3
MAIL_MAILER=smtp
MAIL_HOST=smtp.example.com
MAIL_PORT=587
MAIL_USERNAME=postmaster@example.com
MAIL_PASSWORD=super_secret_mail_pw
`)
	src, info, err := FromEnvBytes(envBytes)
	if err != nil {
		t.Fatalf("FromEnvBytes: %v", err)
	}
	if src.Driver != "mysql" || src.Prefix != "bs_" || src.PasswordMethod != "SALTED2MD5" {
		t.Fatalf("unexpected source: %+v", src)
	}
	if len(src.Transition) != 2 || src.Transition[0].Salt != "first_salt" || src.Transition[1].Salt != "second_salt" {
		t.Fatalf("unexpected transition: %+v", src.Transition)
	}
	if src.Mail == nil || !src.Mail.HostSet || src.Mail.Password != "super_secret_mail_pw" {
		t.Fatalf("unexpected mail: %+v", src.Mail)
	}
	if !info.LocalhostWarning || !info.HasSalt || !info.HasMail || info.FSDriver != "s3" {
		t.Fatalf("unexpected env info: %+v", info)
	}
	rawJSON, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}
	for _, secret := range []string{"super_secret_db_pw", "super_secret_mail_pw", "first_salt", "second_salt"} {
		if strings.Contains(string(rawJSON), secret) {
			t.Fatalf("EnvInfo JSON leaked secret %q: %s", secret, rawJSON)
		}
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	rep := Report{
		DryRun: true,
		Entries: []Entry{
			{Severity: Blocking, Table: "users", ID: "1", Message: "dup email"},
			{Severity: Warning, Table: "bs_extra", Message: "stays in bs", LeftBehind: true},
		},
		Imported:    map[string]int{"users": 5},
		Files:       3,
		FilesCopied: 2,
		Superadmins: 1,
		Failed:      errors.New("copy failed"),
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var got struct {
		DryRun  bool           `json:"dryRun"`
		Entries []Entry        `json:"entries"`
		Failed  string         `json:"failed"`
		Counts  map[string]int `json:"imported"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if !got.DryRun || len(got.Entries) != 2 || got.Entries[0].Severity != Blocking || got.Failed != "copy failed" || got.Counts["users"] != 5 {
		t.Fatalf("unexpected JSON round-trip: %+v", got)
	}
}

func TestDetectTexturesRoot(t *testing.T) {
	hexName := strings.Repeat("a", 64)
	cases := []struct {
		name    string
		relFile string
		wantSub string
	}{
		{name: "flat", relFile: hexName, wantSub: ""},
		{name: "textures", relFile: filepath.Join("textures", hexName), wantSub: "textures"},
		{name: "storage_textures", relFile: filepath.Join("storage", "textures", hexName), wantSub: filepath.Join("storage", "textures")},
		{name: "wrapped", relFile: filepath.Join("backup-2026", "textures", hexName), wantSub: filepath.Join("backup-2026", "textures")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, tc.relFile)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
				t.Fatal(err)
			}
			want := root
			if tc.wantSub != "" {
				want = filepath.Join(root, tc.wantSub)
			}
			if got := DetectTexturesRoot(root); got != want {
				t.Fatalf("DetectTexturesRoot() = %q, want %q", got, want)
			}
		})
	}
}

func TestSourceSummaryReadOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bs.db")
	texDir := filepath.Join(dir, "textures")
	if err := os.MkdirAll(texDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(texDir, hash), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	pool, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE bs_users (uid INTEGER PRIMARY KEY)`,
		`INSERT INTO bs_users VALUES (1), (2)`,
		`CREATE TABLE bs_players (pid INTEGER PRIMARY KEY, uid INTEGER, name TEXT, tid_skin INTEGER, tid_cape INTEGER)`,
		`INSERT INTO bs_players VALUES (1, 1, 'Alex', 1, 0)`,
		`CREATE TABLE bs_textures (tid INTEGER PRIMARY KEY)`,
		`INSERT INTO bs_textures VALUES (1)`,
		`CREATE TABLE bs_user_closet (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE bs_options (option_name TEXT, option_value TEXT)`,
		`INSERT INTO bs_options VALUES ('ygg_private_key', '---KEY---')`,
	} {
		if _, err := pool.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()

	sum, err := TestSource(ctx, Source{
		Driver:      "sqlite",
		DSN:         dbPath,
		Prefix:      "bs_",
		TexturesDir: texDir,
	})
	if err != nil {
		t.Fatalf("TestSource: %v", err)
	}
	if sum.Users != 2 || sum.Players != 1 || sum.Textures != 1 || !sum.HasYggdrasilKey || !sum.TextureDirStats.Exists || sum.TextureDirStats.Files != 1 {
		t.Fatalf("unexpected SourceSummary: %+v", sum)
	}
}
