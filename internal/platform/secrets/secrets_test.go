package secrets

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestSetupTokenLifecycleAndSecurity(t *testing.T) {
	dir := t.TempDir()
	var tokens []string
	for i := range 7 {
		tok, err := CreateSetupToken(dir, time.Hour)
		if err != nil {
			t.Fatalf("CreateSetupToken %d: %v", i, err)
		}
		if len(tok) != 43 {
			t.Fatalf("token length = %d, want 43", len(tok))
		}
		tokens = append(tokens, tok)
	}
	// Raw token must never appear in the file on disk.
	rawFile, err := os.ReadFile(SetupTokenFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if strings.Contains(string(rawFile), tok) {
			t.Fatal("raw setup token found in setup.token file")
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(SetupTokenFile(dir))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("setup.token mode = %v, want 0600", info.Mode().Perm())
		}
	}
	// Only the newest 5 tokens are kept.
	for i, tok := range tokens {
		ok, err := VerifySetupToken(dir, tok)
		if err != nil {
			t.Fatal(err)
		}
		want := i >= 2
		if ok != want {
			t.Fatalf("token %d verify = %v, want %v", i, ok, want)
		}
	}
	if ok, err := VerifySetupToken(dir, "wrong-token"); err != nil || ok {
		t.Fatalf("wrong token accepted: ok=%v err=%v", ok, err)
	}
	if err := ClearSetupToken(dir); err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifySetupToken(dir, tokens[len(tokens)-1]); err != nil || ok {
		t.Fatalf("cleared token accepted: ok=%v err=%v", ok, err)
	}
}

func TestSetupTokenExpirationAndMarkers(t *testing.T) {
	dir := t.TempDir()
	// Expired token stored on disk must be rejected.
	if _, err := ensureSecretsDir(dir); err != nil {
		t.Fatal(err)
	}
	expiredJSON := `{"tokenHash":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","expiresAt":"2020-01-01T00:00:00Z","createdAt":"2019-12-31T00:00:00Z"}`
	if err := writePrivateFile(SetupTokenFile(dir), []byte(expiredJSON)); err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifySetupToken(dir, ""); err != nil || ok {
		t.Fatalf("empty token must not verify: ok=%v err=%v", ok, err)
	}

	if IsSetupPending(dir) || IsSetupDone(dir) {
		t.Fatal("fresh dir must have no setup markers")
	}
	if err := MarkSetupPending(dir); err != nil {
		t.Fatal(err)
	}
	if !IsSetupPending(dir) {
		t.Fatal("pending marker missing")
	}
	if err := ClearSetupPending(dir); err != nil {
		t.Fatal(err)
	}
	if IsSetupPending(dir) {
		t.Fatal("pending marker not cleared")
	}
	if err := MarkSetupDone(dir); err != nil {
		t.Fatal(err)
	}
	if !IsSetupDone(dir) {
		t.Fatal("done marker missing")
	}
}
