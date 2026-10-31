package logging

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// RotateAt is the size beyond which OpenFile starts a new file, keeping the old one
// as <name>.1 (one generation: enough to read the last start's setup link).
const RotateAt = 20 << 20

// OpenFile opens path for appending, creating its directory, after moving a file
// grown beyond RotateAt to path.1.
func OpenFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > RotateAt {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
}

// Tee writes to the file first, then to the console: io.MultiWriter stops at the
// first error, and a background service's console may be no handle at all.
func Tee(file, console io.Writer) io.Writer {
	return &tee{file: file, console: console}
}

type tee struct{ file, console io.Writer }

func (t *tee) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	_, _ = t.console.Write(p) // best effort
	return n, err
}
