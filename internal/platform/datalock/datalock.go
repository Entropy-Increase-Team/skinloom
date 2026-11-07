// Package datalock marks a data directory as in use (ADR 0039). A running site holds
// a shared lock on <data>/skinloom.lock, so processes of several roles can share one
// directory; skinloom restore needs the lock exclusively and gives up at once when a
// site holds it. The operating system drops a lock when its process ends, crashed or
// not, so a stale lock file never blocks anything.
package datalock

import (
	"errors"
	"os"
	"path/filepath"
)

// FileName is the lock file in the data directory. It stays after use: deleting it
// would let a new process lock a fresh file while another still holds the old one.
const FileName = "skinloom.lock"

// ErrInUse: another process holds the lock in a mode that conflicts with the request.
var ErrInUse = errors.New("the data directory is in use by another SkinLoom process")

// Lock is a held lock; Release it, or let the process end.
type Lock struct{ f *os.File }

// Shared takes the lock shared without waiting: a running site. It fails with
// ErrInUse while a restore holds it.
func Shared(dataDir string) (*Lock, error) { return take(dataDir, false) }

// Exclusive takes the lock exclusively without waiting: a restore. It fails with
// ErrInUse while any site holds it.
func Exclusive(dataDir string) (*Lock, error) { return take(dataDir, true) }

func take(dataDir string, exclusive bool) (*Lock, error) {
	f, err := os.OpenFile(filepath.Join(dataDir, FileName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f, exclusive); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

// Release gives the lock up.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlock(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	l.f = nil
	return err
}
