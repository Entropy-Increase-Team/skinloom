// Package secrets creates and reads the per-install secrets under <data>/secrets.
// Nothing secret is ever baked into a binary or image (unlike BS's Dockerfile, issue D01/S12).
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Ensure loads the secrets, generating missing ones. Safe to call from several
// processes at once (server + CLI): exactly one generated value wins and everyone
// returns it.
func Ensure(dataDir string) (*Secrets, error) {
	dir := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
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
