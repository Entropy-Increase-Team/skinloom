package bs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/oauthclient"
	"github.com/Entropy-Increase-Team/skinloom/internal/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

// TestImportSiteData: options, language_lines, OAuth clients, the Yggdrasil key and the
// mail settings of .env (ADR 0036).
func TestImportSiteData(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t,
			`INSERT INTO options (option_name, option_value) VALUES
			 ('site_name', '旧站'), ('site_name_zh_CN', '旧站'), ('site_name_en', 'Old Site'), ('site_name_ja_JP', '古いサイト'),
			 ('site_url', 'https://skin.example.com/index.php'), ('announcement', 'Welcome to Blessing Skin 6.0.2!'),
			 ('copyright_text', '<b>&copy; 2020 <a href="{site_url}">{site_name}</a></b>'),
			 ('regs_per_ip', '5'), ('require_verification', 'true'), ('score_per_storage', 'true'), ('sign_score', '50,5'),
			 ('player_name_rule', 'custom'), ('custom_player_name_regexp', '/^[a-z_\x{4e00}-\x{9fa5}]+$/iu'),
			 ('player_name_length_min', '2'), ('player_name_length_max', '40'), ('max_upload_file_size', '10000'),
			 ('recaptcha_sitekey', 'site-key'), ('recaptcha_secretkey', 'secret-key'), ('custom_js', 'alert(1)'),
			 ('user_initial_score', '1000'), ('texture_name_regexp', '/(?<=a)b/'), ('ygg_token_expire_1', '600'),
			 ('favicon_url', 'https://cdn.example.com/icon.png'), ('ygg_private_key', '`+pkcs1+`')`,
			`INSERT INTO language_lines ("group", "key", text) VALUES
			 ('general', 'skinlib', '{"zh_CN":"材质库","en":"Texture Library","ja_JP":"テクスチャ"}'),
			 ('front-end', 'general.skinlib', '{"zh_CN":"别的名字"}'),
			 ('general', 'user.score', '{"zh_CN":"金币"}'),
			 ('general', 'cape', '{"zh_CN":"<b>披风"}'),
			 ('auth', 'login.title', '{"zh_CN":"登录本站"}')`,
			`INSERT INTO oauth_clients (id, user_id, name, secret, redirect, personal_access_client, password_client, revoked, created_at) VALUES
			 (3, 1, 'Forum', 'plain-secret', 'https://forum.example.com/cb, javascript:alert(1)', 0, 0, 0, '2022-01-01 00:00:00'),
			 (4, 1, 'Old', 'x', 'https://old.example.com/cb', 0, 0, 1, '2022-01-01 00:00:00'),
			 (5, NULL, 'Personal Access Client', 'y', 'http://localhost', 1, 0, 0, '2022-01-01 00:00:00'),
			 (6, 77, 'Ghost app', 'z', 'https://ghost.example.com/cb', 0, 0, 0, '2022-01-01 00:00:00'),
			 (7, 2, 'Public app', NULL, 'myapp://callback', 0, 0, 0, '2022-01-01 00:00:00')`,
		)
		f.src.Mail = &Mail{Mailer: "smtp", Host: "smtp.example.com", Port: "465", Username: "noreply@example.com", Password: "mail-pw",
			FromAddress: "noreply@example.com", FromName: "Old Site", HostSet: true}
		tg, _ := newTarget(t, d)
		r, err := Run(ctx, f.src, tg, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if r.Blocked() {
			t.Fatal(entries(r, Blocking))
		}

		got := map[string]string{}
		for _, row := range d.Ent.Setting.Query().AllX(ctx) {
			got[row.ID] = row.Value
		}
		want := map[string]string{
			"site.name":                       `{"en":"Old Site","zh-CN":"旧站"}`,
			"site.url":                        `"https://skin.example.com"`,
			"site.copyright":                  `{"zh-CN":"**© 2020 [旧站](https://skin.example.com)**"}`,
			"auth.registrations_per_ip":       `5`,
			"auth.require_email_verification": `true`,
			"score.sign_min":                  `5`,
			"score.sign_max":                  `50`,
			"player.name_rule":                `"custom"`,
			"player.name_regexp":              `"(?i)^[a-z_\\x{4e00}-\\x{9fa5}]+$"`,
			"player.name_length_min":          `2`,
			"player.name_length_max":          `32`,
			"texture.max_size_kb":             `8192`,
			"captcha.provider":                `"recaptcha"`,
			"captcha.recaptcha.site_key":      `"site-key"`,
			"captcha.recaptcha.secret_key":    `"secret-key"`,
			"theme.custom_js":                 `"alert(1)"`,
			"yggdrasil.token_valid_seconds":   `600`,
			"site.favicon":                    `"https://cdn.example.com/icon.png"`,
			"mail.driver":                     `"smtp"`,
			"mail.smtp.host":                  `"smtp.example.com"`,
			"mail.smtp.username":              `"noreply@example.com"`,
			"mail.smtp.password":              `"mail-pw"`,
			"mail.from":                       `"\"Old Site\" \u003cnoreply@example.com\u003e"`,
		}
		for k, v := range want {
			if !sameJSON(json.RawMessage(got[k]), json.RawMessage(v)) {
				t.Errorf("%s = %s, want %s", k, got[k], v)
			}
		}
		// Defaults write no row: BS's default announcement, score_per_storage "true" (= 1),
		// the initial score, TLS on port 465 (SkinLoom's default).
		for _, k := range []string{"site.announcement", "score.per_storage", "score.initial", "mail.smtp.encryption", "mail.smtp.port", "texture.name_regexp"} {
			if v, ok := got[k]; ok {
				t.Errorf("%s = %s: should be left at the default", k, v)
			}
		}
		var ov map[string]map[string]string
		if err := json.Unmarshal([]byte(got["i18n.overrides"]), &ov); err != nil {
			t.Fatalf("i18n.overrides = %s: %v", got["i18n.overrides"], err)
		}
		if ov["zh-CN"]["nav.skinlib"] != "材质库" || ov["en"]["skinlib.title"] != "Texture Library" || ov["zh-CN"]["dashboard.score"] != "金币" || len(ov) != 2 {
			t.Errorf("overrides = %v", ov)
		}
		// Everything written passes the checks saving it in the admin panel would.
		for k, v := range got {
			def, ok := tg.Settings.Def(k)
			if !ok {
				t.Errorf("unknown setting %s written", k)
				continue
			}
			if _, err := def.Canonical(json.RawMessage(v)); err != nil {
				t.Errorf("%s = %s does not pass its own check: %v", k, v, err)
			}
		}
		cs := d.Ent.OAuthClient.Query().Order(oauthclient.ByID()).AllX(ctx)
		if len(cs) != 2 || cs[0].ID != 3 || cs[1].ID != 7 {
			t.Fatalf("clients = %v", cs)
		}
		if cs[0].SecretHash == nil || *cs[0].SecretHash != oauth.HashSecret("plain-secret") || len(cs[0].RedirectUris) != 1 || cs[0].RedirectUris[0] != "https://forum.example.com/cb" {
			t.Errorf("client 3 = %+v", cs[0])
		}
		if cs[1].SecretHash != nil || cs[1].UserID != 2 {
			t.Errorf("client 7 = %+v", cs[1])
		}
		if r.Imported["oauth_clients"] != 2 || r.Imported["settings"] != len(got) {
			t.Errorf("imported = %v", r.Imported)
		}

		sec := d.Ent.SiteSecret.GetX(ctx, signingKeyName)
		block, _ := pem.Decode([]byte(sec.Value))
		k8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil || !k8.(*rsa.PrivateKey).Equal(key) {
			t.Errorf("signing key not carried over: %v", err)
		}

		for sev, want := range map[Severity][]string{
			Dropped: {"site_name_ja_JP: SkinLoom has no ja_JP interface yet", "auth.login.title", "general.skinlib (ja_JP)",
				"general.cape (zh_CN) = \"<b>披风\": a term must be short plain text", "oauth_clients 4: revoked", "oauth_clients 5: Passport's personal access client", "oauth_clients 6: owner 77 no longer exists"},
			Warning: {"texture_name_regexp", "custom_js: SkinLoom switches pages without reloading", "redirect URI \"javascript:alert(1)\" left out", "front-end.general.skinlib (zh_CN)"},
			Fixed:   {"copyright_text", "player_name_length_min/max", "general.skinlib (zh_CN) = \"材质库\": used for nav.skinlib", "mail: SMTP smtp.example.com:465 with tls"},
		} {
			got := entries(r, sev)
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("%v entries lack %q:\n%s", sev, w, got)
				}
			}
		}
	})
}

