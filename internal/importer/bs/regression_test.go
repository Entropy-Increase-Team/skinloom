package bs

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// TestRegressionReport (design 02 §6 item 4): the dirty site of newFixture, with
// settings, reworded texts and applications, imported into SQLite; the whole report is
// compared with testdata/regression-report.golden. Any change to what the importer
// does with a BS site shows up here as a diff to review (go test -update to accept).
//
// The design asks for an anonymized copy of a real BS database; until one is donated,
// this site collects the dirty data real sites were seen to have.
func TestRegressionReport(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t,
		`INSERT INTO options (option_name, option_value) VALUES
		 ('site_name', 'Regression'), ('site_name_en', 'Regression EN'), ('site_name_ko_KR', '회귀'),
		 ('site_url', 'http://skins.example.org/'), ('announcement', 'Welcome to Blessing Skin 6.0.2!'),
		 ('copyright_text', '<b>Copyright &copy; 2020 <a href="{site_url}">{site_name}</a>.</b> All rights reserved.'),
		 ('regs_per_ip', '0'), ('sign_score', '10,100'), ('score_per_storage', 'true'), ('sign_gap_time', 'a day'),
		 ('player_name_rule', 'cjk'), ('max_texture_width', '4096'), ('custom_css', 'body { color: red }'),
		 ('custom_js', 'console.log(1)'), ('meta_extras', '<meta name="x">'), ('navbar_color', 'pink'),
		 ('recaptcha_sitekey', 'only-the-site-key'), ('ygg_private_key', 'not a key'), ('ygg_tokens_limit', '3'),
		 ('plugins_enabled', '[{"name":"yggdrasil-api"}]'), ('cache_expire_time', '31536000')`,
		`INSERT INTO language_lines ("group", "key", text) VALUES
		 ('general', 'cape', '{"zh_CN":"斗篷","en":"Cloak"}'), ('skinlib', 'upload.button', '{"zh_CN":"上传吧"}'), ('general', 'skin', 'oops')`,
		`INSERT INTO oauth_clients (id, user_id, name, secret, redirect, personal_access_client, password_client, revoked, created_at) VALUES
		 (1, NULL, 'Blessing Skin Personal Access Client', 's', 'http://localhost', 1, 0, 0, '2020-01-01 00:00:00'),
		 (2, 1, '', 'secret', 'https://app.example.org/cb,https://app.example.org/cb', 0, 0, 0, NULL),
		 (3, 2, 'Bad', '$2y$10$abcdefghijklmnopqrstuuvwxyzabcdefghijklmnopqrstuvwxyz1', 'data:text/html,x', 0, 0, 0, NULL)`,
	)
	d := sqliteTarget(t)
	tg, _ := newTarget(t, d)
	r, err := Run(ctx, f.src, tg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := r.WriteText(&got); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "regression-report.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test -run TestRegressionReport -update to create it)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got.Bytes()) {
		t.Errorf("the report changed; review the diff and run go test -run TestRegressionReport -update to accept it\n--- got ---\n%s", got.String())
	}
}
