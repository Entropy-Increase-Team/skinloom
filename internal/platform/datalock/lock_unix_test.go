//go:build unix

package datalock

import (
	"fmt"
	"io/fs"
	"testing"

	"golang.org/x/sys/unix"
)

// A volume without flock must not stop the site from starting; real conflicts and
// other errors still count.
func TestUnsupported(t *testing.T) {
	for _, err := range []error{unix.ENOLCK, unix.EOPNOTSUPP, unix.ENOTSUP, fmt.Errorf("flock: %w", unix.ENOLCK)} {
		if !unsupported(err) {
			t.Errorf("%v: want unsupported", err)
		}
	}
	for _, err := range []error{unix.EWOULDBLOCK, unix.EBADF, unix.EINTR, fs.ErrPermission} {
		if unsupported(err) {
			t.Errorf("%v: want a real error", err)
		}
	}
}
