package bs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// maxFileSize caps what is read per texture file (BS's own limit is a few MB; the
// restore command uses the same cap).
const maxFileSize = 64 << 20

// fileInfo is what the import learns about one texture file.
type fileInfo struct {
	// Hash is the SHA-256 of the content: the storage key the file goes under. It is the
	// BS hash except for files changed after upload.
	Hash          string
	Size          int64
	Width, Height int
	// Problem is set when the file is missing or cannot be used; the texture is still
	// imported (players keep it, it answers 404 as it did in BS) with an unknown size.
	Problem string
}

// scanFiles reads the file of every hash in dir, in parallel.
func scanFiles(ctx context.Context, dir string, hashes []string) map[string]fileInfo {
	out := make(map[string]fileInfo, len(hashes))
	var mu sync.Mutex
	work := make(chan string)
	var wg sync.WaitGroup
	for range max(2, runtime.NumCPU()) {
		wg.Go(func() {
			for h := range work {
				fi := inspect(dir, h)
				mu.Lock()
				out[h] = fi
				mu.Unlock()
			}
		})
	}
	for _, h := range hashes {
		if ctx.Err() != nil {
			break
		}
		work <- h
	}
	close(work)
	wg.Wait()
	return out
}

func inspect(dir, hash string) fileInfo {
	if dir == "" {
		return fileInfo{Hash: hash, Problem: "no texture directory given"}
	}
	data, err := readFile(filepath.Join(dir, hash))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fileInfo{Hash: hash, Problem: "file missing"}
	case err != nil:
		return fileInfo{Hash: hash, Problem: err.Error()}
	}
	sum := sha256.Sum256(data)
	fi := fileInfo{Hash: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fileInfo{Hash: hash, Problem: "not a PNG file"}
	}
	fi.Width, fi.Height = cfg.Width, cfg.Height
	return fi
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("larger than %d MB", maxFileSize>>20)
	}
	return data, nil
}

// copyFiles puts the file of every planned blob into the store under its content hash,
// several at a time. When the store is the BS directory itself (shared), only files
// whose content no longer matches their name need a copy. Files are content-addressed,
// so a file left behind by an import that then fails is harmless: the orphan sweep
// removes it.
func copyFiles(ctx context.Context, dir string, store storage.Store, blobs []tBlob, shared bool, progress func(done, total int)) (int, error) {
	var todo []tBlob
	for _, b := range blobs {
		if b.File != "" && (!shared || b.File != b.Hash) {
			todo = append(todo, b)
		}
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(max(2, runtime.NumCPU()))
	var done atomic.Int64
	for _, b := range todo {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := readFile(filepath.Join(dir, b.File))
			if err != nil {
				return fmt.Errorf("copy texture %s: %w", b.File, err)
			}
			if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != b.Hash {
				return fmt.Errorf("copy texture %s: the file changed during the import", b.File)
			}
			if err := store.Put(ctx, b.Hash, data); err != nil {
				return fmt.Errorf("copy texture %s: %w", b.File, err)
			}
			if n := done.Add(1); progress != nil && n%500 == 0 {
				progress(int(n), len(todo))
			}
			return nil
		})
	}
	err := g.Wait()
	return int(done.Load()), err
}
