//go:build !windows

package launcher

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a new session, like a daemon would.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
