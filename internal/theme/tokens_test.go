package theme

import (
	"testing"
	"testing/fstest"
)

// safeTokenValue used to refuse only ; { } < > and /*. The frontend writes every
// token into one line, `:root:root{--a:V;--b:V;…}:root:root.dark{…}`, so a value with
// an unterminated string runs to the end of the stylesheet (every later token and the
// whole dark block are swallowed into --a), a trailing backslash escapes the ; and
// eats the next declaration, and an unclosed url( or ( does the same up to the next ).
func TestOverridesRejectValuesThatSwallowLaterTokens(t *testing.T) {
	for _, v := range []string{`"oops`, `'oops`, `red\`, `url(https://x/a.png`, `calc(1px + 2px`} {
		err := ValidateOverrides(map[string]any{"light": map[string]any{"primary": v}})
		if err == nil {
			t.Errorf("theme.overrides light.primary = %q accepted; it breaks every token after it", v)
		}
	}
}

// The same check guards theme manifests at load.
func TestThemeLoadRejectsUnterminatedString(t *testing.T) {
	files := fstest.MapFS{"plugin.json": {Data: []byte(`{"id":"x","kind":"theme","version":"1",
		"theme":{"tokens":{"light":{"primary":"\"oops"},"dark":{}}}}`)}}
	if _, err := load(files, "test"); err == nil {
		t.Fatal(`a theme token "oops (unterminated string) loaded`)
	}
}
