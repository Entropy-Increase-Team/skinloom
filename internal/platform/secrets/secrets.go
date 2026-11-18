// Package secrets creates and reads the per-install secrets under <data>/secrets.
// Nothing secret is ever baked into a binary or image (unlike BS's Dockerfile, issue D01/S12).
package secrets

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
)

// Secrets are generated on first start.
type Secrets struct {
	Dir string
	// AdminToken guards the admin API until accounts land in M1.
	AdminToken string
}

// AdminTokenFile is the path of the admin token inside dir.
func AdminTokenFile(dataDir string) string {
	return filepath.Join(dataDir, "secrets", "admin.token")
}

// SetupTokenFile is the path of the file-backed setup tokens created before a database is configured.
func SetupTokenFile(dataDir string) string {
	return filepath.Join(dataDir, "secrets", "setup.token")
}

// SetupPendingFile marks an in-progress or incomplete setup attempt so a failed DB
// write during first-run setup stays diagnostic instead of locking out /setup.
func SetupPendingFile(dataDir string) string {
	return filepath.Join(dataDir, "secrets", "setup.pending")
}

// SetupDoneFile records that this data directory has completed installation at least once,
// so a later database outage fails fast rather than falling back to Setup Mode.
func SetupDoneFile(dataDir string) string {
	return filepath.Join(dataDir, "secrets", "setup.done")
}

const maxSetupTokens = 5

var setupTokenMu sync.Mutex

