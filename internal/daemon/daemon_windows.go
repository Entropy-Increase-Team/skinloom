package daemon

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/kardianos/service"
)

// localService is the low-privilege built-in account the service runs as; the
// password is empty for it.
const localService = `NT AUTHORITY\LocalService`

func platformConfig(c *service.Config) { c.UserName = localService }

// prepare lets LOCAL SERVICE read the bundle and write the data directory and the
// frontend cache, and nothing else (S-1-5-19 is its well-known SID; the name differs
// by language). Reading matters for a bundle in a user's profile, which other
// accounts cannot read by default.
func prepare(o Options) error {
	if err := os.MkdirAll(o.DataDir, 0o750); err != nil {
		return err
	}
	if out, err := exec.Command("icacls", o.dir(), "/grant", "*S-1-5-19:(OI)(CI)RX", "/Q").CombinedOutput(); err != nil {
		return privileged(fmt.Errorf("grant LOCAL SERVICE access to %s: %v: %s", o.dir(), err, out))
	}
	for _, dir := range o.writable() {
		if _, err := os.Stat(dir); err != nil {
			continue // no frontend cache yet: created on first start, inside web/ it can read
		}
		out, err := exec.Command("icacls", dir, "/grant", "*S-1-5-19:(OI)(CI)M", "/T", "/Q").CombinedOutput()
		if err != nil {
			return privileged(fmt.Errorf("grant LOCAL SERVICE access to %s: %v: %s", dir, err, out))
		}
	}
	return nil
}
