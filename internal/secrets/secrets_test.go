package secrets

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestEnsureIsStable(t *testing.T) {
	dir := t.TempDir()
	a, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.AdminToken) != 64 {
		t.Fatalf("token length %d, want 64 hex chars", len(a.AdminToken))
	}
	b, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.AdminToken != b.AdminToken {
		t.Fatal("a second Ensure must return the stored token")
	}
}

// Regression: the server and `skinloom admin token` starting at the same time could
// both generate a token; the loser's file overwrote the winner's, so the running server
// rejected the token printed by the CLI.
func TestEnsureConcurrentFirstRun(t *testing.T) {
	dir := t.TempDir()
	const n = 16
	tokens := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Ensure(dir)
			errs[i] = err
			if s != nil {
				tokens[i] = s.AdminToken
			}
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("Ensure %d: %v", i, errs[i])
		}
		if tokens[i] != tokens[0] {
			t.Fatalf("Ensure %d returned a different token", i)
		}
	}
	onDisk, _ := os.ReadFile(AdminTokenFile(dir))
	if strings.TrimSpace(string(onDisk)) != tokens[0] {
		t.Fatal("file content differs from the returned token")
	}
}

func TestEnsureRegeneratesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AdminTokenFile(dir), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.AdminToken) != 64 {
		t.Fatalf("empty token file must be regenerated, got %q", s.AdminToken)
	}
}
