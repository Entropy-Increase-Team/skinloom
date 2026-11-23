package bs

import (
	"strings"
	"testing"
)

// Every enabled plugin gets a line, so nothing a site relied on is left out of the
// report silently: known ones with what SkinLoom does instead, others as unknown.
func TestPlanPlugins(t *testing.T) {
	r := &Report{}
	planPlugins(&sourceData{Options: map[string]string{"plugins_enabled": `[{"name":"yggdrasil-connect","version":"6.1.0"},` +
		`{"name":"texture-description","version":"1.3.2"},{"name":"oauth-github"},{"name":"hitokoto"},{"name":"school-identity","version":"0.4.3"},{"name":"blessing-skin-export-bridge","version":"1.0.0"},{"name":""}]`}}, r)
	want := map[string]Severity{
		"yggdrasil-connect 6.1.0":           Warning,
		"texture-description 1.3.2":         Warning,
		"blessing-skin-export-bridge 1.0.0": Fixed,
		"oauth-github":                      Dropped,
		"hitokoto":                          Dropped,
		"school-identity 0.4.3":             Warning,
	}
	if len(r.Entries) != len(want) {
		t.Fatalf("%d entries, want %d: %+v", len(r.Entries), len(want), r.Entries)
	}
	for _, e := range r.Entries {
		name, _, _ := strings.Cut(e.Message, ":")
		if sev, ok := want[name]; !ok || e.Severity != sev || e.Table != "plugins" {
			t.Errorf("%+v: want %v for %q", e, want[name], name)
		}
	}
	if !strings.Contains(r.Entries[4].Message, "does not know this plugin") {
		t.Errorf("unknown plugin: %s", r.Entries[4].Message)
	}

	// No list, or not JSON: nothing to say.
	for _, v := range []string{"", "not json"} {
		r := &Report{}
		planPlugins(&sourceData{Options: map[string]string{"plugins_enabled": v}}, r)
		if len(r.Entries) != 0 {
			t.Errorf("%q: %+v", v, r.Entries)
		}
	}
}

func TestPlanYggdrasilConnectOptions(t *testing.T) {
	r := &Report{}
	d := &sourceData{
		Options: map[string]string{
			"plugins_enabled":        `[{"name":"yggdrasil-connect","version":"6.1.0"}]`,
			"ygg_disable_authserver": "true",
			"ygg_connect_server_url": "https://janus.example.com",
		},
	}
	reg := testRegistry(t)
	out := planSettings(d, planOptions{Settings: reg, Locales: []string{"zh-CN", "en"}, Location: shanghai}, r)
	if string(out["yggdrasil.disable_authserver"]) != "true" {
		t.Fatalf("expected yggdrasil.disable_authserver=true, got %q", string(out["yggdrasil.disable_authserver"]))
	}
	if string(out["player.keep_uuid_on_rename"]) != "true" {
		t.Fatalf("expected player.keep_uuid_on_rename=true, got %q", string(out["player.keep_uuid_on_rename"]))
	}

	planPlugins(d, r)
	var sawPluginHint, sawJanus bool
	for _, e := range r.Entries {
		if strings.Contains(e.Message, "bs-importer") && strings.Contains(e.Message, "yggdrasil-connect") {
			sawPluginHint = true
		}
		if strings.Contains(e.Message, "ygg_connect_server_url") && strings.Contains(e.Message, "/.well-known/openid-configuration") {
			sawJanus = true
		}
	}
	if !sawPluginHint {
		t.Fatalf("expected generic plugin migration hint in report, got %+v", r.Entries)
	}
	if !sawJanus {
		t.Fatalf("expected built-in OIDC hint for ygg_connect_server_url, got %+v", r.Entries)
	}
}
