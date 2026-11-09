package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// checkMessage agrees with @formatjs/icu-messageformat-parser, which formats the texts
// in the browser, on every message of testdata/icu-corpus.json: hand-written cases for
// each rule plus random strings of the characters that matter, judged by formatjs
// itself (regenerate with testdata/icu-corpus.mjs after a formatjs upgrade).
func TestCheckMessageAgreesWithFormatJS(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "icu-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus []struct {
		Text string `json:"text"`
		OK   bool   `json:"ok"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus) < 2000 {
		t.Fatalf("corpus of %d", len(corpus))
	}
	wrong := 0
	for _, c := range corpus {
		if err := checkMessage(c.Text); (err == nil) != c.OK {
			wrong++
			if wrong <= 20 {
				t.Errorf("%q: formatjs ok=%v, checkMessage: %v", c.Text, c.OK, err)
			}
		}
	}
	if wrong > 20 {
		t.Errorf("and %d more", wrong-20)
	}
}

// Every text the frontend ships passes, so the default wording is always a valid
// override.
func TestCheckMessageAcceptsTheDefaults(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "..", "web", "apps", "web", "messages", "*.json"))
	if len(files) < 2 {
		t.Fatalf("message files: %v", files)
	}
	n := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		var walk func(prefix string, node map[string]any)
		walk = func(prefix string, node map[string]any) {
			for k, v := range node {
				switch v := v.(type) {
				case string:
					n++
					if err := checkMessage(v); err != nil {
						t.Errorf("%s %s%s: %v", filepath.Base(f), prefix, k, err)
					}
				case map[string]any:
					walk(prefix+k+".", v)
				}
			}
		}
		walk("", tree)
	}
	if n < 1000 {
		t.Fatalf("only %d texts", n)
	}
}

func TestOverridesAreCheckedAsMessages(t *testing.T) {
	ok := map[string]any{"en": map[string]any{"nav.skinlib": "Skins", "x.count": "{n, plural, one {# skin} other {# skins}}"}}
	if err := validateOverrides(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"{n, plural, one {# skin}}", "<b>bold", "{name", "{n, numbr}"} {
		v := map[string]any{"en": map[string]any{"x.y": bad}}
		if err := validateOverrides(v); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
