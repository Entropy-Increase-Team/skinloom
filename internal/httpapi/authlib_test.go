package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuthlibInjector (design 02 §6 item 2) runs testdata/authlib/Harness.java, a
// stand-in for authlib-injector and Mojang's authlib, against a real server: sign in,
// pick a profile, join, hasJoined, profile lookups, with every signature checked
// against the metadata key and every texture URL against skinDomains. It needs Java
// 11+; without java on PATH it is skipped, unless SKINLOOM_TEST_JAVA is set (CI).
func TestAuthlibInjector(t *testing.T) {
	java, err := exec.LookPath("java")
	if err != nil {
		if os.Getenv("SKINLOOM_TEST_JAVA") != "" {
			t.Fatal("SKINLOOM_TEST_JAVA is set but java is not on PATH")
		}
		t.Skip("java not on PATH")
	}
	d := newTestDeps(t)
	h, _ := New(d)
	srv := httptest.NewServer(h)
	defer srv.Close()
	// Texture URLs in profiles point at the site address: this server.
	setSettings(t, d, fmt.Sprintf(`{"captcha.provider": "none", "site.url": %q}`, srv.URL))
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	var skin, cape UploadView
	_ = json.Unmarshal(upload(t, alice, "Skin", "alex", true, testPNG(t, 64, 64, 7)).Body.Bytes(), &skin)
	_ = json.Unmarshal(upload(t, alice, "Cape", "cape", true, testPNG(t, 64, 32, 8)).Body.Bytes(), &cape)
	// Two players: the launcher picks one and refreshes with it, as HMCL and others do.
	for i, name := range []string{"Steve", "Second"} {
		var c PlayerCreated
		rec := alice.do("POST", "/api/v1/me/players", fmt.Sprintf(`{"name":%q}`, name))
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("player: %d %s", rec.Code, rec.Body)
		}
		if i == 0 {
			if rec := alice.do("PUT", fmt.Sprintf("/api/v1/me/players/%d/textures", c.Player.ID),
				fmt.Sprintf(`{"skin":%d,"cape":%d}`, skin.Texture.ID, cape.Texture.ID)); rec.Code != http.StatusOK {
				t.Fatalf("wear: %d %s", rec.Code, rec.Body)
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	harness, _ := filepath.Abs(filepath.Join("testdata", "authlib", "Harness.java"))
	cmd := exec.CommandContext(ctx, java, harness, srv.URL+YggdrasilPrefix, "alice@example.com", "user-pass-1")
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil || !strings.Contains(string(out), "ALL OK") {
		t.Fatalf("harness: %v", err)
	}
}