// TestTextTermsExist: every SkinLoom text a BS term is mapped to exists in both
// catalogs and is a plain term (overrides keep the default's placeholders).
func TestTextTermsExist(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "web", "apps", "web", "messages")
	for _, loc := range []string{"zh-CN", "en"} {
		raw, err := os.ReadFile(filepath.Join(dir, loc+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		for bsKey, keys := range textTerms {
			for _, k := range keys {
				var node any = tree
				for part := range strings.SplitSeq(k, ".") {
					m, _ := node.(map[string]any)
					node = m[part]
				}
				s, ok := node.(string)
				switch {
				case !ok:
					t.Errorf("%s → %s: no such text in %s", bsKey, k, loc)
				case strings.ContainsAny(s, "{}<>"):
					t.Errorf("%s → %s: %q is not a plain term", bsKey, k, s)
				}
			}
		}
	}
}

func TestPHPValues(t *testing.T) {
	for in, want := range map[string]int64{"12": 12, " 7 ": 7, "-3": -3, "12abc": 12, "abc": 0, "true": 1, "false": 0, "": 0, "null": 0} {
		if n, _ := phpInt(phpOption(in)); n != want {
			t.Errorf("phpInt(%q) = %d, want %d", in, n, want)
		}
	}
	for in, want := range map[string]bool{"true": true, "false": false, "1": true, "0": false, "": false, "on": true, "(false)": false} {
		if got := phpBool(phpOption(in)); got != want {
			t.Errorf("phpBool(%q) = %v", in, got)
		}
	}
}

func TestPHPRegexp(t *testing.T) {
	for in, want := range map[string]string{
		`/^[a-z]+$/`:     `^[a-z]+$`,
		`/^[a-z]+$/iu`:   `(?i)^[a-z]+$`,
		`#^\w{3,}$#`:     `^\w{3,}$`,
		`{^[A-Z]$}`:      `^[A-Z]$`,
		`/(?<=a)b/`:      "",
		`/^(a)\1$/`:      "",
		`no delimiters`:  "",
		`/^x$/x`:         "",
		`/unterminated`:  "",
		`/^[\x{4e00}]$/`: `^[\x{4e00}]$`,
	} {
		got, ok := phpRegexp(in)
		if got != want || ok != (want != "") {
			t.Errorf("phpRegexp(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestPlanMail(t *testing.T) {
	reg := testRegistry(t)
	for _, tc := range []struct {
		mail Mail
		want map[string]string // setting → JSON; nil = nothing written
	}{
		{Mail{Mailer: "smtp", Host: "smtp.qq.com", Port: "587", Encryption: "tls", FromAddress: "a@b.c", HostSet: true},
			map[string]string{"mail.driver": `"smtp"`, "mail.smtp.port": `587`, "mail.smtp.encryption": `"starttls"`}},
		{Mail{Mailer: "smtp", Host: "localhost", Port: "25", HostSet: true}, map[string]string{"mail.smtp.encryption": `"none"`, "mail.smtp.port": `25`}},
		{Mail{Mailer: "smtp", Port: "587"}, nil}, // MAIL_HOST never set: Laravel's placeholder
		{Mail{Mailer: "log"}, map[string]string{"mail.driver": `"log"`}},
		{Mail{Mailer: "sendmail", HostSet: true}, nil},
	} {
		r := &Report{}
		m := &optionMapper{reg: reg, out: map[string]json.RawMessage{}, r: r}
		planMail(m, &tc.mail)
		if tc.want == nil && len(m.out) != 0 {
			t.Errorf("%+v: wrote %v", tc.mail, m.out)
		}
		for k, v := range tc.want {
			if !sameJSON(m.out[k], json.RawMessage(v)) {
				t.Errorf("%+v: %s = %s, want %s", tc.mail, k, m.out[k], v)
			}
		}
	}
}

func TestHTMLToMarkdown(t *testing.T) {
	got := htmlToMarkdown(`<p>Hi <i>there</i><br/>by <strong>{site_name}</strong> &amp; <a href='{site_url}/about' target="_blank">us</a></p>`, "Site", "https://s.example")
	if want := "Hi *there*  \nby **Site** & [us](https://s.example/about)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSigningKeyFormats(t *testing.T) {
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	der8, _ := x509.MarshalPKCS8PrivateKey(small)
	for name, in := range map[string]string{
		"garbage":   "not a key",
		"public":    string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1}})),
		"encrypted": string(pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1}})),
		"too small": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der8})),
	} {
		r := &Report{}
		if got := planSigningKey(&sourceData{Options: map[string]string{"ygg_private_key": in}}, r); got != "" || !strings.Contains(entries(r, Warning), "restart once") {
			t.Errorf("%s: key %q, warnings %s", name, got, entries(r, Warning))
		}
	}
}

