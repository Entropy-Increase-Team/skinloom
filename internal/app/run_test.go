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
	"strings"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/secrets"
)

// End-to-end: a real server on a free port, a browser-like SSE client, a settings change
// through the admin API, and a graceful shutdown with the stream still open.
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

	if _, err := os.Stat(filepath.Join(data, "config.toml")); err != nil {
		t.Fatalf("default config not written: %v", err)
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
