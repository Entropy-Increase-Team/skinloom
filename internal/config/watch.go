package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch reloads the file on change and calls apply with the old and new config plus the
// changed keys. Invalid edits are logged and ignored, so a typo never takes the site down.
//
// It watches the parent directory rather than the file: editors that save atomically
// (write a temp file, then rename over the original) would otherwise end the watch.
func Watch(ctx context.Context, path string, current Config, log *slog.Logger, apply func(old, new Config, changed []string)) {
	abs, err := filepath.Abs(path)
	if err != nil {
		log.Warn("config hot reload disabled", "err", err)
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn("config hot reload disabled", "err", err)
		return
	}
	if err := w.Add(filepath.Dir(abs)); err != nil {
		w.Close()
		log.Warn("config hot reload disabled", "err", err)
		return
	}
	go func() {
		defer w.Close()
		var debounce <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Warn("config watcher error", "err", err)
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if sameFile(ev.Name, abs) {
					debounce = time.After(300 * time.Millisecond) // coalesce bursts from one save
				}
			case <-debounce:
				debounce = nil
				if _, err := os.Stat(abs); err != nil {
					continue // mid-rename or deleted; keep running with the current config
				}
				next, err := Load(abs)
				if err != nil {
					log.Error("config file has errors; keeping the running config", "path", abs, "err", err)
					continue
				}
				changed := Diff(current, next)
				if len(changed) == 0 {
					continue
				}
				apply(current, next, changed)
				current = next
			}
		}
	}()
}

func sameFile(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
