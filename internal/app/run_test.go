package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/secrets"
)

// End-to-end: Pre-Core Setup Mode, SSE stream open across the atomic transition to
// Running Mode, settings hot reload, and prompt shutdown with the stream still open.
func TestRunServesHotReloadAndShutsDownPromptly(t *testing.T) {
	data := t.TempDir()
	t.Setenv("SKINLOOM_SERVER__LISTEN", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: Paths{DataDir: data}, NoWeb: true, Ready: func(a net.Addr) { ready <- a }})
	}()
	var base string
	select {
	case a := <-ready:
		base = "http://" + a.String()
	case err := <-done:
		t.Fatalf("Run exited early: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("server did not become ready")
	}

	for _, p := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(base + p)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %v %v", p, err, resp)
		}
		resp.Body.Close()
	}
	// Setup Mode must block normal business endpoints with 503.
	if resp, err := http.Get(base + "/api/v1/textures"); err != nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/v1/textures in setup mode: %v %v", err, resp)
	} else {
		resp.Body.Close()
	}
	// With web.mode off, pages get the placeholder instead of a proxy error.
	if resp, err := http.Get(base + "/"); err != nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /: %v %v", err, resp)
	} else {
		resp.Body.Close()
	}

	stream, err := http.Get(base + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			events <- sc.Text()
		}
		close(events)
	}()
	expect(t, events, "event: hello")

	setupTok, err := secrets.CreateSetupToken(data, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	setupReq, _ := http.NewRequest(http.MethodPost, base+"/api/v1/setup", strings.NewReader(
		`{"token":"`+setupTok+`","email":"admin@example.com","nickname":"Admin","password":"admin-pass-1"}`,
	))
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.Header.Set("X-SkinLoom-Request", "1")
	setupResp, err := http.DefaultClient.Do(setupReq)
	if err != nil || setupResp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/setup: %v %v", err, setupResp)
	}
	setupResp.Body.Close()
	expect(t, events, "event: config.changed")

	token, err := secrets.Ensure(data)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPatch, base+"/api/v1/admin/settings", strings.NewReader(`{"site.name":{"en":"E2E"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.AdminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		body := ""
		if resp != nil {
			b, _ := io.ReadAll(resp.Body)
			body = string(b)
		}
		t.Fatalf("PATCH settings: %v %s", err, body)
	}
	resp.Body.Close()
	expect(t, events, "event: config.changed")

	resp, err = http.Get(base + "/api/v1/site/bootstrap?lang=en")
	if err != nil {
		t.Fatal(err)
	}
	var boot struct {
		Site struct{ Name string } `json:"site"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&boot)
	resp.Body.Close()
	if boot.Site.Name != "E2E" {
		t.Fatalf("bootstrap site name %q, want E2E", boot.Site.Name)
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown hung with an open event stream")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("shutdown took %s with an open event stream, want < 3s", d)
	}

	cfgPath := filepath.Join(data, "config.toml")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("config not written after setup: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(secrets.SetupTokenFile(data)); !os.IsNotExist(err) {
		t.Fatal("setup.token must be removed after setup completes")
	}
}

func TestRunColdStartLeavesNoConfigOrDB(t *testing.T) {
	data := t.TempDir()
	t.Setenv("SKINLOOM_SERVER__LISTEN", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: Paths{DataDir: data}, NoWeb: true, Ready: func(a net.Addr) { ready <- a }})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Run exited early: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("server did not become ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	for _, unexpected := range []string{"config.toml", "skinloom.db", filepath.Join("secrets", "admin.token")} {
		if _, err := os.Stat(filepath.Join(data, unexpected)); !os.IsNotExist(err) {
			t.Fatalf("unexpected file %s created before setup completed", unexpected)
		}
	}
	if _, err := os.Stat(secrets.SetupTokenFile(data)); err != nil {
		t.Fatalf("setup.token missing on cold start: %v", err)
	}
}

func TestRunInstalledSiteFailsFastOnDBOutage(t *testing.T) {
	data := t.TempDir()
	t.Setenv("SKINLOOM_SERVER__LISTEN", "127.0.0.1:0")

	// Install a site first.
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{Paths: Paths{DataDir: data}, NoWeb: true, Ready: func(a net.Addr) { ready <- a }})
	}()
	addr := <-ready
	tok, _ := secrets.CreateSetupToken(data, time.Hour)
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr.String()+"/api/v1/setup", strings.NewReader(
		`{"token":"`+tok+`","email":"admin@example.com","nickname":"Admin","password":"admin-pass-1"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SkinLoom-Request", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("setup failed: %v %v", err, resp)
	}
	resp.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// Point config.toml at an unreachable MySQL instance: Run must fail fast, NOT reopen /setup.
	bad := config.Defaults()
	bad.Database.Driver = "mysql"
	bad.Database.DSN = "user:pass@tcp(127.0.0.1:1)/skinloom?timeout=1s"
	if err := config.Write(filepath.Join(data, "config.toml"), bad, nil); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), RunOptions{Paths: Paths{DataDir: data}, NoWeb: true})
	if err == nil {
		t.Fatal("expected installed site with unreachable DB to fail fast")
	}
}

func expect(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before %q", want)
			}
			if strings.HasPrefix(l, want) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}