type setupTokenEntry struct {
	TokenHash string    `json:"tokenHash"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type setupTokenStore struct {
	Tokens []setupTokenEntry `json:"tokens"`
}

func ensureSecretsDir(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create secrets directory: %w", err)
	}
	if err := fsperm.Private(dir); err != nil { // 0700 means nothing on Windows
		return "", err
	}
	return dir, nil
}

// CreateSetupToken creates a new one-time setup token (32 random bytes, base64url),
// stores its SHA-256 hash in <dataDir>/secrets/setup.token (keeping up to 5 recent
// unexpired tokens), and returns the raw token.
func CreateSetupToken(dataDir string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	setupTokenMu.Lock()
	defer setupTokenMu.Unlock()

	if _, err := ensureSecretsDir(dataDir); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	existing := loadSetupTokens(SetupTokenFile(dataDir), now)

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	entry := setupTokenEntry{
		TokenHash: hex.EncodeToString(sum[:]),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	tokens := append([]setupTokenEntry{entry}, existing...)
	if len(tokens) > maxSetupTokens {
		tokens = tokens[:maxSetupTokens]
	}
	payload, err := json.MarshalIndent(setupTokenStore{Tokens: tokens}, "", "  ")
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	if err := writePrivateFile(SetupTokenFile(dataDir), payload); err != nil {
		return "", err
	}
	return raw, nil
}

// VerifySetupToken checks whether raw matches an unexpired hash stored in setup.token.
func VerifySetupToken(dataDir, raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	setupTokenMu.Lock()
	defer setupTokenMu.Unlock()

	entries := loadSetupTokens(SetupTokenFile(dataDir), time.Now().UTC())
	if len(entries) == 0 {
		return false, nil
	}
	sum := sha256.Sum256([]byte(raw))
	gotHash := hex.EncodeToString(sum[:])
	matched := false
	for _, e := range entries {
		if len(e.TokenHash) == len(gotHash) && subtle.ConstantTimeCompare([]byte(e.TokenHash), []byte(gotHash)) == 1 {
			matched = true
		}
	}
	return matched, nil
}

// ClearSetupToken removes <dataDir>/secrets/setup.token.
func ClearSetupToken(dataDir string) error {
	setupTokenMu.Lock()
	defer setupTokenMu.Unlock()
	err := os.Remove(SetupTokenFile(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// MarkSetupPending writes the <dataDir>/secrets/setup.pending marker.
func MarkSetupPending(dataDir string) error {
	if _, err := ensureSecretsDir(dataDir); err != nil {
		return err
	}
	return writePrivateFile(SetupPendingFile(dataDir), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

// IsSetupPending reports whether <dataDir>/secrets/setup.pending exists.
func IsSetupPending(dataDir string) bool {
	_, err := os.Stat(SetupPendingFile(dataDir))
	return err == nil
}

// ClearSetupPending removes <dataDir>/secrets/setup.pending.
func ClearSetupPending(dataDir string) error {
	err := os.Remove(SetupPendingFile(dataDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// MarkSetupDone writes the <dataDir>/secrets/setup.done marker.
func MarkSetupDone(dataDir string) error {
	if _, err := ensureSecretsDir(dataDir); err != nil {
		return err
	}
	return writePrivateFile(SetupDoneFile(dataDir), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"))
}

// IsSetupDone reports whether <dataDir>/secrets/setup.done exists.
func IsSetupDone(dataDir string) bool {
	_, err := os.Stat(SetupDoneFile(dataDir))
	return err == nil
}

func loadSetupTokens(path string, now time.Time) []setupTokenEntry {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []setupTokenEntry
	var store setupTokenStore
	if err := json.Unmarshal(b, &store); err == nil && len(store.Tokens) > 0 {
		out = store.Tokens
	} else {
		var single setupTokenEntry
		if err := json.Unmarshal(b, &single); err == nil && single.TokenHash != "" {
			out = []setupTokenEntry{single}
		}
	}
	live := out[:0]
	for _, e := range out {
		if e.TokenHash != "" && e.ExpiresAt.After(now) {
			live = append(live, e)
		}
	}
	return live
}

func writePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-secret-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := fsperm.Private(tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// Ensure loads the secrets, generating missing ones. Safe to call from several
// processes at once (server + CLI): exactly one generated value wins and everyone
// returns it.
func Ensure(dataDir string) (*Secrets, error) {
	dir := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}
	if err := fsperm.Private(dir); err != nil { // 0700 means nothing on Windows
		return nil, err
	}
	token, err := ensureFile(AdminTokenFile(dataDir), 32)
	if err != nil {
		return nil, err
	}
	return &Secrets{Dir: dir, AdminToken: token}, nil
}

func ensureFile(path string, size int) (string, error) {
	for attempt := 0; attempt < 50; attempt++ {
		if v, err := read(path); err == nil && v != "" {
			return v, nil
		} else if err == nil { // present but empty: replace it
			_ = os.Remove(path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("read %s: %w", path, err)
		}

		buf := make([]byte, size)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		v := hex.EncodeToString(buf)
		// Write the full value to a temp file, then link it into place: the target either
		// does not exist or already holds a complete secret, never a partial write.
		tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-secret-*")
		if err != nil {
			return "", err
		}
		_, werr := tmp.WriteString(v + "\n")
		cerr := tmp.Close()
		if werr != nil || cerr != nil {
			os.Remove(tmp.Name())
			return "", errors.Join(werr, cerr)
		}
		_ = os.Chmod(tmp.Name(), 0o600)
		lerr := os.Link(tmp.Name(), path) // fails if another process won the race
		os.Remove(tmp.Name())
		if lerr == nil {
			return v, nil
		}
		if !errors.Is(lerr, fs.ErrExist) {
			// No hard links (FAT/exFAT drives, some network shares): exclusive create.
			if err := createExclusive(path, v); err == nil {
				return v, nil
			} else if !errors.Is(err, fs.ErrExist) {
				return "", fmt.Errorf("write %s: %w (link: %v)", path, err, lerr)
			}
		}
		time.Sleep(time.Millisecond) // someone else created it; read theirs
	}
	return "", fmt.Errorf("could not create %s", path)
}

func createExclusive(path, v string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(v + "\n")
	return errors.Join(werr, f.Close())
}

func read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
