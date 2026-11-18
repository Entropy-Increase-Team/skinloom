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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
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

// FileFetcher inspects and reads texture files by BS hash.
type FileFetcher interface {
	Inspect(ctx context.Context, hashes []string, progress func(done, total int)) map[string]fileInfo
	Fetch(ctx context.Context, hash string) ([]byte, error)
	SharedWith(storeDir string) bool
}

type dirFileFetcher struct {
	dir string
}

func (f dirFileFetcher) Inspect(ctx context.Context, hashes []string, progress func(done, total int)) map[string]fileInfo {
	return scanFiles(ctx, f.dir, hashes, progress)
}

func (f dirFileFetcher) Fetch(_ context.Context, hash string) ([]byte, error) {
	return readFile(filepath.Join(f.dir, hash))
}

func (f dirFileFetcher) SharedWith(storeDir string) bool {
	return sameDir(f.dir, storeDir)
}

// DetectTexturesRoot inspects an extracted texture archive directory and locates the
// folder holding the 64-hex hash files (checking root, textures/, storage/textures/,
// and a single top-level wrapper folder if present).
func DetectTexturesRoot(dir string) string {
	if dir == "" {
		return ""
	}
	candidates := []string{
		dir,
		filepath.Join(dir, "textures"),
		filepath.Join(dir, "storage", "textures"),
	}
	if entries, err := os.ReadDir(dir); err == nil {
		var dirs []string
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "__MACOSX" {
				dirs = append(dirs, filepath.Join(dir, e.Name()))
			}
		}
		if len(dirs) == 1 {
			candidates = append(candidates,
				dirs[0],
				filepath.Join(dirs[0], "textures"),
				filepath.Join(dirs[0], "storage", "textures"),
			)
		}
	}
	for _, c := range candidates {
		if hasHexTextureFile(c) {
			return c
		}
	}
	return dir
}

func hasHexTextureFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && storage.ValidKey(e.Name()) {
			return true
		}
	}
	return false
}

// scanFiles reads the file of every hash in dir, in parallel.
func scanFiles(ctx context.Context, dir string, hashes []string, progress func(done, total int)) map[string]fileInfo {
	out := make(map[string]fileInfo, len(hashes))
	var mu sync.Mutex
	work := make(chan string)
	var wg sync.WaitGroup
	var done atomic.Int64
	var lastReport atomic.Int64
	lastReport.Store(time.Now().UnixNano())

	for range max(2, runtime.NumCPU()) {
		wg.Go(func() {
			for h := range work {
				fi := inspect(dir, h)
				mu.Lock()
				out[h] = fi
				mu.Unlock()
				reportProgress(&done, &lastReport, len(hashes), progress)
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
	if progress != nil && len(hashes) > 0 {
		progress(int(done.Load()), len(hashes))
	}
	return out
}

func reportProgress(done, lastReport *atomic.Int64, total int, progress func(done, total int)) {
	n := done.Add(1)
	if progress == nil {
		return
	}
	now := time.Now().UnixNano()
	prev := lastReport.Load()
	if n%50 == 0 || now-prev >= int64(200*time.Millisecond) {
		if lastReport.CompareAndSwap(prev, now) {
			progress(int(n), total)
		}
	}
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
func copyFiles(ctx context.Context, fetcher FileFetcher, store storage.Store, blobs []tBlob, shared bool, progress func(done, total int)) (int, error) {
	var todo []tBlob
	for _, b := range blobs {
		if b.File != "" && (!shared || b.File != b.Hash) {
			todo = append(todo, b)
		}
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(max(2, runtime.NumCPU()))
	var done atomic.Int64
	var lastReport atomic.Int64
	lastReport.Store(time.Now().UnixNano())
	for _, b := range todo {
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := fetcher.Fetch(ctx, b.File)
			if err != nil {
				return fmt.Errorf("copy texture %s: %w", b.File, err)
			}
			if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != b.Hash {
				return fmt.Errorf("copy texture %s: the file changed during the import", b.File)
			}
			if err := store.Put(ctx, b.Hash, data); err != nil {
				return fmt.Errorf("copy texture %s: %w", b.File, err)
			}
			reportProgress(&done, &lastReport, len(todo), progress)
			return nil
		})
	}
	err := g.Wait()
	n := int(done.Load())
	if progress != nil && len(todo) > 0 {
		progress(n, len(todo))
	}
	return n, err
}
