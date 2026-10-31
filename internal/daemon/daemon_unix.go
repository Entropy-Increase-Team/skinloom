//go:build !windows

package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/kardianos/service"
)

func platformConfig(*service.Config) {}

// prepare checks the service user and hands it the data directory and frontend cache.
func prepare(o Options) error {
	if err := os.MkdirAll(o.DataDir, 0o750); err != nil {
		return err
	}
	if o.User == "" {
		return nil
	}
	u, err := user.Lookup(o.User)
	if err != nil {
		return fmt.Errorf("user %q: %w (create it, e.g. sudo useradd --system --shell /usr/sbin/nologin %s, or pass --user)", o.User, err, o.User)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	for _, dir := range o.writable() {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(path, uid, gid)
		})
		if err != nil {
			return privileged(fmt.Errorf("give %s to %s: %w", dir, o.User, err))
		}
	}
	return nil
}