// A site without option rows (option() falls back to config/options.php) imports no
// placeholder text: BS's raw default announcement still says {version}.
func TestPlanSettingsWithoutOptions(t *testing.T) {
	r := &Report{}
	d := &sourceData{Options: map[string]string{}}
	out := planSettings(d, planOptions{Settings: testRegistry(t), Locales: []string{"zh-CN", "en"}, Location: shanghai}, r)
	for _, k := range []string{"site.announcement", "site.description", "site.copyright", "site.url", "site.favicon"} {
		if v, ok := out[k]; ok {
			t.Errorf("%s = %s, want SkinLoom's default", k, v)
		}
	}
	// Only the .env warning (no Mail) and the one default that changes what users see:
	// BS asked for a player name at sign-up.
	if n := r.Count(Warning) + r.Count(Dropped); n != 2 || !strings.Contains(entries(r, Dropped), "register_with_player_name") {
		t.Errorf("report: %v", r.Entries)
	}
}

// BS's custom rule with no pattern accepts any name (PlayerName falls back to /.*/),
// as SkinLoom's custom rule with an empty player.name_regexp does: the rule stays
// custom instead of turning into the stricter official one.
func TestPlanSettingsCustomRuleWithoutPattern(t *testing.T) {
	r := &Report{}
	d := &sourceData{Options: map[string]string{"player_name_rule": "custom", "custom_player_name_regexp": ""}}
	out := planSettings(d, planOptions{Settings: testRegistry(t), Locales: []string{"zh-CN", "en"}, Location: shanghai}, r)
	if got := string(out["player.name_rule"]); got != `"custom"` {
		t.Errorf("player.name_rule = %s, want \"custom\"", got)
	}
	if v, ok := out["player.name_regexp"]; ok {
		t.Errorf("player.name_regexp = %s, want the default (any name)", v)
	}
	if w := entries(r, Warning); strings.Contains(w, "custom_player_name_regexp") {
		t.Errorf("warned about an empty pattern:\n%s", w)
	}
}

