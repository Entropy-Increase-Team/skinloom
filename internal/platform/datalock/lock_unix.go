//go:build unix

package datalock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// flock locks belong to the open file, so two opens in one process conflict too.
func lock(f *os.File, exclusive bool) error {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	for {
		err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK):
			return ErrInUse
		case unsupported(err):
			return nil // as on systems without file locks: nothing is refused
		default:
			return err
		}
	}
}

func unlock(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil && !unsupported(err) {
		return err
	}
	return nil
}

// unsupported: the volume has no flock (some CIFS, FUSE and network volumes). A data
// directory there worked before the lock existed, so it still starts, unprotected.
func unsupported(err error) bool {
	return errors.Is(err, unix.ENOLCK) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTSUP)
}