// Laravel 10 (Symfony Mailer) picks implicit TLS by the port alone: MAIL_ENCRYPTION=ssl
// on 587 still talks STARTTLS, and implicit TLS there would not connect.
func TestPlanMailEncryptionFollowsThePort(t *testing.T) {
	for _, tc := range []struct{ port, enc, want string }{
		{"465", "", "tls"},
		{"465", "ssl", "tls"},
		{"465", "tls", "tls"},
		{"587", "ssl", "starttls"},
		{"587", "tls", "starttls"},
		{"587", "", "starttls"},
		{"25", "", "none"},
		{"25", "ssl", "starttls"},
	} {
		mail := &Mail{Mailer: "smtp", Host: "smtp.example.com", HostSet: true, Port: tc.port, Encryption: tc.enc, FromAddress: "noreply@example.com"}
		out := planSettings(&sourceData{Options: map[string]string{}}, planOptions{Settings: testRegistry(t), Locales: []string{"zh-CN", "en"}, Location: shanghai, Mail: mail}, &Report{})
		got := string(out["mail.smtp.encryption"])
		if got == "" {
			got = `"tls"` // left out: the same as SkinLoom's default
		}
		if got != `"`+tc.want+`"` {
			t.Errorf("port %s, MAIL_ENCRYPTION=%q: encryption %s, want %q", tc.port, tc.enc, got, tc.want)
		}
	}
}

func TestFromHeader(t *testing.T) {
	for _, tc := range []struct{ name, addr, want string }{
		{"", "noreply@example.com", "noreply@example.com"},
		{"Old Site", "noreply@example.com", `"Old Site" <noreply@example.com>`},
		{"老皮肤站", "noreply@example.com", `"老皮肤站" <noreply@example.com>`},
		{`Say "hi" \ there`, "a@b.example", `"Say \"hi\" \\ there" <a@b.example>`},
		{"Name", "not an address", "not an address"},
	} {
		got := fromHeader(tc.name, tc.addr)
		if got != tc.want {
			t.Errorf("fromHeader(%q, %q) = %s, want %s", tc.name, tc.addr, got, tc.want)
		}
		if a, err := mail.ParseAddress(got); tc.name != "" && tc.addr != "not an address" && (err != nil || a.Name != tc.name) {
			t.Errorf("%s does not parse back to %q: %v %+v", got, tc.name, err, a)
		}
	}
}
